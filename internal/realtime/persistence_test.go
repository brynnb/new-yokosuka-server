package realtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

type savedLocation struct {
	characterID int64
	location    store.Location
	playtime    time.Duration
}

type recordingLocationSaver struct {
	mu    sync.Mutex
	saves []savedLocation
}

func (s *recordingLocationSaver) SaveLocation(
	_ context.Context,
	characterID int64,
	location store.Location,
	playtime time.Duration,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves = append(s.saves, savedLocation{
		characterID: characterID,
		location:    location,
		playtime:    playtime,
	})
	return nil
}

func TestPersistentClientFlushesOnlyLatestDirtyLocation(t *testing.T) {
	client := &Client{
		persistent:       true,
		characterID:      42,
		lastPlaytimeSave: time.Now().Add(-3 * time.Second),
	}
	saver := &recordingLocationSaver{}

	client.markLocation(store.Location{WorldID: "exterior", X: 1})
	client.markLocation(store.Location{WorldID: "dobuita", X: 9, Y: 2, Z: 3, Yaw: 1.5})
	if err := client.flushLocation(context.Background(), saver); err != nil {
		t.Fatal(err)
	}
	if err := client.flushLocation(context.Background(), saver); err != nil {
		t.Fatal(err)
	}

	if len(saver.saves) != 1 {
		t.Fatalf("save count = %d, want 1", len(saver.saves))
	}
	saved := saver.saves[0]
	if saved.characterID != 42 || saved.location.WorldID != "dobuita" || saved.location.X != 9 {
		t.Fatalf("unexpected save: %#v", saved)
	}
	if saved.playtime < 2*time.Second {
		t.Fatalf("playtime = %s, want at least 2s", saved.playtime)
	}
}

func TestSyncAccountLocationsIncludesLiveAndDisconnectingZones(t *testing.T) {
	saver := &recordingLocationSaver{}
	live := &Client{
		persistent:       true,
		accountID:        7,
		characterID:      42,
		lastPlaytimeSave: time.Now(),
	}
	disconnecting := &Client{
		persistent:       true,
		accountID:        7,
		characterID:      43,
		lastPlaytimeSave: time.Now(),
	}
	otherAccount := &Client{
		persistent:       true,
		accountID:        8,
		characterID:      44,
		lastPlaytimeSave: time.Now(),
	}
	live.markLocation(store.Location{WorldID: "cinema", X: 1})
	disconnecting.markLocation(store.Location{WorldID: "s2wt00", X: 2})
	otherAccount.markLocation(store.Location{WorldID: "exterior", X: 3})
	hub := &Hub{
		clients: map[string]*Client{
			"live":  live,
			"other": otherAccount,
		},
		pendingLocations: map[string]*Client{
			"disconnecting": disconnecting,
		},
		locationSaver: saver,
	}

	if err := hub.SyncAccountLocations(context.Background(), 7); err != nil {
		t.Fatal(err)
	}

	if len(saver.saves) != 2 {
		t.Fatalf("save count = %d, want 2", len(saver.saves))
	}
	savedWorlds := map[string]bool{}
	for _, saved := range saver.saves {
		savedWorlds[saved.location.WorldID] = true
	}
	if !savedWorlds["cinema"] || !savedWorlds["s2wt00"] {
		t.Fatalf("saved worlds = %#v, want cinema and s2wt00", savedWorlds)
	}
	if otherAccount.dirtyLocation == nil {
		t.Fatal("sync flushed a different account's location")
	}
}
