package scriptevent

import (
	"math"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
)

func TestReviewedBoundsExactlyMatchClosedRegistryCapabilities(t *testing.T) {
	registry, err := scriptcontent.Registry()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReviewedBoundsCapabilities(registry); err != nil {
		t.Fatal(err)
	}
}

func TestReviewedHatoBoundsMatchExactNativeTransform(t *testing.T) {
	bounds := ReviewedActorBounds(
		"D000", "AKIR", 119.13939666748047, 0, 79.6144027709961,
		-9375*math.Pi*2/fullNativeTurn,
	)
	if !bounds["AKIR"]["d000.hato.spatial.5"] {
		t.Fatalf("exact Hato transform did not match: %#v", bounds)
	}
}

func TestReviewedHatoBoundsPreserveStrictFacingLimit(t *testing.T) {
	bounds := ReviewedActorBounds(
		"D000", "AKIR", 119.13939666748047, 0, 79.6144027709961,
		-(9375+16384)*math.Pi*2/fullNativeTurn,
	)
	if bounds["AKIR"]["d000.hato.spatial.5"] {
		t.Fatalf("strict native facing boundary unexpectedly matched")
	}
}

func TestReviewedBoundsDoNotInventUnknownRecords(t *testing.T) {
	if got := ReviewedActorBounds("JOMO", "AKIR", 0, 0, 0, 0); len(got) != 0 {
		t.Fatalf("unknown reviewed bounds=%#v", got)
	}
}
