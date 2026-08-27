package realtime

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

func TestArcadeHighScoreUpdatesArcadeRoomAndAnnouncesGlobally(t *testing.T) {
	hub := newTestRig(t, 10).hub
	arcadeClient := newClient(
		hub,
		nil,
		"arcade-player",
		"Arcade Player",
		ConnectionMetadata{},
	)
	dobuitaClient := newClient(
		hub,
		nil,
		"dobuita-player",
		"Dobuita Player",
		ConnectionMetadata{},
	)
	hub.mu.Lock()
	hub.clients[arcadeClient.id] = arcadeClient
	hub.clients[dobuitaClient.id] = dobuitaClient
	hub.rooms["arcade"] = map[string]*Client{
		arcadeClient.id: arcadeClient,
	}
	hub.rooms["dobuita"] = map[string]*Client{
		dobuitaClient.id: dobuitaClient,
	}
	hub.mu.Unlock()

	achievedAt := time.UnixMilli(1_750_000_000_123)
	hub.PublishArcadeHighScore(
		"darts-1",
		88.5,
		"Nozomi Harasaki",
		achievedAt,
	)

	var scoreEvent protocol.ArcadeHighScoreEvent
	if err := json.Unmarshal(<-arcadeClient.send, &scoreEvent); err != nil {
		t.Fatal(err)
	}
	if scoreEvent.Type != protocol.TypeArcadeHighScore ||
		scoreEvent.MachineID != "darts-1" ||
		scoreEvent.Score != 88.5 ||
		scoreEvent.PlayerName != "Nozomi Harasaki" ||
		scoreEvent.AchievedAt != achievedAt.UnixMilli() {
		t.Fatalf("unexpected arcade score event: %#v", scoreEvent)
	}

	var arcadeAnnouncement protocol.SystemMessage
	if err := json.Unmarshal(
		<-arcadeClient.send,
		&arcadeAnnouncement,
	); err != nil {
		t.Fatal(err)
	}
	var dobuitaAnnouncement protocol.SystemMessage
	if err := json.Unmarshal(
		<-dobuitaClient.send,
		&dobuitaAnnouncement,
	); err != nil {
		t.Fatal(err)
	}
	want := "Nozomi Harasaki set a new high score of 88.5 in Darts Seven (Board 2)!"
	if arcadeAnnouncement.Type != protocol.TypeSystemMessage ||
		arcadeAnnouncement.Text != want ||
		dobuitaAnnouncement.Type != protocol.TypeSystemMessage ||
		dobuitaAnnouncement.Text != want {
		t.Fatalf(
			"unexpected announcements: arcade=%#v dobuita=%#v",
			arcadeAnnouncement,
			dobuitaAnnouncement,
		)
	}
	select {
	case unexpected := <-dobuitaClient.send:
		t.Fatalf("non-arcade player received score event: %s", unexpected)
	default:
	}
}
