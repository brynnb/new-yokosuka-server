package worldstate

import (
	"context"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/protocol"
)

type Manager struct {
	clock *Clock
}

func NewManager(clock *Clock) *Manager {
	return &Manager{clock: clock}
}

func (m *Manager) Snapshot() protocol.WorldState {
	return m.clock.Snapshot()
}

func (m *Manager) SetGameSecond(gameSecond int) (protocol.WorldState, error) {
	return m.clock.SetGameSecond(gameSecond)
}

func (m *Manager) Run(ctx context.Context, onBoundary func(protocol.WorldState)) {
	previous := m.Snapshot()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next := m.Snapshot()
			if next.DayNumber != previous.DayNumber ||
				next.TimeOfDayIndex != previous.TimeOfDayIndex ||
				next.SeasonIndex != previous.SeasonIndex ||
				next.Revision != previous.Revision {
				onBoundary(next)
			}
			previous = next
		}
	}
}
