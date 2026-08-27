package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type fakeArcadeScoreStore struct {
	scores       []store.ArcadeHighScore
	submission   store.ArcadeScoreSubmission
	character    store.Character
	characterErr error
	accountID    int64
	characterID  int64
	machineID    string
	score        float64
}

func (f *fakeArcadeScoreStore) ArcadeHighScores(
	context.Context,
) ([]store.ArcadeHighScore, error) {
	return f.scores, nil
}

func (f *fakeArcadeScoreStore) CharacterForAccount(
	_ context.Context,
	accountID int64,
	characterID int64,
) (store.Character, error) {
	f.accountID = accountID
	f.characterID = characterID
	if f.characterErr != nil {
		return store.Character{}, f.characterErr
	}
	if f.character.ID == 0 {
		return store.Character{ID: characterID, Name: "Ryo Hazuki"}, nil
	}
	return f.character, nil
}

func (f *fakeArcadeScoreStore) SubmitArcadeScore(
	_ context.Context,
	accountID int64,
	characterID int64,
	machineID string,
	score float64,
) (store.ArcadeScoreSubmission, error) {
	f.accountID = accountID
	f.characterID = characterID
	f.machineID = machineID
	f.score = score
	return f.submission, nil
}

type fakeArcadeScorePublisher struct {
	machineID  string
	score      float64
	playerName string
	achievedAt time.Time
}

func (f *fakeArcadeScorePublisher) PublishArcadeHighScore(
	machineID string,
	score float64,
	playerName string,
	achievedAt time.Time,
) {
	f.machineID = machineID
	f.score = score
	f.playerName = playerName
	f.achievedAt = achievedAt
}

type fakeArcadeAuthenticator struct {
	account store.Account
	err     error
}

func (f fakeArcadeAuthenticator) FromRequest(
	context.Context,
	*http.Request,
) (store.Account, error) {
	return f.account, f.err
}

func TestArcadeScoresGetReturnsAllMachineScores(t *testing.T) {
	database := &fakeArcadeScoreStore{scores: []store.ArcadeHighScore{{
		MachineID:  "qte-0",
		Score:      1234,
		AchievedAt: time.Unix(10, 0),
	}}}
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{}, database, nil).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/arcade-scores", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var body struct {
		Scores []store.ArcadeHighScore `json:"scores"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scores) != 1 || body.Scores[0].Score != 1234 {
		t.Fatalf("unexpected scores: %#v", body.Scores)
	}
	if response.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("unexpected cache policy: %q", response.Header().Get("Cache-Control"))
	}
}

func TestArcadeScoresPostSubmitsAuthenticatedScore(t *testing.T) {
	database := &fakeArcadeScoreStore{
		character: store.Character{ID: 9, Name: "Nozomi Harasaki"},
		submission: store.ArcadeScoreSubmission{
			ArcadeHighScore: store.ArcadeHighScore{
				MachineID:  "darts-1",
				Score:      88.5,
				AchievedAt: time.Unix(20, 0),
			},
			NewHighScore: true,
		},
	}
	publisher := &fakeArcadeScorePublisher{}
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{
		account: store.Account{ID: 42},
	}, database, publisher).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/arcade-scores",
			strings.NewReader(
				`{"characterId":9,"machineId":"darts-1","score":88.5}`,
			),
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if database.accountID != 42 ||
		database.characterID != 9 ||
		database.machineID != "darts-1" ||
		database.score != 88.5 {
		t.Fatalf(
			"unexpected submission: account=%d machine=%q score=%f",
			database.accountID,
			database.machineID,
			database.score,
		)
	}
	if publisher.machineID != "darts-1" ||
		publisher.score != 88.5 ||
		publisher.playerName != "Nozomi Harasaki" ||
		!publisher.achievedAt.Equal(time.Unix(20, 0)) {
		t.Fatalf("unexpected published score: %#v", publisher)
	}
}

func TestArcadeScoresPostRequiresAuthentication(t *testing.T) {
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{
		err: auth.ErrUnauthenticated,
	}, &fakeArcadeScoreStore{}, nil).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/arcade-scores",
			strings.NewReader(
				`{"characterId":9,"machineId":"qte-0","score":100}`,
			),
		),
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestArcadeScoresPostRejectsAnotherAccountsCharacter(t *testing.T) {
	database := &fakeArcadeScoreStore{characterErr: store.ErrNotFound}
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{
		account: store.Account{ID: 42},
	}, database, &fakeArcadeScorePublisher{}).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/arcade-scores",
			strings.NewReader(
				`{"characterId":99,"machineId":"qte-0","score":100}`,
			),
		),
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.Code)
	}
	if database.machineID != "" {
		t.Fatal("score was submitted for an unavailable character")
	}
}

func TestArcadeScoresPostDoesNotPublishAnOrdinaryScore(t *testing.T) {
	database := &fakeArcadeScoreStore{
		submission: store.ArcadeScoreSubmission{
			ArcadeHighScore: store.ArcadeHighScore{
				MachineID:  "qte-0",
				Score:      500,
				AchievedAt: time.Unix(20, 0),
			},
		},
	}
	publisher := &fakeArcadeScorePublisher{}
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{
		account: store.Account{ID: 42},
	}, database, publisher).ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/api/arcade-scores",
			strings.NewReader(
				`{"characterId":9,"machineId":"qte-0","score":100}`,
			),
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if publisher.machineID != "" {
		t.Fatalf("ordinary score was published: %#v", publisher)
	}
}

func TestArcadeScoresRejectInvalidMachineAndScoreShape(t *testing.T) {
	tests := []string{
		`{"characterId":9,"machineId":"unknown","score":100}`,
		`{"characterId":9,"machineId":"qte-0","score":10.5}`,
		`{"characterId":9,"machineId":"darts-0","score":-1}`,
		`{"characterId":9,"machineId":"darts-1","score":301}`,
		`{"characterId":9,"machineId":"qte-1","score":100000}`,
	}
	for _, body := range tests {
		response := httptest.NewRecorder()
		NewArcadeScoreHandler(fakeArcadeAuthenticator{
			account: store.Account{ID: 42},
		}, &fakeArcadeScoreStore{}, nil).ServeHTTP(
			response,
			httptest.NewRequest(
				http.MethodPost,
				"/api/arcade-scores",
				strings.NewReader(body),
			),
		)
		if response.Code != http.StatusUnprocessableEntity {
			t.Errorf("body %s: status = %d, want 422", body, response.Code)
		}
	}
}

func TestArcadeScoresPostDoesNotMaskAuthenticationErrors(t *testing.T) {
	response := httptest.NewRecorder()
	NewArcadeScoreHandler(fakeArcadeAuthenticator{
		err: errors.New("database unavailable"),
	}, &fakeArcadeScoreStore{}, nil).ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/api/arcade-scores", nil),
	)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}
