package store

import "testing"

func TestNormalizeOfficialNativeDialogueRegions(t *testing.T) {
	regions, err := normalizeOfficialNativeDialogueRegions([]NativeDialogueRegionReference{{
		Disc: 1, Area: " d000 ", ExecutableTargetIndex: 629,
		RegionStartFileOffset: 0x7fa98, Ownership: "translated",
		EvidenceLocator: "exact#0x7fa98",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0].Ordinal != 0 || regions[0].Area != "D000" {
		t.Fatalf("normalized regions=%#v", regions)
	}

	invalid := []NativeDialogueRegionReference{
		{Disc: 0, Area: "D000", Ownership: "translated", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D00", Ownership: "translated", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", ExecutableTargetIndex: -1, Ownership: "translated", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", RegionStartFileOffset: -1, Ownership: "translated", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", Ownership: "unknown", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", Ownership: "translated", ActivityID: "not-allowed", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", Ownership: "specialized-activity-owned", EvidenceLocator: "evidence"},
		{Disc: 1, Area: "D000", Ownership: "translated"},
	}
	for index, region := range invalid {
		if _, err := normalizeOfficialNativeDialogueRegions([]NativeDialogueRegionReference{region}); err == nil {
			t.Fatalf("invalid region %d was accepted: %#v", index, region)
		}
	}

	duplicate := regions[0]
	if _, err := normalizeOfficialNativeDialogueRegions([]NativeDialogueRegionReference{duplicate, duplicate}); err == nil {
		t.Fatal("duplicate native dialogue region was accepted")
	}
}
