package playername

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
		err   error
	}{
		{input: "alex", want: "Alex"},
		{input: "aLEX", want: "Alex"},
		{input: "  aLEX sMITH  ", want: "Alex Smith"},
		{input: "mary-jane", want: "Mary-jane"},
		{input: "mary-JANE", want: "Mary-Jane"},
		{input: "jEAN-lUC pICARD", want: "Jean-luc Picard"},
		{input: "Al", err: ErrLength},
		{input: "alex   smith", err: ErrFormat},
		{input: "alex van dyke", err: ErrFormat},
		{input: "mary-jane-watson", err: ErrFormat},
		{input: "-alex", err: ErrFormat},
		{input: "alex-", err: ErrFormat},
		{input: "alex- smith", err: ErrFormat},
		{input: "alex2", err: ErrFormat},
		{input: "o'brien", err: ErrFormat},
		{input: "Ryo", err: ErrBlocked},
		{input: "Ine-san", err: ErrBlocked},
	}
	for _, current := range tests {
		t.Run(current.input, func(t *testing.T) {
			got, err := Normalize(current.input)
			if got != current.want || !errors.Is(err, current.err) {
				t.Fatalf(
					"Normalize(%q) = %q, %v; want %q, %v",
					current.input, got, err, current.want, current.err,
				)
			}
		})
	}
}

func TestValidationMessagesReportsEveryBrokenRule(t *testing.T) {
	got := ValidationMessages("-ab_cd ef gh-ij")
	want := []string{
		"Names can only contain letters, spaces, and hyphens.",
		"Names can contain at most one space.",
		"Names can contain at most one hyphen.",
		"Spaces and hyphens must be between letters.",
	}
	if len(got) != len(want) {
		t.Fatalf("ValidationMessages() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("ValidationMessages()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}
