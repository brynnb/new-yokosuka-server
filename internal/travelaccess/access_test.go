package travelaccess

import (
	"testing"
	"time"
)

func gameTime(hour, minute, second, nanosecond int) time.Time {
	return time.Date(1986, time.June, 9, hour, minute, second, nanosecond, time.UTC)
}

func TestEmbeddedDobuitaRuleUsesExactProvenAssociation(t *testing.T) {
	catalog := MustLoad()
	rule, ok := catalog.Rule("d000-door-30-to-dcha-entry-0")
	if !ok {
		t.Fatal("selector-30 DCHA access rule is missing")
	}
	if rule.Source.WorldID != "dobuita" || rule.Source.Area != "D000" ||
		rule.Source.DoorSelector != 30 || rule.Source.Layer != 14 {
		t.Fatalf("unexpected source: %#v", rule.Source)
	}
	if rule.Destination.WorldID != "dcha" || rule.Destination.Area != "DCHA" ||
		rule.Destination.Entry != 0 {
		t.Fatalf("unexpected destination: %#v", rule.Destination)
	}
	if rule.OpenWindow.StartMinute != 8*60 ||
		rule.OpenWindow.EndMinute != 19*60+30 {
		t.Fatalf("unexpected opening window: %#v", rule.OpenWindow)
	}
	if !catalog.ControlsDestination("dcha") || catalog.ControlsDestination("dbyo") {
		t.Fatal("controlled destination set does not match the reviewed rule")
	}
	manifest := catalog.Manifest()
	if len(manifest.PresentationOnlyAssociations) != 1 ||
		manifest.PresentationOnlyAssociations[0].DoorSelector != 63 ||
		manifest.PresentationOnlyAssociations[0].Destination != nil {
		t.Fatalf("selector 63 must remain presentation-only: %#v", manifest.PresentationOnlyAssociations)
	}
}

func TestOpeningWindowIsHalfOpenAtExactBoundaries(t *testing.T) {
	rule, _ := MustLoad().Rule("d000-door-30-to-dcha-entry-0")
	tests := []struct {
		name string
		at   time.Time
		open bool
	}{
		{"immediately before opening", gameTime(7, 59, 59, 999_999_999), false},
		{"at opening", gameTime(8, 0, 0, 0), true},
		{"immediately before closing", gameTime(19, 29, 59, 999_999_999), true},
		{"at closing", gameTime(19, 30, 0, 0), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := rule.IsOpen(test.at); got != test.open {
				t.Fatalf("IsOpen(%s) = %t, want %t", test.at, got, test.open)
			}
		})
	}
}

func TestSidebarInteriorsAreExplicitlyAlwaysAccessible(t *testing.T) {
	catalog := MustLoad()
	want := []string{"interior", "cinema", "arcade", "djaz"}
	if got := catalog.Manifest().AlwaysAccessibleInteriorWorldIDs; len(got) != len(want) {
		t.Fatalf("always-accessible interiors = %#v, want %#v", got, want)
	}
	for index, worldID := range want {
		if got := catalog.Manifest().AlwaysAccessibleInteriorWorldIDs[index]; got != worldID {
			t.Fatalf("always-accessible interiors = %#v, want %#v", catalog.Manifest().AlwaysAccessibleInteriorWorldIDs, want)
		}
		if !catalog.IsAlwaysAccessible(worldID) {
			t.Fatalf("sidebar interior %q is not always accessible", worldID)
		}
		if catalog.ControlsDestination(worldID) {
			t.Fatalf("sidebar interior %q is still access-controlled", worldID)
		}
	}

	rule, _ := catalog.Rule("d000-door-30-to-dcha-entry-0")
	rule.Destination.WorldID = "arcade"
	if !catalog.IsRuleOpen(rule, gameTime(7, 59, 59, 0)) {
		t.Fatal("an always-accessible interior was closed by public hours")
	}
}

func TestProximityUsesTheEvidenceBackedDoorPosition(t *testing.T) {
	rule, _ := MustLoad().Rule("d000-door-30-to-dcha-entry-0")
	if !rule.WithinRange(rule.Source.Position[0]+10, rule.Source.Position[2]) {
		t.Fatal("the exact maximum interaction distance should be allowed")
	}
	if rule.WithinRange(rule.Source.Position[0]+10.001, rule.Source.Position[2]) {
		t.Fatal("an excessive distance was allowed")
	}
}
