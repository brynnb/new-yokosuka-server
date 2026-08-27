package realtime

import (
	"context"
	"errors"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/store"
)

const positionFlushInterval = 2 * time.Second

type LocationSaver interface {
	SaveLocation(context.Context, int64, store.Location, time.Duration) error
}

// SyncAccountLocations durably reconciles location updates that belong to an
// account before an HTTP character-list read. pendingLocations covers the
// narrow refresh window after a WebSocket is removed from the live client map
// but before its disconnect flush completes.
func (h *Hub) SyncAccountLocations(ctx context.Context, accountID int64) error {
	h.mu.RLock()
	saver := h.locationSaver
	clients := make([]*Client, 0)
	seen := make(map[*Client]struct{})
	collect := func(client *Client) {
		if client == nil || !client.persistent || client.accountID != accountID {
			return
		}
		if _, exists := seen[client]; exists {
			return
		}
		seen[client] = struct{}{}
		clients = append(clients, client)
	}
	for _, client := range h.clients {
		collect(client)
	}
	for _, client := range h.pendingLocations {
		collect(client)
	}
	h.mu.RUnlock()

	var syncErrors []error
	for _, client := range clients {
		if err := client.flushLocation(ctx, saver); err != nil {
			syncErrors = append(syncErrors, err)
		}
	}
	return errors.Join(syncErrors...)
}

func (h *Hub) SetLocationSaver(saver LocationSaver) {
	h.mu.Lock()
	h.locationSaver = saver
	if vendingStore, ok := saver.(VendingStore); ok {
		h.vendingStore = vendingStore
	}
	h.mu.Unlock()
}

func (h *Hub) RunPersistence(ctx context.Context) {
	ticker := time.NewTicker(positionFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.FlushLocations(context.Background())
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			h.FlushLocations(shutdownCtx)
			cancel()
			return
		}
	}
}

func (h *Hub) FlushLocations(ctx context.Context) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		if client.persistent {
			clients = append(clients, client)
		}
	}
	saver := h.locationSaver
	h.mu.RUnlock()
	for _, client := range clients {
		if err := client.flushLocation(ctx, saver); err != nil {
			h.logf("position save failed for character %d: %v", client.characterID, err)
		}
	}
}

func (h *Hub) flushClientLocation(client *Client) {
	if client == nil || !client.persistent {
		return
	}
	h.mu.RLock()
	saver := h.locationSaver
	h.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := client.flushLocation(ctx, saver); err != nil {
		h.logf("position save failed for character %d: %v", client.characterID, err)
	}
}
