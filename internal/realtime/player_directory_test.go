package realtime

import (
	"testing"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

func TestPlayerDirectoryReportsPlayersAcrossWorlds(t *testing.T) {
	rig := newTestRig(t, 10)
	first, firstWelcome := rig.connect(t)
	second, secondWelcome := rig.connect(t)
	readClientCount(t, first, 2)

	sendPresence(t, first, "dobuita", 1, nil, 0)
	var snapshot protocol.Snapshot
	readJSON(t, first, &snapshot)
	readPlayerEntered(t, second, firstWelcome.Self.ID, "dobuita")

	sendPresence(t, second, "yamanose", 1, nil, 0)
	readJSON(t, second, &snapshot)
	readPlayerEntered(t, first, secondWelcome.Self.ID, "yamanose")

	if err := first.WriteJSON(protocol.NewHeader(protocol.TypePlayerDirectoryRequest)); err != nil {
		t.Fatal(err)
	}
	var directory protocol.PlayerDirectory
	readJSON(t, first, &directory)
	if directory.Type != protocol.TypePlayerDirectory ||
		len(directory.Players) != 2 {
		t.Fatalf("unexpected player directory: %#v", directory)
	}
	worlds := make(map[string]string, len(directory.Players))
	for _, player := range directory.Players {
		worlds[player.ID] = player.WorldID
	}
	if worlds[firstWelcome.Self.ID] != "dobuita" ||
		worlds[secondWelcome.Self.ID] != "yamanose" {
		t.Fatalf("directory lost player locations: %#v", directory.Players)
	}
}
