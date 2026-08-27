package scriptcontent

import (
	"context"
	"os"
	"testing"
)

func TestOfficialYarnCompilerBridge(t *testing.T) {
	path := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if path == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, err := NewProcessCompiler(path)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := compiler.Compile(context.Background(), "valid.yarn", `title: Start
---
<<if flag_set("hato.met")>>
Ryo: Hello again.
<<else>>
Ryo: Hello.
<<set_flag "hato.met">>
<<endif>>
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if !valid.Valid || len(valid.Program) == 0 || len(valid.Lines) != 2 || len(valid.Nodes) != 1 ||
		len(valid.Analysis.Dependencies) != 2 {
		t.Fatalf("unexpected valid compilation: %#v", valid)
	}
	if valid.Analysis.Dependencies[0] != (Dependency{Access: "read", Kind: "flag", Identifier: "hato.met"}) ||
		valid.Analysis.Dependencies[1] != (Dependency{Access: "write", Kind: "flag", Identifier: "hato.met"}) {
		t.Fatalf("unexpected dependencies: %#v", valid.Analysis.Dependencies)
	}
	if len(valid.Analysis.Identifiers) != 1 || valid.Analysis.Identifiers[0] != (IdentifierUsage{Kind: "flag", Identifier: "hato.met"}) {
		t.Fatalf("unexpected identifiers: %#v", valid.Analysis.Identifiers)
	}

	invalid, err := compiler.Compile(context.Background(), "invalid.yarn", `title: Start
---
<<if true>>
Ryo: This scope is not closed.
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if invalid.Valid || len(invalid.Program) != 0 || len(invalid.Diagnostics) == 0 || invalid.Diagnostics[0].Line == 0 {
		t.Fatalf("unexpected invalid compilation: %#v", invalid)
	}

	unknown, err := compiler.Compile(context.Background(), "unknown.yarn", `title: Start
---
<<made_up_command "value">>
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Valid || len(unknown.Diagnostics) == 0 || unknown.Diagnostics[len(unknown.Diagnostics)-1].Code != "NYC0001" {
		t.Fatalf("unexpected unknown-command compilation: %#v", unknown)
	}

	unimplemented, err := compiler.Compile(context.Background(), "unimplemented.yarn", `title: Start
---
<<play_sound "telephone.ring">>
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if unimplemented.Valid || len(unimplemented.Diagnostics) == 0 ||
		unimplemented.Diagnostics[len(unimplemented.Diagnostics)-1].Code != "NYC0001" {
		t.Fatalf("unexpected unimplemented-command compilation: %#v", unimplemented)
	}

	for _, unsupportedQuery := range []string{"actor_state", "object_exists", "activity_result"} {
		source := "title: Start\n---\n<<declare $value = " + unsupportedQuery + "(\"unknown\")>>\n===\n"
		if unsupportedQuery == "actor_state" {
			source = "title: Start\n---\n<<declare $value = actor_state(\"HATO\", \"mood\")>>\n===\n"
		}
		result, err := compiler.Compile(context.Background(), unsupportedQuery+".yarn", source)
		if err != nil {
			t.Fatal(err)
		}
		if result.Valid || len(result.Diagnostics) == 0 ||
			result.Diagnostics[len(result.Diagnostics)-1].Code != "NYC0001" {
			t.Fatalf("unexpected unsupported-query %s compilation: %#v", unsupportedQuery, result)
		}
	}

	unknownCapability, err := compiler.Compile(context.Background(), "unknown-capability.yarn", `title: Start
---
<<start_camera "invented.camera">>
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if unknownCapability.Valid || len(unknownCapability.Diagnostics) == 0 ||
		unknownCapability.Diagnostics[len(unknownCapability.Diagnostics)-1].Code != "NYC0008" {
		t.Fatalf("unexpected unknown-capability compilation: %#v", unknownCapability)
	}

	dynamicIdentifier, err := compiler.Compile(context.Background(), "dynamic.yarn", `title: Start
---
<<declare $flagName = "hato.met">>
<<if flag_set($flagName)>>
Ryo: This dependency cannot be indexed safely.
<<endif>>
===
`)
	if err != nil {
		t.Fatal(err)
	}
	if dynamicIdentifier.Valid || len(dynamicIdentifier.Diagnostics) == 0 ||
		dynamicIdentifier.Diagnostics[len(dynamicIdentifier.Diagnostics)-1].Code != "NYC0007" {
		t.Fatalf("unexpected dynamic-identifier compilation: %#v", dynamicIdentifier)
	}
}
