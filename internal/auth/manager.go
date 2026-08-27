package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brynnb/new-yokosuka-server/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	CookieName      = "new_yokosuka_session"
	SessionLifetime = 30 * 24 * time.Hour
)

var (
	ErrUnauthenticated    = errors.New("authentication required")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("password must be between 8 and 72 bytes")
	ErrInvalidGuestToken  = errors.New("invalid guest token")
)

type Manager struct {
	store         *store.Store
	secureCookies bool
	now           func() time.Time
}

type Session struct {
	Account   store.Account
	RawToken  string
	ExpiresAt time.Time
}

func NewManager(database *store.Store, secureCookies bool) *Manager {
	return &Manager{
		store:         database,
		secureCookies: secureCookies,
		now:           time.Now,
	}
}

func validateGuestToken(token string) error {
	if len(token) < 16 || len(token) > 128 || !utf8.ValidString(token) {
		return ErrInvalidGuestToken
	}
	for _, value := range token {
		if value <= 0x20 || value > 0x7e {
			return ErrInvalidGuestToken
		}
	}
	return nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) < 3 || len(email) > 254 || strings.Count(email, "@") != 1 {
		return "", ErrInvalidEmail
	}
	parts := strings.SplitN(email, "@", 2)
	if parts[0] == "" || parts[1] == "" || !strings.Contains(parts[1], ".") {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func validatePassword(password string) error {
	size := len([]byte(password))
	if size < 8 || size > 72 {
		return ErrInvalidPassword
	}
	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(
		[]byte(password),
		bcrypt.DefaultCost,
	)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func passwordMatches(passwordHash, password string) bool {
	return bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	) == nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (m *Manager) issue(ctx context.Context, account store.Account) (Session, error) {
	rawToken, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	expiresAt := m.now().Add(SessionLifetime)
	if err := m.store.CreateSession(
		ctx,
		store.HashToken(rawToken),
		account.ID,
		expiresAt,
	); err != nil {
		return Session{}, err
	}
	return Session{Account: account, RawToken: rawToken, ExpiresAt: expiresAt}, nil
}

func (m *Manager) Guest(ctx context.Context, guestToken string) (Session, error) {
	if err := validateGuestToken(guestToken); err != nil {
		return Session{}, err
	}
	account, err := m.store.GetOrCreateGuest(ctx, store.HashToken(guestToken))
	if err != nil {
		return Session{}, err
	}
	return m.issue(ctx, account)
}

func (m *Manager) Login(ctx context.Context, email, password string) (Session, error) {
	normalized, err := normalizeEmail(email)
	if err != nil || validatePassword(password) != nil {
		return Session{}, ErrInvalidCredentials
	}
	account, passwordHash, err := m.store.AccountCredentials(ctx, normalized)
	if err != nil || !passwordMatches(passwordHash, password) {
		return Session{}, ErrInvalidCredentials
	}
	return m.issue(ctx, account)
}

func (m *Manager) Register(
	ctx context.Context,
	current *store.Account,
	email,
	password string,
) (Session, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return Session{}, err
	}
	if err := validatePassword(password); err != nil {
		return Session{}, err
	}
	passwordHash, err := hashPassword(password)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	var account store.Account
	if current != nil && current.AccountType == "guest" {
		account, err = m.store.UpgradeGuestAccount(
			ctx,
			current.ID,
			normalized,
			passwordHash,
		)
	} else {
		account, err = m.store.CreateRegisteredAccount(
			ctx,
			normalized,
			passwordHash,
		)
	}
	if err != nil {
		return Session{}, err
	}
	return m.issue(ctx, account)
}

func (m *Manager) FromRequest(ctx context.Context, request *http.Request) (store.Account, error) {
	cookie, err := request.Cookie(CookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return store.Account{}, ErrUnauthenticated
	}
	account, err := m.store.AccountBySession(ctx, store.HashToken(cookie.Value))
	if errors.Is(err, store.ErrNotFound) {
		return store.Account{}, ErrUnauthenticated
	}
	return account, err
}

func (m *Manager) RevokeRequest(ctx context.Context, request *http.Request) error {
	cookie, err := request.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	return m.store.RevokeSession(ctx, store.HashToken(cookie.Value))
}

func (m *Manager) SetCookie(response http.ResponseWriter, session Session) {
	http.SetCookie(response, &http.Cookie{
		Name:     CookieName,
		Value:    session.RawToken,
		Path:     "/",
		Expires:  session.ExpiresAt,
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) ClearCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
