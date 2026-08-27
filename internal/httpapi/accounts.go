package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/auth"
	"github.com/brynnb/new-yokosuka-server/internal/dialoguestate"
	"github.com/brynnb/new-yokosuka-server/internal/game"
	"github.com/brynnb/new-yokosuka-server/internal/playername"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const requestTimeout = 5 * time.Second

type authAttemptWindow struct {
	mu       sync.Mutex
	attempts []time.Time
}

type AccountHandler struct {
	auth                 *auth.Manager
	store                *store.Store
	locationSynchronizer CharacterLocationSynchronizer
	authAttempts         sync.Map
}

type CharacterLocationSynchronizer interface {
	SyncAccountLocations(context.Context, int64) error
}

func NewAccountHandler(
	manager *auth.Manager,
	database *store.Store,
	locationSynchronizer CharacterLocationSynchronizer,
) *AccountHandler {
	return &AccountHandler{
		auth:                 manager,
		store:                database,
		locationSynchronizer: locationSynchronizer,
	}
}

func decodeBody(response http.ResponseWriter, request *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

func requestIP(request *http.Request) string {
	forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}
	return request.RemoteAddr
}

func (h *AccountHandler) allowAuthAttempt(request *http.Request) bool {
	value, _ := h.authAttempts.LoadOrStore(requestIP(request), &authAttemptWindow{})
	window := value.(*authAttemptWindow)
	window.mu.Lock()
	defer window.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	kept := window.attempts[:0]
	for _, attempt := range window.attempts {
		if attempt.After(cutoff) {
			kept = append(kept, attempt)
		}
	}
	window.attempts = kept
	if len(kept) >= 10 {
		return false
	}
	window.attempts = append(window.attempts, now)
	return true
}

func (h *AccountHandler) responseForAccount(
	account store.Account,
) map[string]any {
	return map[string]any{
		"account": account,
	}
}

func (h *AccountHandler) writeSession(
	response http.ResponseWriter,
	session auth.Session,
) {
	body := h.responseForAccount(session.Account)
	h.auth.SetCookie(response, session)
	writeJSON(response, http.StatusOK, body)
}

func (h *AccountHandler) Guest(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.allowAuthAttempt(request) {
		writeError(response, http.StatusTooManyRequests, "too many authentication attempts")
		return
	}
	var body struct {
		GuestToken string `json:"guestToken"`
	}
	if err := decodeBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	session, err := h.auth.Guest(ctx, body.GuestToken)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidGuestToken) {
			writeError(response, http.StatusUnauthorized, "guest authentication failed")
			return
		}
		log.Printf("guest authentication unavailable: %v", err)
		writeError(
			response,
			http.StatusServiceUnavailable,
			"account service temporarily unavailable",
		)
		return
	}
	h.writeSession(response, session)
}

func (h *AccountHandler) Login(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.allowAuthAttempt(request) {
		writeError(response, http.StatusTooManyRequests, "too many authentication attempts")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	session, err := h.auth.Login(ctx, body.Email, body.Password)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "invalid email or password")
		return
	}
	h.writeSession(response, session)
}

func (h *AccountHandler) Register(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.allowAuthAttempt(request) {
		writeError(response, http.StatusTooManyRequests, "too many authentication attempts")
		return
	}
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(response, request, &body); err != nil {
		writeError(response, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	var current *store.Account
	if account, err := h.auth.FromRequest(ctx, request); err == nil {
		current = &account
	}
	session, err := h.auth.Register(ctx, current, body.Email, body.Password)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrEmailTaken):
			writeError(response, http.StatusConflict, "email already taken")
		case errors.Is(err, auth.ErrInvalidEmail), errors.Is(err, auth.ErrInvalidPassword):
			writeError(response, http.StatusUnprocessableEntity, err.Error())
		default:
			writeError(response, http.StatusInternalServerError, "registration failed")
		}
		return
	}
	h.writeSession(response, session)
}

func (h *AccountHandler) Logout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	_ = h.auth.RevokeRequest(ctx, request)
	h.auth.ClearCookie(response)
	response.WriteHeader(http.StatusNoContent)
}

