package scriptcontent

import "testing"

func TestValidateYarnTriggers(t *testing.T) {
	nodes := []CompiledNode{{Title: "Hato"}, {Title: "Telephone"}}
	triggers, diagnostics := ValidateYarnTriggers([]Trigger{
		{NodeID: " Hato ", Kind: "talk", Area: "D000", Actor: "HATO", Priority: 20},
		{NodeID: "Telephone", Kind: "use", Area: "D000", Object: "hazuki.telephone"},
	}, nodes)
	if len(diagnostics) != 0 || len(triggers) != 2 {
		t.Fatalf("triggers=%#v diagnostics=%#v", triggers, diagnostics)
	}
	if triggers[0].NodeID != "Hato" || triggers[0].Configuration == nil {
		t.Fatalf("trigger was not normalized: %#v", triggers[0])
	}
}

func TestValidateYarnTriggersFailsClosed(t *testing.T) {
	nodes := []CompiledNode{{Title: "Start"}}
	tests := []Trigger{
		{NodeID: "Missing", Kind: "enter", Area: "D000"},
		{NodeID: "Start", Kind: "talk", Area: "D000"},
		{NodeID: "Start", Kind: "use", Area: "D000", Object: "not valid"},
		{NodeID: "Start", Kind: "nearby", Area: "D000"},
	}
	for _, trigger := range tests {
		_, diagnostics := ValidateYarnTriggers([]Trigger{trigger}, nodes)
		if len(diagnostics) == 0 {
			t.Fatalf("trigger unexpectedly valid: %#v", trigger)
		}
	}
}
