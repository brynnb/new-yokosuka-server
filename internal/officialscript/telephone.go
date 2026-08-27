package officialscript

import (
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func jomoGoroTelephone() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:        "original-jomo-goro-telephone",
		Title:       "Hazuki residence — Goro's missed-work telephone call",
		Description: "Reviewed Yarn translation of JOMO command 39 and SA1093. The native gate, one-time arming latch, three dialogue variants, exact voice IDs, cameras, and timed telephone presentation sequences retain source-level provenance.",
		Summary:     "On story disc three, after native JOMO bank-2 bit 410 is set and before bit 420, arm bit 415 on the first scheduler pass. Later passes ring the telephone and play Goro's first, second, or repeat missed-work call according to bits 820 and 821.",
		NativeSources: []store.NativeSourceReference{
			{
				Role:    "room-program",
				Locator: "disc1/SCENE/01/JOMO/MAPINFO.BIN#0x8aafc>command39>0x7c2a8",
				Hash:    "4af865fbb62a06e917d6625149f4bcf77fa6a6bba19b08b8433440f96712a2b9",
			},
			{
				Role:    "dialogue-archive",
				Locator: "disc3/SCENE/03/STREAM/SA1093.AFS",
				Hash:    "970cf23cb0df835cf0c2cde35341847cd79ee60b3a23440c298613c708f47fdc",
			},
		},
		NativeDialogueRegions: []store.NativeDialogueRegionReference{
			{Disc: 1, Area: "JOMO", ExecutableTargetIndex: 317, RegionStartFileOffset: 0x7c670, Ownership: "translated", EvidenceLocator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/jomo-goro-telephone-vertical-slice.json#selection.branches[0]"},
			{Disc: 1, Area: "JOMO", ExecutableTargetIndex: 318, RegionStartFileOffset: 0x7cdda, Ownership: "translated", EvidenceLocator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/jomo-goro-telephone-vertical-slice.json#selection.branches[1]"},
			{Disc: 1, Area: "JOMO", ExecutableTargetIndex: 319, RegionStartFileOffset: 0x7d45a, Ownership: "translated", EvidenceLocator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/jomo-goro-telephone-vertical-slice.json#selection.branches[2]"},
		},
		SourceText: `title: Start
tags: original disc3 JOMO SA1093 incoming-telephone
---
<<if progress_value("native.story.disc") != 3>>
    <<jump Pass>>
<<endif>>
<<if !flag_set("native.jomo.free_conversation.bank2.bit410")>>
    <<jump Pass>>
<<endif>>
<<if flag_set("native.jomo.free_conversation.bank2.bit420")>>
    <<jump Pass>>
<<endif>>

<<if !flag_set("native.jomo.free_conversation.bank2.bit415")>>
    <<set_flag "native.jomo.free_conversation.bank2.bit415">>
    <<jump Pass>>
<<endif>>

<<play_sequence "jomo.telephone.ring.sa1093">>
<<start_camera "jomo.telephone.camera.3031">>
<<if !flag_set("native.jomo.free_conversation.bank2.bit820")>>
    <<jump FirstCall>>
<<elseif !flag_set("native.jomo.free_conversation.bank2.bit821")>>
    <<jump SecondCall>>
<<else>>
    <<jump RepeatCall>>
<<endif>>
===

title: FirstCall
tags: original disc3 JOMO SA1093 first
---
<<play_sequence "jomo.telephone.answer.sa1093.first">>
<<start_camera "jomo.telephone.camera.3032">>
Ryo: Hello? #voice:SA1093A001 #speaker:AKIR
Goro: Hey Bro! It’s Goro. #voice:SA1093B001 #speaker:GORO
Ryo: Yo. #voice:SA1093A002 #speaker:AKIR
<<start_camera "jomo.telephone.camera.3033">>
Goro: Don’t \{yo\} me! #voice:SA1093B002 #speaker:GORO
Goro: Where the hell were you[br/]yesterday? #voice:SA1093B003 #speaker:GORO
Ryo: Sorry. #voice:SA1093A003 #speaker:AKIR
Goro: I lied to the foreman[br/]and covered for you. #voice:SA1093B004 #speaker:GORO
Goro: But, you’ve got to come today. #voice:SA1093B005 #speaker:GORO
<<start_camera "jomo.telephone.camera.3034">>
Goro: 12 noon at Warehouse No. 1. #voice:SA1093B006 #speaker:GORO
Ryo: Gotcha. #voice:SA1093A004 #speaker:AKIR
Goro: See you then. #voice:SA1093B007 #speaker:GORO
<<play_sequence "jomo.telephone.hangup.sa1093.first">>
<<set_flag "native.jomo.free_conversation.bank2.bit820">>
<<stop_camera>>
<<complete>>
===

title: SecondCall
tags: original disc3 JOMO SA1093 second
---
<<play_sequence "jomo.telephone.answer.sa1093.later">>
<<start_camera "jomo.telephone.camera.3035">>
Ryo: Hello? #voice:SA1093A001 #speaker:AKIR
Goro: Hey Bro! It’s Goro. #voice:SA1093B001 #speaker:GORO
Ryo: Oh. #voice:SA1093A005 #speaker:AKIR
<<start_camera "jomo.telephone.camera.3036">>
Goro: Don’t \{Oh\} me! #voice:SA1093B008 #speaker:GORO
Goro: You’re coming today, right Bro? #voice:SA1093B009 #speaker:GORO
Ryo: Yeah. #voice:SA1093A006 #speaker:AKIR
Goro: 12 noon at Warehouse No. 1. #voice:SA1093B006 #speaker:GORO
<<start_camera "jomo.telephone.camera.3037">>
Ryo: Gotcha. #voice:SA1093A004 #speaker:AKIR
Goro: See you then. #voice:SA1093B007 #speaker:GORO
<<play_sequence "jomo.telephone.hangup.sa1093.later">>
<<set_flag "native.jomo.free_conversation.bank2.bit821">>
<<stop_camera>>
<<complete>>
===

title: RepeatCall
tags: original disc3 JOMO SA1093 repeat
---
<<play_sequence "jomo.telephone.answer.sa1093.later">>
<<start_camera "jomo.telephone.camera.3035">>
Ryo: Hello? #voice:SA1093A001 #speaker:AKIR
Goro: Hey Bro! It’s Goro. #voice:SA1093B001 #speaker:GORO
Ryo: Oh. #voice:SA1093A005 #speaker:AKIR
<<start_camera "jomo.telephone.camera.3036">>
Goro: Don’t \{Oh\} me! #voice:SA1093B008 #speaker:GORO
Goro: Come on Bro! Get with it! #voice:SA1093B011 #speaker:GORO
Ryo: Sorry... #voice:SA1093A007 #speaker:AKIR
Goro: 12 noon, Warehouse No. 1. #voice:SA1093B010 #speaker:GORO
<<start_camera "jomo.telephone.camera.3037">>
Ryo: Gotcha. #voice:SA1093A004 #speaker:AKIR
Goro: You’d better be there! #voice:SA1093B012 #speaker:GORO
<<play_sequence "jomo.telephone.hangup.sa1093.later">>
<<stop_camera>>
<<complete>>
===

title: Pass
tags: original disc3 JOMO SA1093
---
<<pass_trigger>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "automatic", Area: "JOMO", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{
			{Name: "Arm incoming call", Description: "Disc-three prerequisite is met; the first scheduler pass sets bit 415 and passes to lower-priority behavior.", StartNode: "Start", Fixture: goroTelephoneFixture()},
			{Name: "First missed-work call", Description: "The call is armed and neither call-history bit is set, selecting SA1093's first branch.", StartNode: "Start", Fixture: goroTelephoneFixture("native.jomo.free_conversation.bank2.bit415")},
			{Name: "Second missed-work call", Description: "The first-call history bit is set and the second is clear, selecting SA1093's second branch.", StartNode: "Start", Fixture: goroTelephoneFixture("native.jomo.free_conversation.bank2.bit415", "native.jomo.free_conversation.bank2.bit820")},
			{Name: "Repeat missed-work call", Description: "Both call-history bits are set, selecting SA1093's repeat branch without another durable history write.", StartNode: "Start", Fixture: goroTelephoneFixture("native.jomo.free_conversation.bank2.bit415", "native.jomo.free_conversation.bank2.bit820", "native.jomo.free_conversation.bank2.bit821")},
			{Name: "Finished — pass trigger", Description: "The recovered completion bit 420 is set, so the automatic candidate declines without presentation.", StartNode: "Start", Fixture: goroTelephoneFixture("native.jomo.free_conversation.bank2.bit420")},
		},
	}
}

func goroTelephoneFixture(extraFlags ...string) []byte {
	flags := map[string]bool{"native.jomo.free_conversation.bank2.bit410": true}
	for _, flag := range extraFlags {
		flags[flag] = true
	}
	return mustPreviewFixture(scriptevent.PreviewFixture{
		Scene: "JOMO", Progress: map[string]float64{"native.story.disc": 3}, Flags: flags,
	})
}
