package npc

import (
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npcdata"
)

func BenchmarkFullManifestTick(b *testing.B) {
	manifest, err := npcdata.Load()
	if err != nil {
		b.Fatal(err)
	}
	engine, err := NewEngine(manifest)
	if err != nil {
		b.Fatal(err)
	}
	server := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	game := time.Date(1986, 6, 9, 16, 0, 0, 0, time.UTC)
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		offset := time.Duration(index) * 100 * time.Millisecond
		if _, err := engine.Tick(
			TickTime{
				ServerTime: server.Add(offset),
				GameTime:   game.Add(offset * 15),
				DayNumber:  0,
				DayLength:  96 * time.Minute,
			},
			nil,
		); err != nil {
			b.Fatal(err)
		}
	}
}
