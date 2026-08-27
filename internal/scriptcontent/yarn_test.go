package scriptcontent

import (
	"strings"
	"testing"
)

func TestCanonicalYarnSourcePreservesAuthorTextAndNormalizesTransport(t *testing.T) {
	source, err := CanonicalYarnSource("title: Start\r\n---\r\nRyo: Hello\r\n===")
	if err != nil {
		t.Fatal(err)
	}
	if source != "title: Start\n---\nRyo: Hello\n===\n" {
		t.Fatalf("canonical source = %q", source)
	}
	if SourceHash(source) != SourceHash(source) || len(SourceHash(source)) != 64 {
		t.Fatal("source hash is not stable SHA-256")
	}
}

func TestCanonicalYarnSourceRejectsMissingAndOversizedSource(t *testing.T) {
	if _, err := CanonicalYarnSource(" \n\t"); err == nil {
		t.Fatal("missing source was accepted")
	}
	if _, err := CanonicalYarnSource(strings.Repeat("x", MaxYarnSourceBytes+1)); err == nil {
		t.Fatal("oversized source was accepted")
	}
}
