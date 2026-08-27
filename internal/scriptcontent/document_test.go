package scriptcontent

import "testing"

func validDocument() Document {
	return Document{
		Schema: Schema, EntryNodeID: "start",
		Nodes: []Node{
			{ID: "start", Type: "trigger", Label: "Talk", Config: map[string]any{"kind": "talk", "area": "D000", "actor": "HATO"}},
			{ID: "gate", Type: "condition", Label: "Has clue", Config: map[string]any{"kind": "flag", "key": "knows_charlie"}},
			{ID: "write", Type: "action", Label: "Remember clue", Config: map[string]any{"kind": "set_flag", "key": "knows_charlie"}},
			{ID: "end", Type: "end", Label: "Complete", Config: map[string]any{"kind": "complete"}},
		},
		Edges: []Edge{
			{ID: "a", From: "start", To: "gate"},
			{ID: "b", From: "gate", To: "write", Port: "false"},
			{ID: "c", From: "write", To: "end"},
		},
	}
}

func TestValidateDerivesDependenciesAndTriggers(t *testing.T) {
	analysis, err := Validate(validDocument(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Triggers) != 1 || analysis.Triggers[0].Area != "D000" {
		t.Fatalf("unexpected triggers: %#v", analysis.Triggers)
	}
	if len(analysis.Dependencies) != 2 || analysis.Dependencies[0].Access != "read" || analysis.Dependencies[1].Access != "write" {
		t.Fatalf("unexpected dependencies: %#v", analysis.Dependencies)
	}
}

func TestValidateRejectsBrokenGraphReferences(t *testing.T) {
	document := validDocument()
	document.Edges[0].To = "missing"
	if _, err := Validate(document, false); err == nil {
		t.Fatal("broken edge was accepted")
	}
}

func TestPublishingRejectsUnresolvedNativeNodes(t *testing.T) {
	document := validDocument()
	document.Nodes = append(document.Nodes, Node{ID: "unknown", Type: "unresolved_native", Label: "Unknown operation", Config: map[string]any{"kind": "0x013e"}})
	if analysis, err := Validate(document, true); err == nil || len(analysis.Warnings) == 0 {
		t.Fatalf("unresolved publish result: %#v, %v", analysis, err)
	}
	if _, err := Validate(document, false); err != nil {
		t.Fatalf("draft rejected unresolved evidence: %v", err)
	}
}
