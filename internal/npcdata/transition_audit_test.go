package npcdata

import "testing"

func TestEveryRouteTransitionHasAnExplicitClassification(t *testing.T) {
	audit, err := LoadTransitionAudit()
	if err != nil {
		t.Fatal(err)
	}
	if audit.Summary.TransitionCount == 0 ||
		audit.Summary.TransitionCount != len(audit.Transitions) {
		t.Fatalf(
			"transition summary = %d, rows = %d",
			audit.Summary.TransitionCount,
			len(audit.Transitions),
		)
	}
	for _, transition := range audit.Transitions {
		if transition.ActorID == "" ||
			transition.FromRouteID == "" ||
			transition.ToRouteID == "" ||
			transition.Classification == "" {
			t.Fatalf("unclassified transition: %+v", transition)
		}
		if transition.VisibleWarp && transition.Distance <= 0.05 {
			t.Fatalf("false visible-warp classification: %+v", transition)
		}
	}
}
