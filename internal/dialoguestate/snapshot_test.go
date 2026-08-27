package dialoguestate

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDefaultSnapshotMatchesNativeRuntimeLayout(t *testing.T) {
	snapshot := Default()
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 0 ||
		snapshot.ProgressIndices.DynamicCursor != 23 {
		t.Fatalf("unexpected default metadata: %#v", snapshot)
	}
	expectedBank2 := []int{2, 3, 5, 6, 7}
	for _, index := range expectedBank2 {
		if snapshot.State.Banks.Bank2[index>>3]&(1<<(index&7)) == 0 {
			t.Fatalf("free-roam bank 2 flag %d was not initialized", index)
		}
	}
	expectedBank3 := []int{
		0, 2, 4, 5, 7, 9, 10, 13, 14, 33, 34, 35, 36, 38, 39, 40, 41,
	}
	for _, index := range expectedBank3 {
		if snapshot.State.Banks.Bank3[index>>3]&(1<<(index&7)) == 0 {
			t.Fatalf("free-roam bank 3 flag %d was not initialized", index)
		}
	}
	for index := 0; index < 325; index++ {
		value := math.Float32frombits(binary.LittleEndian.Uint32(
			snapshot.Progress[index*12+8:],
		))
		if value != 9 {
			t.Fatalf("record %d metric = %f, want 9", index, value)
		}
	}
}

func TestSnapshotValidationRejectsMalformedFixedState(t *testing.T) {
	snapshot := Default()
	snapshot.Progress = snapshot.Progress[:100]
	if err := snapshot.Validate(); err == nil {
		t.Fatal("short progress table was accepted")
	}

	snapshot = Default()
	bad := "TOO-LONG"
	snapshot.ProgressIndices.DynamicIdentities[0] = &bad
	if err := snapshot.Validate(); err == nil {
		t.Fatal("invalid dynamic identity was accepted")
	}

	snapshot = Default()
	value := 256
	snapshot.Random.History[0] = &value
	if err := snapshot.Validate(); err == nil {
		t.Fatal("invalid random history was accepted")
	}

	snapshot = Default()
	snapshot.GameState.ActorBytes["TOO-LONG"] = 1
	if err := snapshot.Validate(); err == nil {
		t.Fatal("invalid native actor byte identity was accepted")
	}

	snapshot = Default()
	snapshot.GameState.ScriptBits = snapshot.GameState.ScriptBits[:4]
	if err := snapshot.Validate(); err == nil {
		t.Fatal("short persistent script bitfield was accepted")
	}
}

func TestSnapshotValidationAcceptsFourByteOrientedRunes(t *testing.T) {
	snapshot := Default()
	snapshot.GameState.ActorBytes["AéÇZ"] = 1
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("four byte-oriented runes were rejected: %v", err)
	}
}
