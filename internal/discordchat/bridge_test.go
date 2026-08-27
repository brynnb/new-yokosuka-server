package discordchat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedRequest(t *testing.T, secret string, body string, timestamp time.Time) *http.Request {
	t.Helper()
	timestampText := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestampText))
	_, _ = mac.Write([]byte(body))
	request := httptest.NewRequest(http.MethodPost, "/api/discord/game-chat", strings.NewReader(body))
	request.Header.Set("X-Game-Chat-Timestamp", timestampText)
	request.Header.Set("X-Game-Chat-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request
}

func TestSignedIngressPublishesMessage(t *testing.T) {
	secret := strings.Repeat("s", 32)
	var got Message
	bridge := &Bridge{secret: secret, publish: func(senderName, text string) error {
		got = Message{SenderName: senderName, Text: text}
		return nil
	}}
	body := `{"senderName":"Tester[Discord]","text":"hello"}`
	recorder := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(recorder, signedRequest(t, secret, body, time.Now()))
	if recorder.Code != http.StatusNoContent || got != (Message{SenderName: "Tester[Discord]", Text: "hello"}) {
		t.Fatalf("status=%d message=%#v", recorder.Code, got)
	}
}

func TestIngressRejectsInvalidOrStaleSignature(t *testing.T) {
	secret := strings.Repeat("s", 32)
	bridge := &Bridge{secret: secret, publish: func(string, string) error { return nil }}
	body := `{"senderName":"Tester","text":"hello"}`
	for _, request := range []*http.Request{
		signedRequest(t, "wrong"+secret, body, time.Now()),
		signedRequest(t, secret, body, time.Now().Add(-6*time.Minute)),
	} {
		recorder := httptest.NewRecorder()
		bridge.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want unauthorized", recorder.Code)
		}
	}
}

func TestEnvironmentConfigurationIsAllOrNothing(t *testing.T) {
	t.Setenv("DISCORD_CHAT_SHARED_SECRET", "")
	t.Setenv("DISCORD_CHAT_WEBHOOK_URL", "")
	publish := func(string, string) error { return nil }

	bridge, err := NewFromEnvironment(publish)
	if err != nil || bridge != nil {
		t.Fatalf("disabled configuration = (%#v, %v), want (nil, nil)", bridge, err)
	}

	t.Setenv("DISCORD_CHAT_WEBHOOK_URL", "https://discord.example/webhook")
	if _, err := NewFromEnvironment(publish); err == nil {
		t.Fatal("partial configuration was accepted")
	}

	t.Setenv("DISCORD_CHAT_SHARED_SECRET", "too-short")
	if _, err := NewFromEnvironment(publish); err == nil {
		t.Fatal("short shared secret was accepted")
	}

	t.Setenv("DISCORD_CHAT_SHARED_SECRET", strings.Repeat("s", 32))
	bridge, err = NewFromEnvironment(publish)
	if err != nil || bridge == nil {
		t.Fatalf("complete configuration = (%#v, %v), want enabled bridge", bridge, err)
	}
}
