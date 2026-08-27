// Package officialscript contains reviewed, reproducible translations of
// recovered native behavior. They are import inputs; PostgreSQL remains the
// canonical runtime and editable representation.
package officialscript

import (
	"encoding/json"

	"github.com/brynnb/new-yokosuka-server/internal/scriptevent"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type catalogEntry struct {
	name  string
	build func() store.OfficialYarnImport
}

var catalog = []catalogEntry{
	{name: "d000-hato", build: d000Hato},
	{name: "jomo-goro-telephone", build: jomoGoroTelephone},
	{name: "d000-telephone-book", build: d000TelephoneBook},
	{name: "d000-door-61-closed-check", build: d000Door61ClosedCheck},
	{name: "d000-door-51-closed-check", build: d000Door51ClosedCheck},
	{name: "d000-door-1-closed-check", build: d000Door1ClosedCheck},
}

func mustPreviewFixture(fixture scriptevent.PreviewFixture) json.RawMessage {
	if err := scriptevent.ValidatePreviewFixture(fixture); err != nil {
		panic("invalid built-in script test fixture: " + err.Error())
	}
	encoded, err := json.Marshal(fixture)
	if err != nil {
		panic("encode built-in script test fixture: " + err.Error())
	}
	return encoded
}

func Lookup(name string) (store.OfficialYarnImport, bool) {
	for _, entry := range catalog {
		if entry.name == name {
			return entry.build(), true
		}
	}
	return store.OfficialYarnImport{}, false
}

// Names returns every reviewed built-in import in deterministic deployment
// order. Callers still resolve each definition through Lookup so this catalog
// remains the single source of truth.
func Names() []string {
	names := make([]string, len(catalog))
	for index, entry := range catalog {
		names[index] = entry.name
	}
	return names
}
