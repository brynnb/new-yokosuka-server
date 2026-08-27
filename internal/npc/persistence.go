package npc

import (
	"context"
	"log"
	"time"
)

const checkpointInterval = 5 * time.Second

func RunCheckpointPersistence(
	ctx context.Context,
	engine *Engine,
	store CheckpointStore,
	logger *log.Logger,
) {
	if engine == nil || store == nil {
		return
	}
	ticker := time.NewTicker(checkpointInterval)
	defer ticker.Stop()
	save := func(parent context.Context) {
		saveCtx, cancel := context.WithTimeout(parent, 4*time.Second)
		defer cancel()
		if err := FlushCheckpoints(saveCtx, engine, store); err != nil &&
			logger != nil {
			logger.Printf("NPC checkpoint save failed: %v", err)
		}
	}
	for {
		select {
		case <-ticker.C:
			save(context.Background())
		case <-ctx.Done():
			return
		}
	}
}

func FlushCheckpoints(
	ctx context.Context,
	engine *Engine,
	store CheckpointStore,
) error {
	if engine == nil || store == nil {
		return nil
	}
	return store.SaveNPCCheckpoints(ctx, engine.Checkpoints())
}
