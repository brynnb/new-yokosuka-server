package scriptcontent

import "testing"

func argument(argumentType, value string) CompiledArgument {
	return CompiledArgument{Type: argumentType, IsStatic: true, Value: &value}
}

func TestAnalyzeCallsExtractsTypedDependencies(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	calls := []CompiledCall{
		{Kind: "function", Name: "flag_set", Node: "Start", Arguments: []CompiledArgument{argument("string", "hato.met")}},
		{Kind: "command", Name: "set_progress", Node: "Start", Arguments: []CompiledArgument{argument("string", "hato.stage"), argument("number", "2")}},
		{Kind: "command", Name: "increment_progress", Node: "Start", Arguments: []CompiledArgument{argument("string", "hato.stage"), argument("number", "1")}},
		{Kind: "command", Name: "set_flag", Node: "Start", Arguments: []CompiledArgument{argument("string", "hato.met")}},
		{Kind: "command", Name: "start_camera", Node: "Start", Arguments: []CompiledArgument{argument("string", "d000.hato.camera.2950")}},
	}
	analysis, diagnostics := AnalyzeCalls(calls, registry)
	if len(diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
	want := []Dependency{
		{Access: "read", Kind: "flag", Identifier: "hato.met"},
		{Access: "write", Kind: "flag", Identifier: "hato.met"},
		{Access: "write", Kind: "progress", Identifier: "hato.stage"},
	}
	if len(analysis.Dependencies) != len(want) {
		t.Fatalf("dependencies = %#v, want %#v", analysis.Dependencies, want)
	}
	for index := range want {
		if analysis.Dependencies[index] != want[index] {
			t.Fatalf("dependency %d = %#v, want %#v", index, analysis.Dependencies[index], want[index])
		}
	}
	wantIdentifiers := []IdentifierUsage{
		{Kind: "camera", Identifier: "d000.hato.camera.2950"},
		{Kind: "flag", Identifier: "hato.met"},
		{Kind: "progress", Identifier: "hato.stage"},
	}
	if len(analysis.Identifiers) != len(wantIdentifiers) {
		t.Fatalf("identifiers = %#v, want %#v", analysis.Identifiers, wantIdentifiers)
	}
	for index := range wantIdentifiers {
		if analysis.Identifiers[index] != wantIdentifiers[index] {
			t.Fatalf("identifier %d = %#v, want %#v", index, analysis.Identifiers[index], wantIdentifiers[index])
		}
	}
}

func TestAnalyzeCallsFailsClosed(t *testing.T) {
	registry, err := Registry()
	if err != nil {
		t.Fatal(err)
	}
	value := "hato.met"
	tests := []struct {
		name string
		call CompiledCall
		code string
	}{
		{name: "unknown", call: CompiledCall{Kind: "command", Name: "guess_hato"}, code: "NYC0001"},
		{name: "wrong kind", call: CompiledCall{Kind: "function", Name: "set_flag"}, code: "NYC0002"},
		{name: "wrong count", call: CompiledCall{Kind: "command", Name: "set_progress", Arguments: []CompiledArgument{argument("string", "hato.stage")}}, code: "NYC0004"},
		{name: "dynamic command", call: CompiledCall{Kind: "command", Name: "set_flag", Arguments: []CompiledArgument{{Type: "string"}}}, code: "NYC0005"},
		{name: "bare string", call: CompiledCall{Kind: "command", Name: "set_flag", Arguments: []CompiledArgument{{Type: "bare", IsStatic: true, Value: &value}}}, code: "NYC0006"},
		{name: "dynamic identifier query", call: CompiledCall{Kind: "function", Name: "flag_set", Arguments: []CompiledArgument{{Type: "string"}}}, code: "NYC0007"},
		{name: "unknown closed capability", call: CompiledCall{Kind: "command", Name: "start_camera", Arguments: []CompiledArgument{argument("string", "invented.camera")}}, code: "NYC0008"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, diagnostics := AnalyzeCalls([]CompiledCall{test.call}, registry)
			if len(diagnostics) == 0 || diagnostics[0].Code != test.code {
				t.Fatalf("diagnostics = %#v, want first code %s", diagnostics, test.code)
			}
		})
	}
}
