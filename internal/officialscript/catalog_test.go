package officialscript

import "testing"

func TestCatalogNamesResolveUniqueDefinitions(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("official script catalog is empty")
	}
	seenNames := make(map[string]bool, len(names))
	seenSlugs := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" || seenNames[name] {
			t.Fatalf("invalid or duplicate catalog name %q", name)
		}
		seenNames[name] = true
		definition, found := Lookup(name)
		if !found {
			t.Fatalf("catalog name %q does not resolve", name)
		}
		if definition.Slug == "" || seenSlugs[definition.Slug] {
			t.Fatalf("catalog name %q resolved invalid or duplicate slug %q", name, definition.Slug)
		}
		seenSlugs[definition.Slug] = true
	}
	if _, found := Lookup("all"); found {
		t.Fatal("all is a CLI selector, not a built-in definition")
	}
}
