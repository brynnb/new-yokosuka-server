package officialscript

import (
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const d000Door1StoryFlag = "native.d1.d000.free_conversation.bank2.bit170"

func d000Door1ClosedCheck() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:          "original-d000-door-1-closed-check",
		Title:         "Dobuita — sliding storefront door 1",
		Description:   "Reviewed Yarn translation of D000 target 531. The native bank-2 flag, opening-hour gap, uniform line selection, side-aware XMPT routes, sliding-door motion, delayed sound cues, cleanup, and five used voice lines retain exact source provenance.",
		Summary:       "At logical door 1, stage Ryo on the correct side of the sliding door and reproduce its original closed-hours response; once native bank-2 bit 170 is set, the check is silent from 10:00 through 20:59.",
		SourceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#selector1>target531>0x74874",
		SourceHash:    "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e",
		NativeSources: []store.NativeSourceReference{
			{Role: "room-program", Locator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x74874>0x74df4", Hash: "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e"},
			{Role: "dialogue-archive", Locator: "disc1/SCENE/01/STREAM/SA1080.AFS", Hash: "632c862cfd6c8aa6584c0aed55a702e79031db56485f8fa7cc459b0f8d122432"},
		},
		NativeDialogueRegions: []store.NativeDialogueRegionReference{{
			Disc: 1, Area: "D000", ExecutableTargetIndex: 531,
			RegionStartFileOffset: 0x74874, Ownership: "translated",
			EvidenceLocator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/d000-door-1-closed-check.json",
		}},
		SourceText: `title: Start
tags: original disc1 D000 door1 SA1080
---
<<declare $open_hours_enabled = false>>
<<set $open_hours_enabled = flag_set("native.d1.d000.free_conversation.bank2.bit170")>>
<<play_sequence "d000.door.1.closed-check.start">>
<<declare $line = 0>>
<<set $line = random_integer(0, 1)>>
<<if !$open_hours_enabled>>
    <<if $line == 0>>
        Ryo: They're not open. #voice:SA1080A003 #speaker:AKIR
    <<else>>
        Ryo: It's closed. #voice:SA1080A002 #speaker:AKIR
    <<endif>>
<<elseif game_hour() < 10>>
    <<if $line == 0>>
        Ryo: It's closed. #voice:SA1080A004 #speaker:AKIR
    <<else>>
        Ryo: It's not open yet. #voice:SA1080A005 #speaker:AKIR
    <<endif>>
<<elseif game_hour() >= 21>>
    <<if $line == 0>>
        Ryo: It's closed. #voice:SA1080A004 #speaker:AKIR
    <<else>>
        Ryo: It's closed already. #voice:SA1080A006 #speaker:AKIR
    <<endif>>
<<endif>>
<<play_sequence "d000.door.1.closed-check.finish">>
<<complete>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "use", Area: "D000",
			Object: "d000.door.1", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{
			door1Fixture("Before open-hours flag — not open", false, 12, 0),
			door1Fixture("Before open-hours flag — closed", false, 12, 1),
			door1Fixture("Before 10:00 — closed", true, 9, 0),
			door1Fixture("Before 10:00 — not open yet", true, 9, 1),
			door1Fixture("Open hours — silent check", true, 10, 0),
			door1Fixture("At or after 21:00 — closed", true, 21, 0),
			door1Fixture("At or after 21:00 — closed already", true, 21, 1),
		},
	}
}

func door1Fixture(name string, openHoursEnabled bool, hour float64, line int64) store.OfficialScriptTestFixture {
	flags := map[string]bool{}
	if openHoursEnabled {
		flags[d000Door1StoryFlag] = true
	}
	return store.OfficialScriptTestFixture{
		Name:        name,
		Description: "Exercises one exact native bank-2, hour, and uniform-random route.",
		StartNode:   "Start",
		Fixture: mustPreviewFixture(scriptevent.PreviewFixture{
			Scene: "D000", GameHour: &hour, Flags: flags,
			RandomIntegers: []int64{line},
		}),
	}
}