func (h *AccountHandler) Session(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	account, err := h.auth.FromRequest(ctx, request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	writeJSON(response, http.StatusOK, h.responseForAccount(account))
}

func characterNameValidationError(name string) string {
	if _, err := playername.Normalize(name); err != nil {
		return err.Error()
	}
	return ""
}

func writeCharacterNameValidationError(
	response http.ResponseWriter,
	messages []string,
) {
	writeJSON(response, http.StatusUnprocessableEntity, map[string]any{
		"error":  "That character name is not eligible.",
		"errors": messages,
	})
}

func (h *AccountHandler) Characters(response http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	account, err := h.auth.FromRequest(ctx, request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	switch request.Method {
	case http.MethodGet:
		// A refresh can request the character list before the old WebSocket's
		// disconnect has finished flushing its newest location. Reconcile the
		// realtime state first so character selection and world loading read the
		// same authoritative server value.
		if h.locationSynchronizer != nil {
			if err := h.locationSynchronizer.SyncAccountLocations(
				ctx,
				account.ID,
			); err != nil {
				log.Printf("character location sync failed: %v", err)
				writeError(
					response,
					http.StatusServiceUnavailable,
					"character locations temporarily unavailable",
				)
				return
			}
		}
		characters, err := h.store.ListCharacters(ctx, account.ID)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "character lookup failed")
			return
		}
		writeJSON(response, http.StatusOK, map[string]any{"characters": characters})
	case http.MethodPost:
		var body struct {
			Name     string `json:"name"`
			AvatarID string `json:"avatarId"`
		}
		if err := decodeBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		validationMessages := playername.ValidationMessages(body.Name)
		if len(validationMessages) > 0 {
			writeCharacterNameValidationError(response, validationMessages)
			return
		}
		normalizedName, _ := playername.Normalize(body.Name)
		body.Name = normalizedName
		if !game.ValidAvatar(body.AvatarID) {
			writeError(response, http.StatusUnprocessableEntity, "invalid avatar")
			return
		}
		character, err := h.store.CreateCharacter(
			ctx, account.ID, body.Name, body.AvatarID,
			"exterior", -6.48, 0, -19.32, 0,
			game.StartingMaxHP,
		)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrNameTaken):
				writeError(response, http.StatusConflict, "That character name is already in use.")
			case errors.Is(err, store.ErrCharacterLimit):
				writeError(response, http.StatusUnprocessableEntity, err.Error())
			default:
				writeError(response, http.StatusInternalServerError, "character creation failed")
			}
			return
		}
		writeJSON(response, http.StatusCreated, character)
	default:
		response.Header().Set("Allow", "GET, POST")
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *AccountHandler) Character(response http.ResponseWriter, request *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(
		request.URL.Path,
		"/api/characters/",
	), "/"), "/")
	characterID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || characterID <= 0 {
		http.NotFound(response, request)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestTimeout)
	defer cancel()
	account, err := h.auth.FromRequest(ctx, request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	if _, err := h.store.CharacterForAccount(ctx, account.ID, characterID); err != nil {
		http.NotFound(response, request)
		return
	}
	if len(parts) == 1 && request.Method == http.MethodDelete {
		if err := h.store.SoftDeleteCharacter(ctx, account.ID, characterID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.NotFound(response, request)
				return
			}
			writeError(response, http.StatusInternalServerError, "character deletion failed")
			return
		}
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) == 2 && parts[1] == "state" && request.Method == http.MethodGet {
		h.writeCharacterState(response, ctx, account.ID, characterID)
		return
	}
	if len(parts) == 2 && parts[1] == "dialogue" {
		switch request.Method {
		case http.MethodGet:
			snapshot, err := h.store.DialogueState(
				ctx,
				account.ID,
				characterID,
			)
			if err != nil {
				writeError(response, http.StatusInternalServerError, "dialogue state failed")
				return
			}
			writeJSON(response, http.StatusOK, snapshot)
		case http.MethodPut:
			var snapshot dialoguestate.Snapshot
			if err := decodeBody(response, request, &snapshot); err != nil {
				writeError(response, http.StatusBadRequest, err.Error())
				return
			}
			saved, err := h.store.SaveDialogueState(
				ctx,
				account.ID,
				characterID,
				snapshot,
			)
			switch {
			case errors.Is(err, store.ErrRevisionConflict):
				current, loadErr := h.store.DialogueState(
					ctx,
					account.ID,
					characterID,
				)
				if loadErr != nil {
					writeError(
						response,
						http.StatusConflict,
						"dialogue state revision conflict",
					)
					return
				}
				log.Printf(
					"dialogue revision conflict: character=%d requested=%d current=%d",
					characterID,
					snapshot.Revision,
					current.Revision,
				)
				writeJSON(response, http.StatusConflict, map[string]any{
					"error":         "dialogue state revision conflict",
					"dialogueState": current,
				})
			case err != nil:
				writeError(response, http.StatusUnprocessableEntity, "invalid dialogue state")
			default:
				writeJSON(response, http.StatusOK, saved)
			}
		default:
			response.Header().Set("Allow", "GET, PUT")
			writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) == 2 && parts[1] == "use-item" && request.Method == http.MethodPost {
		var body struct {
			ItemKey string `json:"itemKey"`
		}
		if err := decodeBody(response, request, &body); err != nil {
			writeError(response, http.StatusBadRequest, err.Error())
			return
		}
		if _, _, err := h.store.UseHealingItem(ctx, characterID, body.ItemKey); err != nil {
			if errors.Is(err, store.ErrInsufficient) {
				writeError(response, http.StatusConflict, "item unavailable")
			} else if err.Error() == "hp is already full" {
				writeError(response, http.StatusConflict, err.Error())
			} else {
				writeError(response, http.StatusInternalServerError, "item use failed")
			}
			return
		}
		h.writeCharacterState(response, ctx, account.ID, characterID)
		return
	}
	writeError(response, http.StatusMethodNotAllowed, "method not allowed")
}

func (h *AccountHandler) writeCharacterState(
	response http.ResponseWriter,
	ctx context.Context,
	accountID,
	characterID int64,
) {
	state, err := h.store.CharacterState(ctx, accountID, characterID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "character state failed")
		return
	}
	progression, err := h.store.Progression(ctx, characterID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "progression lookup failed")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"character":   state.Character,
		"progression": progression,
		"inventory":   state.Inventory,
	})
}
