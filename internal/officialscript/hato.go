package officialscript

import (
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func d000Hato() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:  "original-d000-hato",
		Title: "Yoshifumi Hato — first free conversation",
		Description: "Reviewed Yarn translation of D1 D000's recovered HATO free-conversation route. " +
			"Native offsets, predicates, presentation requests, voice, and state mutation remain traceable to the evidence bundle.",
		Summary:       "When Hato's native bank-2 bit 100 is clear and the time is 07:00–18:59, stage one of the three recovered cameras, Hato's look target and Ryo's exact motions; play F1030B001; increment Hato's native dialogue-state byte; then clean up.",
		SourceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x7a17c>0x7abf4>0x8002c",
		SourceHash:    "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e",
		NativeDialogueRegions: []store.NativeDialogueRegionReference{{
			Disc: 1, Area: "D000", ExecutableTargetIndex: 629,
			RegionStartFileOffset: 0x7fa98, Ownership: "translated",
			EvidenceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x7a17c>0x7abf4>0x8002c>0x7fa98",
		}},
		SourceText: `title: Start
tags: original disc1 D000 HATO
---
<<if flag_set("native.d1.d000.free_conversation.bank2.bit100")>>
    <<jump Ineligible>>
<<endif>>
<<if game_hour() < 7 || game_hour() > 18>>
    <<jump Ineligible>>
<<endif>>
<<if !actor_in_bounds("AKIR", "d000.hato.spatial.5")>>
    <<jump Ineligible>>
<<endif>>

<<declare $camera = 0>>
<<set $camera = random_integer(1, 3)>>
<<if $camera == 1>>
    <<start_camera "d000.hato.camera.2950">>
<<elseif $camera == 2>>
    <<start_camera "d000.hato.camera.2952">>
<<else>>
    <<start_camera "d000.hato.camera.2954">>
<<endif>>
<<look_at_actor "HATO" "AKIR">>
<<play_player_motion "d000.hato.player-motion.903">>
Hato: Ain't got time for punk kids.[br/]Get out of here. #voice:F1030B001 #speaker:HATO
<<increment_progress "native.d1.character.HATO.dialogue_state" 1>>
<<play_player_motion "d000.hato.player-motion.100">>
<<clear_actor_look "HATO">>
<<stop_camera>>
<<complete>>
===

title: Ineligible
tags: original disc1 D000 HATO
---
<<pass_trigger>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "talk", Area: "D000", Actor: "HATO", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{
			{
				Name: "Eligible — camera route 1", Description: "Midday, inside Hato's recovered interaction bounds; selects native camera request 2950.", StartNode: "Start",
				Fixture: hatoFixture(12, true, nil, 1),
			},
			{
				Name: "Eligible — camera route 2", Description: "Midday, inside Hato's recovered interaction bounds; selects native camera request 2952.", StartNode: "Start",
				Fixture: hatoFixture(12, true, nil, 2),
			},
			{
				Name: "Eligible — camera route 3", Description: "Midday, inside Hato's recovered interaction bounds; selects native camera request 2954.", StartNode: "Start",
				Fixture: hatoFixture(12, true, nil, 3),
			},
			{
				Name: "Ineligible — already spoken", Description: "The recovered bank-2 bit 100 gate is already set, so this trigger passes without presentation.", StartNode: "Start",
				Fixture: hatoFixture(12, true, map[string]bool{"native.d1.d000.free_conversation.bank2.bit100": true}, 0),
			},
			{
				Name: "Ineligible — outside hours", Description: "Hato is in bounds but the native 07:00–18:59 time gate fails.", StartNode: "Start",
				Fixture: hatoFixture(20, true, nil, 0),
			},
			{
				Name: "Ineligible — outside authored bounds", Description: "Time and flags pass, but Ryo is outside the exact recovered Hato spatial record.", StartNode: "Start",
				Fixture: hatoFixture(12, false, nil, 0),
			},
		},
	}
}

func hatoFixture(hour float64, inBounds bool, flags map[string]bool, camera int64) []byte {
	fixture := scriptevent.PreviewFixture{
		Scene: "D000", GameHour: &hour, Flags: flags,
		ActorBounds: map[string]map[string]bool{"AKIR": {"d000.hato.spatial.5": inBounds}},
	}
	if camera > 0 {
		fixture.RandomIntegers = []int64{camera}
	}
	return mustPreviewFixture(fixture)
}
