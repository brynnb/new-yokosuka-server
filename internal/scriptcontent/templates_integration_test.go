package scriptcontent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptEditorStarterTemplatesCompileWithPinnedToolchain(t *testing.T) {
	compilerPath := os.Getenv("NEW_YOKOSUKA_YARN_COMPILER")
	if compilerPath == "" {
		t.Skip("NEW_YOKOSUKA_YARN_COMPILER is not set")
	}
	compiler, err := NewProcessCompiler(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	templatePath := filepath.Join("testdata", "script-templates.json")
	encoded, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	var templates []struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Summary     string `json:"summary"`
		SourceText  string `json:"sourceText"`
	}
	if err := json.Unmarshal(encoded, &templates); err != nil {
		t.Fatal(err)
	}
	if len(templates) != 3 {
		t.Fatalf("starter template count=%d, want 3", len(templates))
	}
	seen := map[string]bool{}
	for _, template := range templates {
		if strings.TrimSpace(template.ID) == "" || seen[template.ID] ||
			strings.TrimSpace(template.Title) == "" || strings.TrimSpace(template.Description) == "" ||
			strings.TrimSpace(template.Summary) == "" || strings.TrimSpace(template.SourceText) == "" {
			t.Fatalf("invalid starter template: %#v", template)
		}
		seen[template.ID] = true
		compilation, err := compiler.Compile(
			context.Background(), "template-"+template.ID+".yarn", template.SourceText,
		)
		if err != nil {
			t.Fatalf("compile %s: %v", template.ID, err)
		}
		if !compilation.Valid || len(compilation.Program) == 0 || len(compilation.Nodes) != 1 || compilation.Nodes[0].Title != "Start" {
			t.Fatalf("template %s did not compile: %#v", template.ID, compilation)
		}
	}
}
