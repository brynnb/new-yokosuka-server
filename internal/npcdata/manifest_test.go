package npcdata

import (
	"encoding/json"
	"testing"
)

func TestGeneratedManifestLoadsWithStableRoutes(t *testing.T) {
	manifest, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Actors) != 234 {
		t.Fatalf("actor count = %d, want 234", len(manifest.Actors))
	}
	routeIDs := make(map[string]struct{})
	variantCount := 0
	for _, actor := range manifest.Actors {
		variantCount += len(actor.ScheduleVariants)
		for _, variant := range actor.ScheduleVariants {
			for _, journey := range variant.Journeys {
				for _, operation := range journey.Operations {
					if operation.Code != 1 {
						continue
					}
					var payload struct {
						Points [][]float64 `json:"points"`
					}
					if err := json.Unmarshal(operation.Payload, &payload); err != nil {
						t.Fatal(err)
					}
					if len(payload.Points) == 0 {
						continue
					}
					if operation.RouteID == "" {
						t.Fatalf("%s route at %s has no ID", actor.InstanceID, operation.FileOffset)
					}
					if _, duplicate := routeIDs[operation.RouteID]; duplicate {
						t.Fatalf("duplicate route ID %s", operation.RouteID)
					}
					routeIDs[operation.RouteID] = struct{}{}
				}
			}
		}
	}
	if variantCount != 431 {
		t.Fatalf("variant count = %d, want 431", variantCount)
	}
	if len(routeIDs) != 1620 {
		t.Fatalf("route count = %d, want all-variant route count 1620", len(routeIDs))
	}
}
