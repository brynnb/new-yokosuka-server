package httpapi

import (
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestAuthenticationResponseContainsAccountWithoutCharacters(t *testing.T) {
	handler := &AccountHandler{}
	response := handler.responseForAccount(store.Account{})
	if _, exists := response["account"]; !exists {
		t.Fatal("authentication response is missing account")
	}
	if _, exists := response["characters"]; exists {
		t.Fatal("authentication response must not contain characters")
	}
}

func TestCharacterNameValidationMessages(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{"Al", "Name must be between 3 and 24 characters."},
		{"A_name", "Names may contain letters, one internal space, and one internal hyphen."},
		{"-dsf-s-fs-fs", "Names may contain letters, one internal space, and one internal hyphen."},
		{"Alex Van Dyke", "Names may contain letters, one internal space, and one internal hyphen."},
		{"Ryo", "That name is not allowed. Main Shenmue character names cannot be used."},
		{"Ine-san", "That name is not allowed. Main Shenmue character names cannot be used."},
		{"Harasaki", ""},
		{"Avery Smith-Jones", ""},
	}
	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			if got := characterNameValidationError(current.name); got != current.message {
				t.Fatalf("validation error = %q, want %q", got, current.message)
			}
		})
	}
}
