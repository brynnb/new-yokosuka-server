package officialscript

import (
	"fmt"

	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func d000Door61ClosedCheck() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:          "original-d000-door-61-closed-check",
		Title:         "Dobuita — closed storefront door 61",
		Description:   "Reviewed Yarn translation of D000 target 532. The native calendar and hour branches, uniform three-way line selection, XMPT alignment, player motion, phase-timed sound cues, and five voice lines retain exact source provenance.",
		Summary:       "At logical door 61, align Ryo for the storefront check and choose one of the original locked/closed lines according to whether the date is before April 1 and whether the time is before 05:00.",
		SourceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#selector61>target532>0x7557c",
		SourceHash:    "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e",
		NativeDialogueRegions: []store.NativeDialogueRegionReference{{
			Disc: 1, Area: "D000", ExecutableTargetIndex: 532,
			RegionStartFileOffset: 0x7557c, Ownership: "translated",
			EvidenceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x7557c; https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/actor-xmpt-operation-evidence.json",
		}},
		SourceText: `title: Start
tags: original disc1 D000 door61 SA1088
---
<<play_sequence "d000.door.61.closed-check.start">>
<<declare $line = 0>>
<<set $line = random_integer(1, 3)>>
<<if game_date_on_or_after(4, 1)>>
    <<if $line == 1>>
        Ryo: I can't get in. #voice:SA1088A001 #speaker:AKIR
    <<elseif $line == 2>>
        Ryo: It's closed. #voice:SA1088A002 #speaker:AKIR
    <<else>>
        Ryo: The door's locked. #voice:SA1088A003 #speaker:AKIR
    <<endif>>
<<elseif game_hour() >= 5>>
    <<if $line == 1>>
        Ryo: It's closed. #voice:SA1088A002 #speaker:AKIR
    <<elseif $line == 2>>
        Ryo: It's not open yet. #voice:SA1088A004 #speaker:AKIR
    <<else>>
        Ryo: The door's locked. #voice:SA1088A003 #speaker:AKIR
    <<endif>>
<<else>>
    <<if $line == 1>>
        Ryo: It's not open now. #voice:SA1088A005 #speaker:AKIR
    <<elseif $line == 2>>
        Ryo: It's closed. #voice:SA1088A002 #speaker:AKIR
    <<else>>
        Ryo: The door's locked. #voice:SA1088A003 #speaker:AKIR
    <<endif>>
<<endif>>
<<play_sequence "d000.door.61.closed-check.finish">>
<<complete>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "use", Area: "D000",
			Object: "d000.door.61", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{
			door61Fixture("April or later — can't get in", 4, 1, 12, 1),
			door61Fixture("April or later — closed", 4, 1, 12, 2),
			door61Fixture("April or later — locked", 4, 1, 12, 3),
			door61Fixture("Before April after 05:00 — closed", 3, 31, 5, 1),
			door61Fixture("Before April after 05:00 — not open yet", 3, 31, 5, 2),
			door61Fixture("Before April after 05:00 — locked", 3, 31, 5, 3),
			door61Fixture("Before 05:00 — not open now", 3, 31, 4, 1),
			door61Fixture("Before 05:00 — closed", 3, 31, 4, 2),
			door61Fixture("Before 05:00 — locked", 3, 31, 4, 3),
		},
	}
}

func door61Fixture(name string, month, day int, hour float64, line int64) store.OfficialScriptTestFixture {
	date := fmt.Sprintf("1986-%02d-%02d", month, day)
	return store.OfficialScriptTestFixture{
		Name:        name,
		Description: "Exercises one exact native calendar, hour, and random-selector route.",
		StartNode:   "Start",
		Fixture: mustPreviewFixture(scriptevent.PreviewFixture{
			Scene: "D000", GameDate: &date, GameHour: &hour,
			RandomIntegers: []int64{line},
		}),
	}
}
