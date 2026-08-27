package playername

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/brynnb/new-yokosuka-server/internal/contentfilter"
)

var (
	ErrLength  = errors.New("Name must be between 3 and 24 characters.")
	ErrFormat  = errors.New("Names may contain letters, one internal space, and one internal hyphen.")
	ErrBlocked = errors.New("That name is not allowed. Main Shenmue character names cannot be used.")
)

const (
	lettersMessage   = "Names can only contain letters, spaces, and hyphens."
	spaceMessage     = "Names can contain at most one space."
	hyphenMessage    = "Names can contain at most one hyphen."
	separatorMessage = "Spaces and hyphens must be between letters."
)

// ValidationMessages reports every rule the supplied name violates.
func ValidationMessages(name string) []string {
	messages := make([]string, 0, 4)
	if !utf8.ValidString(name) {
		return []string{lettersMessage}
	}
	normalized := strings.TrimSpace(name)
	if count := utf8.RuneCountInString(normalized); count < 3 || count > 24 {
		messages = append(messages, ErrLength.Error())
	}

	values := []rune(normalized)
	spaceCount := 0
	hyphenCount := 0
	invalidCharacter := false
	invalidSeparator := false
	for index, value := range values {
		switch {
		case unicode.IsLetter(value):
		case value == ' ':
			spaceCount++
			if index == 0 || index == len(values)-1 ||
				!unicode.IsLetter(values[index-1]) ||
				!unicode.IsLetter(values[index+1]) {
				invalidSeparator = true
			}
		case value == '-':
			hyphenCount++
			if index == 0 || index == len(values)-1 ||
				!unicode.IsLetter(values[index-1]) ||
				!unicode.IsLetter(values[index+1]) {
				invalidSeparator = true
			}
		default:
			invalidCharacter = true
		}
	}
	if invalidCharacter {
		messages = append(messages, lettersMessage)
	}
	if spaceCount > 1 {
		messages = append(messages, spaceMessage)
	}
	if hyphenCount > 1 {
		messages = append(messages, hyphenMessage)
	}
	if invalidSeparator {
		messages = append(messages, separatorMessage)
	}
	if len(messages) == 0 {
		lower := strings.ToLower(normalized)
		if lower == "server" || lower == "system" ||
			strings.HasPrefix(lower, "server ") ||
			strings.HasPrefix(lower, "system ") ||
			!contentfilter.NameAllowed(normalized) {
			messages = append(messages, ErrBlocked.Error())
		}
	}
	return messages
}

// Normalize applies the single character-name policy used by account creation
// and later in-world renaming.
func Normalize(name string) (string, error) {
	normalized := strings.TrimSpace(name)
	messages := ValidationMessages(name)
	if len(messages) > 0 {
		switch messages[0] {
		case ErrLength.Error():
			return "", ErrLength
		case ErrBlocked.Error():
			return "", ErrBlocked
		default:
			return "", ErrFormat
		}
	}

	values := []rune(normalized)
	capitalizeNextLetter := true
	preserveNextLetterCase := false
	for index, value := range values {
		switch {
		case unicode.IsLetter(value):
			if capitalizeNextLetter {
				values[index] = unicode.ToUpper(value)
			} else if !preserveNextLetterCase {
				values[index] = unicode.ToLower(value)
			}
			capitalizeNextLetter = false
			preserveNextLetterCase = false
		case value == ' ':
			capitalizeNextLetter = true
			preserveNextLetterCase = false
		case value == '-':
			preserveNextLetterCase = true
		}
	}

	normalized = string(values)
	return normalized, nil
}
