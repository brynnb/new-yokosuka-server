package realtime

import (
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

func TestPersistentCharacterCanUseConnectionScopedAppearance(t *testing.T) {
	rig := newTestRig(t, 10)
	client := &Client{
		id:          "player-1",
		name:        "Ryo",
		accountID:   7,
		characterID: 11,
		avatarID:    "ryo",
		persistent:  true,
		send:        make(chan []byte, 8),
		done:        make(chan struct{}),
	}
	rig.hub.clients[client.id] = client

	if !rig.hub.HandlePresence(client, protocol.Presence{
		Header:    protocol.NewHeader(protocol.TypePresence),
		WorldID:   "exterior",
		AvatarID:  "ine",
		Movement:  "idle",
		Sequence:  1,
		VehicleQW: 1,
	}) {
		t.Fatal("avatar-changing presence was rejected")
	}

	if client.avatarID != "ine" {
		t.Fatalf("client avatar = %q, want ine", client.avatarID)
	}
	state := rig.hub.presences[client.id].state
	if state.AvatarID != "ine" || state.CharacterID != "11" {
		t.Fatalf("unexpected authoritative player state: %#v", state)
	}
}

func TestPresenceRejectsUnknownAvatar(t *testing.T) {
	if _, ok := validatePresence(protocol.Presence{
		WorldID:   "exterior",
		AvatarID:  "not-a-real-avatar",
		Movement:  "idle",
		Sequence:  1,
		VehicleQW: 1,
	}); ok {
		t.Fatal("presence accepted an unknown avatar")
	}
}

func TestPresenceAcceptsGeneratedShenmueOneAvatar(t *testing.T) {
	if _, ok := validatePresence(protocol.Presence{
		WorldID:   "exterior",
		AvatarID:  "s1-hos-l",
		Movement:  "idle",
		Sequence:  1,
		VehicleQW: 1,
	}); !ok {
		t.Fatal("presence rejected an allowlisted Shenmue I avatar")
	}
}
