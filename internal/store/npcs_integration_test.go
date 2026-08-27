package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/brynnb/new-yokosuka-server/internal/npc"
)

func TestPostgresNPCCheckpointBatchRoundTrip(t *testing.T) {
	databaseURL := os.Getenv("NEW_YOKOSUKA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEW_YOKOSUKA_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	id := fmt.Sprintf("TEST:%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = database.db.Exec(`DELETE FROM npc_runtime_state WHERE npc_id = $1`, id)
	})
	want := npc.Checkpoint{
		NPCID:            id,
		DayNumber:        9,
		AccumulatedDelay: 1200.5,
		Revision:         44,
		UpdatedAt:        time.Now().UTC().Truncate(time.Microsecond),
	}
	if err := database.SaveNPCCheckpoints(ctx, []npc.Checkpoint{want}); err != nil {
		t.Fatal(err)
	}
	checkpoints, err := database.LoadNPCCheckpoints(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range checkpoints {
		if got.NPCID != id {
			continue
		}
		if got.DayNumber != want.DayNumber ||
			got.AccumulatedDelay != want.AccumulatedDelay ||
			got.Revision != want.Revision ||
			!got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatalf("checkpoint = %#v, want %#v", got, want)
		}
		return
	}
	t.Fatalf("checkpoint %s was not loaded", id)
}
