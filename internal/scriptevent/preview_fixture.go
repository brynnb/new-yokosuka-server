package scriptevent

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	maxPreviewFixtureBytes    = 64 * 1024
	maxPreviewFixtureValues   = 256
	maxPreviewFixtureChoices  = 128
	maxPreviewIdentifierRunes = 160
)

// ValidatePreviewFixture bounds the temporary world state accepted by both
// saved fixtures and one-off previews. It deliberately validates structure,
// not game-specific identifier guesses; canonical identifiers remain the
// compiler and repository catalog's responsibility.
func ValidatePreviewFixture(fixture PreviewFixture) error {
	encoded, err := json.Marshal(fixture)
	if err != nil {
		return errors.New("preview fixture must contain finite JSON values")
	}
	if len(encoded) > maxPreviewFixtureBytes {
		return fmt.Errorf("preview fixture exceeds %d bytes", maxPreviewFixtureBytes)
	}
	if fixture.Yen < 0 {
		return errors.New("preview yen cannot be negative")
	}
	if fixture.GameHour != nil && (!previewFinite(*fixture.GameHour) || *fixture.GameHour < 0 || *fixture.GameHour >= 24) {
		return errors.New("preview game hour must be from 0 up to, but not including, 24")
	}
	if fixture.GameDate != nil {
		if _, err := ParseCalendarDate(*fixture.GameDate); err != nil {
			return err
		}
	}
	if err := validateFixtureKey("scene", fixture.Scene, true); err != nil {
		return err
	}
	if err := validateBoolMap("flags", fixture.Flags); err != nil {
		return err
	}
	if err := validateFloatMap("progress", fixture.Progress); err != nil {
		return err
	}
	if len(fixture.Inventory) > maxPreviewFixtureValues {
		return errors.New("preview inventory has too many entries")
	}
	for key, quantity := range fixture.Inventory {
		if err := validateFixtureKey("inventory", key, false); err != nil {
			return err
		}
		if quantity < 0 {
			return fmt.Errorf("preview inventory %q cannot be negative", key)
		}
	}
	if err := validateBoolMap("actor presence", fixture.ActorPresence); err != nil {
		return err
	}
	if err := validateNestedFloatMap("actor states", fixture.ActorStates); err != nil {
		return err
	}
	if err := validateNestedBoolMap("actor bounds", fixture.ActorBounds); err != nil {
		return err
	}
	if err := validateBoolMap("object existence", fixture.ObjectExistence); err != nil {
		return err
	}
	if len(fixture.ActivityResults) > maxPreviewFixtureValues {
		return errors.New("preview activity results have too many entries")
	}
	for key, value := range fixture.ActivityResults {
		if err := validateFixtureKey("activity result", key, false); err != nil {
			return err
		}
		if utf8.RuneCountInString(value) > 160 {
			return fmt.Errorf("preview activity result %q is too long", key)
		}
	}
	if len(fixture.RandomIntegers) > maxPreviewFixtureChoices {
		return errors.New("preview has too many random integers")
	}
	if len(fixture.OptionSelections) > maxPreviewFixtureChoices {
		return errors.New("preview has too many option selections")
	}
	for _, selection := range fixture.OptionSelections {
		if selection < 0 {
			return errors.New("preview option selections cannot be negative")
		}
	}
	return nil
}

func previewFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateFixtureKey(label, value string, optional bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if optional {
			return nil
		}
		return fmt.Errorf("preview %s contains an empty identifier", label)
	}
	if utf8.RuneCountInString(value) > maxPreviewIdentifierRunes {
		return fmt.Errorf("preview %s identifier is too long", label)
	}
	return nil
}

func validateBoolMap(label string, values map[string]bool) error {
	if len(values) > maxPreviewFixtureValues {
		return fmt.Errorf("preview %s has too many entries", label)
	}
	for key := range values {
		if err := validateFixtureKey(label, key, false); err != nil {
			return err
		}
	}
	return nil
}

func validateFloatMap(label string, values map[string]float64) error {
	if len(values) > maxPreviewFixtureValues {
		return fmt.Errorf("preview %s has too many entries", label)
	}
	for key, value := range values {
		if err := validateFixtureKey(label, key, false); err != nil {
			return err
		}
		if !previewFinite(value) {
			return fmt.Errorf("preview %s %q must be finite", label, key)
		}
	}
	return nil
}

func validateNestedFloatMap(label string, values map[string]map[string]float64) error {
	if len(values) > maxPreviewFixtureValues {
		return fmt.Errorf("preview %s has too many actors", label)
	}
	count := 0
	for actor, states := range values {
		if err := validateFixtureKey(label, actor, false); err != nil {
			return err
		}
		count += len(states)
		if count > maxPreviewFixtureValues {
			return fmt.Errorf("preview %s has too many entries", label)
		}
		if err := validateFloatMap(label, states); err != nil {
			return err
		}
	}
	return nil
}

func validateNestedBoolMap(label string, values map[string]map[string]bool) error {
	if len(values) > maxPreviewFixtureValues {
		return fmt.Errorf("preview %s has too many actors", label)
	}
	count := 0
	for actor, states := range values {
		if err := validateFixtureKey(label, actor, false); err != nil {
			return err
		}
		count += len(states)
		if count > maxPreviewFixtureValues {
			return fmt.Errorf("preview %s has too many entries", label)
		}
		if err := validateBoolMap(label, states); err != nil {
			return err
		}
	}
	return nil
}
