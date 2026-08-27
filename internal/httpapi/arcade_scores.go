package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type arcadeMachineRules struct {
	decimal bool
	maximum float64
}

var arcadeMachines = map[string]arcadeMachineRules{
	"qte-0":   {maximum: 1_000_000_000},
	"qte-1":   {maximum: 99_999},
	"darts-0": {decimal: true, maximum: 300},
	"darts-1": {decimal: true, maximum: 300},
}

type ArcadeScoreStore interface {
	ArcadeHighScores(context.Context) ([]store.ArcadeHighScore, error)
	CharacterForAccount(context.Context, int64, int64) (store.Character, error)
	SubmitArcadeScore(
		context.Context,
		int64,
		int64,
		string,
		float64,
	) (store.ArcadeScoreSubmission, error)
}

type ArcadeScorePublisher interface {
	PublishArcadeHighScore(
		machineID string,
		score float64,
		playerName string,
		achievedAt time.Time,
	)
}

type ArcadeScoreAuthenticator interface {
	FromRequest(context.Context, *http.Request) (store.Account, error)
}

type ArcadeScoreHandler struct {
	auth      ArcadeScoreAuthenticator
	store     ArcadeScoreStore
	publisher ArcadeScorePublisher
}

func NewArcadeScoreHandler(
	authenticator ArcadeScoreAuthenticator,
	database ArcadeScoreStore,
	publisher ArcadeScorePublisher,
) *ArcadeScoreHandler {
	return &ArcadeScoreHandler{
		auth:      authenticator,
		store:     database,
		publisher: publisher,
	}
}

func validArcadeScore(machineID string, score float64) bool {
	rules, ok := arcadeMachines[machineID]
	if !ok || math.IsNaN(score) || math.IsInf(score, 0) ||
		score < 0 || score > rules.maximum {
		return false
	}
	return rules.decimal || score == math.Trunc(score)
}

func normalizeArcadeScore(machineID string, score float64) float64 {
	if arcadeMachines[machineID].decimal {
		return math.Round(score*10_000) / 10_000
	}
	return score
}

func (h *ArcadeScoreHandler) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	switch request.Method {
	case http.MethodGet:
		scores, err := h.store.ArcadeHighScores(ctx)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "arcade scores unavailable")
			return
		}
		response.Header().Set("Cache-Control", "no-cache")
		writeJSON(response, http.StatusOK, map[string]any{"scores": scores})
	case http.MethodPost:
		account, err := h.auth.FromRequest(ctx, request)
		if err != nil {
			status := http.StatusInternalServerError
			message := "score submission failed"
			if errors.Is(err, auth.ErrUnauthenticated) {
				status = http.StatusUnauthorized
				message = "authentication required"
			}
			writeError(response, status, message)
			return
		}
		var body struct {
			CharacterID int64   `json:"characterId"`
			MachineID   string  `json:"machineId"`
			Score       float64 `json:"score"`
		}
		if err := decodeBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		if body.CharacterID <= 0 ||
			!validArcadeScore(body.MachineID, body.Score) {
			writeError(response, http.StatusUnprocessableEntity, "invalid arcade score")
			return
		}
		body.Score = normalizeArcadeScore(body.MachineID, body.Score)
		character, err := h.store.CharacterForAccount(
			ctx,
			account.ID,
			body.CharacterID,
		)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(response, http.StatusForbidden, "character not available")
				return
			}
			writeError(response, http.StatusInternalServerError, "score submission failed")
			return
		}
		submission, err := h.store.SubmitArcadeScore(
			ctx,
			account.ID,
			character.ID,
			body.MachineID,
			body.Score,
		)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "score submission failed")
			return
		}
		if submission.NewHighScore && h.publisher != nil {
			h.publisher.PublishArcadeHighScore(
				submission.MachineID,
				submission.Score,
				character.Name,
				submission.AchievedAt,
			)
		}
		writeJSON(response, http.StatusOK, submission)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}
