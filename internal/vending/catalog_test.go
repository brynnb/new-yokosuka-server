package vending

import (
	"testing"
)

func TestManifestMatchesOriginalShenmueCatalog(t *testing.T) {
	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		key  string
		name string
	}{
		{"jet_cola", "Jet Cola"},
		{"fruda_orange", "Fruda Orange"},
		{"fruda_grape", "Fruda Grape"},
		{"jet_soda", "Jet Soda"},
		{"bell_woods_coffee", "Bell Wood's Coffee Original Blend"},
	}
	products := catalog.Products()
	if len(products) != len(want) {
		t.Fatalf("product count = %d, want %d", len(products), len(want))
	}
	for index, expected := range want {
		if products[index].Key != expected.key ||
			products[index].Name != expected.name {
			t.Fatalf("product %d = %#v, want %#v", index, products[index], expected)
		}
	}
	if catalog.UnitPrice() != 100 {
		t.Fatalf("unit price = %d, want 100", catalog.UnitPrice())
	}
	if catalog.Prize().Key != "winning_can" {
		t.Fatalf("prize = %#v", catalog.Prize())
	}
}

func TestManifestContainsEveryPlacedMachine(t *testing.T) {
	catalog := MustLoad()
	manifest := catalog.Manifest()
	if len(manifest.Machines) != 14 {
		t.Fatalf("machine count = %d, want 14", len(manifest.Machines))
	}
	counts := map[string]int{}
	for _, machine := range manifest.Machines {
		counts[machine.WorldID]++
		if resolved, ok := catalog.Machine(machine.ID); !ok || resolved != machine {
			t.Fatalf("machine %q did not round-trip", machine.ID)
		}
	}
	want := map[string]int{
		"sakuragaoka": 1,
		"dobuita":     5,
		"mfsy":        2,
		"ma00":        2,
		"ma00race":    4,
	}
	for worldID, expected := range want {
		if counts[worldID] != expected {
			t.Fatalf("%s machine count = %d, want %d", worldID, counts[worldID], expected)
		}
	}
}

func TestProximityAndWinningDraw(t *testing.T) {
	catalog := MustLoad()
	machine := catalog.Manifest().Machines[0]
	if !catalog.IsNear(
		machine,
		machine.WorldID,
		machine.Position[0]+machine.InteractionRadius,
		machine.Position[2],
	) {
		t.Fatal("point on interaction boundary should be accepted")
	}
	if catalog.IsNear(
		machine,
		"dobuita",
		machine.Position[0],
		machine.Position[2],
	) {
		t.Fatal("wrong world should be rejected")
	}
	if catalog.IsWinningDraw(0) != true ||
		catalog.IsWinningDraw(1) != false ||
		catalog.IsWinningDraw(10) != false {
		t.Fatal("winning draw is not exactly one in ten")
	}
}
