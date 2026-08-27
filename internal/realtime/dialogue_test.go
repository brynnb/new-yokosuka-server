package realtime

import (
	"encoding/json"
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/dialoguestate"
	"github.com/brynnb/new-yokosuka-server/internal/protocol"
	"github.com/brynnb/new-yokosuka-server/internal/store"
)

func TestPersistentWelcomeIncludesDialogueSnapshot(t *testing.T) {
	rig := newTestRig(t, 10)
	snapshot := dialoguestate.Default()
	snapshot.Revision = 12
	snapshot.State.Banks.Bank2[10] = 1
	client, err := rig.hub.Register(nil, ConnectionMetadata{
		AccountID:   7,
		AccountType: "registered",
		Character: &store.Character{
			ID:        42,
			Name:      "Ryo",
			AvatarKey: "ryo",
			WorldID:   "dobuita",
			CurrentHP: 20,
		},
		DialogueState: &snapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rig.hub.Unregister(client) })

	var welcome protocol.Welcome
	if err := json.Unmarshal(<-client.send, &welcome); err != nil {
		t.Fatal(err)
	}
	if welcome.DialogueState == nil ||
		welcome.DialogueState.Revision != 12 ||
		welcome.DialogueState.State.Banks.Bank2[10] != 1 {
		t.Fatalf("welcome omitted dialogue state: %#v", welcome.DialogueState)
	}
}
