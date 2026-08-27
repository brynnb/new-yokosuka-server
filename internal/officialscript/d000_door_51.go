package officialscript

import (
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func d000Door51ClosedCheck() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:          "original-d000-door-51-closed-check",
		Title:         "Dobuita — closed storefront door 51",
		Description:   "Reviewed Yarn translation of D000 target 530. The native story-sequence and hour branches, weighted line selection, XMPT alignment, player motion, phase-timed sound cues, and four voice lines retain exact source provenance.",
		Summary:       "At logical door 51, align Ryo for the storefront check and select the original closed-hours line from authoritative story sequence, time, and weighted random state.",
		SourceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#selector51>target530>0x7507c",
		SourceHash:    "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e",
		NativeSources: []store.NativeSourceReference{
			{Role: "room-program", Locator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x7507c", Hash: "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e"},
			{Role: "dialogue-archive", Locator: "disc1/SCENE/01/STREAM/SA1081.AFS", Hash: "d3a46fd1f6bdc2ec427f3395ba0daa5fa9ff1df04591629f729867e418f9cdce"},
		},
		NativeDialogueRegions: []store.NativeDialogueRegionReference{{
			Disc: 1, Area: "D000", ExecutableTargetIndex: 530,
			RegionStartFileOffset: 0x7507c, Ownership: "translated",
			EvidenceLocator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/d000-door-51-closed-check.json",
		}},
		SourceText: `title: Start
tags: original disc1 D000 door51 SA1081
---
<<play_sequence "d000.door.51.closed-check.start">>
<<declare $line = 0>>
<<set $line = random_integer(1, 4)>>
<<if progress_value("native.story.sequence") <= 202>>
    <<if $line <= 3>>
        Ryo: It's closed... #voice:SA1081A002 #speaker:AKIR
    <<else>>
        Ryo: They're not open. #voice:SA1081A003 #speaker:AKIR
    <<endif>>
<<elseif $line <= 3>>
    Ryo: It's closed... #voice:SA1081A002 #speaker:AKIR
<<elseif game_hour() > 4 && game_hour() < 10>>
    Ryo: It's not open yet. #voice:SA1081A005 #speaker:AKIR
<<else>>
    Ryo: It's closed already. #voice:SA1081A006 #speaker:AKIR
<<endif>>
<<play_sequence "d000.door.51.closed-check.finish">>
<<complete>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "use", Area: "D000",
			Object: "d000.door.51", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{
			door51Fixture("Sequence 202 — weighted closed", 202, 12, 1),
			door51Fixture("Sequence 202 — alternate closed", 202, 12, 4),
			door51Fixture("After 202 — weighted closed", 203, 12, 1),
			door51Fixture("After 202 — not open yet", 203, 5, 4),
			door51Fixture("After 202 — closed already", 203, 10, 4),
		},
	}
}

func door51Fixture(name string, storySequence int, hour float64, quartile int64) store.OfficialScriptTestFixture {
	return store.OfficialScriptTestFixture{
		Name:        name,
		Description: "Exercises one exact native story-sequence, hour, and weighted-random route.",
		StartNode:   "Start",
		Fixture: mustPreviewFixture(scriptevent.PreviewFixture{
			Scene: "D000", GameHour: &hour,
			Progress:       map[string]float64{"native.story.sequence": float64(storySequence)},
			RandomIntegers: []int64{quartile},
		}),
	}
}
