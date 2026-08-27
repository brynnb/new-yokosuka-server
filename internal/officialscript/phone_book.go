package officialscript

import (
	"github.com/brynnb/new-yokosuka-server/internal/scriptcontent"
	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func d000TelephoneBook() store.OfficialYarnImport {
	return store.OfficialYarnImport{
		Slug:        "original-d000-telephone-book",
		Title:       "Dobuita telephone book",
		Description: "Reviewed Yarn entry for D000's persistent-owner TBK1 interaction. Yarn owns authoritative selection and event lifetime; the exact camera, motion, prop, SA1071 dialogue, sound, and postlude remain one registered native specialized activity rather than being approximated as generic commands.",
		Summary:     "When the player uses TBK1, run the recovered D000 telephone-book activity under the database script event owner, then complete without granting durable story state from the client acknowledgment.",
		NativeSources: []store.NativeSourceReference{
			{
				Role:    "room-program",
				Locator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x69b14>TBK1>0x6a49c",
				Hash:    "7712f3ae8c9e154b3831bc8d8af31ebc135f35d930ae50c503a65e3af34e9b7e",
			},
			{
				Role:    "dialogue-archive",
				Locator: "disc1/SCENE/01/STREAM/SA1071.AFS",
				Hash:    "7587943ce2910d652a5a1674a8fcfa285fd9f6c8d2d6be2e58e6183541418340",
			},
			{
				Role:    "motion-bank",
				Locator: "bundled/play/assets/dobuita/M_D000.MOTN",
				Hash:    "102a00b99c7d354e7c62ca712a3cbb95d32439065ef741e365d4b9dab0934991",
			},
			{
				Role:    "closed-prop",
				Locator: "bundled/play/assets/dobuita/DENS501G.CHRM",
				Hash:    "3c06acbdcc6888f5a79e4f4aa8ae6d55e0c82d1bfe9746e01c376b5da8b22c3d",
			},
			{
				Role:    "opened-prop",
				Locator: "bundled/play/assets/dobuita/DENS502G.CHRM",
				Hash:    "bc68d76e1f0a53f2bb9a5c63eabf4dfa7b9bb15e3c5096c40673074a9ee67bde",
			},
			{
				Role:    "presentation-evidence",
				Locator: "https://github.com/brynnb/ghidra-dreamcast-shenmue/blob/master/evidence/d000-phone-book-interaction.json",
				Hash:    "9e6f6ec2fa084b08c8ca14fda1a64ddb10cab4078a6c7db72863ccae0c490ebb",
			},
		},
		NativeDialogueRegions: []store.NativeDialogueRegionReference{{
			Disc: 1, Area: "D000", ExecutableTargetIndex: 482,
			RegionStartFileOffset: 0x6b06c, Ownership: "specialized-activity-owned",
			ActivityID:      "d000.telephone-book.native",
			EvidenceLocator: "disc1/SCENE/01/D000/MAPINFO.BIN#0x69b14>TBK1>0x6a49c>0x6b06c",
		}},
		SourceText: `title: Start
tags: original disc1 D000 TBK1 specialized-activity
---
<<start_activity "d000.telephone-book.native">>
<<complete>>
===
`,
		Triggers: []scriptcontent.Trigger{{
			NodeID: "Start", Kind: "use", Area: "D000", Object: "TBK1", Priority: 100,
		}},
		TestFixtures: []store.OfficialScriptTestFixture{{
			Name: "Use Dobuita telephone book", Description: "The exact D000 TBK1 object exists; preview yields the bounded native telephone-book activity and no durable reward.", StartNode: "Start",
			Fixture: mustPreviewFixture(scriptevent.PreviewFixture{
				Scene: "D000", ObjectExistence: map[string]bool{"TBK1": true},
			}),
		}},
	}
}
