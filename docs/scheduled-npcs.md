# Scheduled NPC simulation

The server evaluates scheduled NPC programs, resolves their authoritative state,
persists runtime delays, and broadcasts room-scoped changes. Extraction and
reverse-engineering methodology is deliberately out of scope here; this guide
documents only the distilled inputs and runtime behavior owned by this repo.

## Data boundary

The generator reads:

- `data/source/scheduled-actors.json`;
- `data/source/scheduled-actor-motions.json`; and
- transition classifications implemented by `tools/generate/lib`.

Run:

```sh
npm run generate:npcs
```

This writes `internal/npcdata/manifest.json` and
`internal/npcdata/transition-audit.json`. Both files are embedded in the Go
binary by `internal/npcdata/manifest.go`. The current generated manifest
contains 234 actor definitions and the audit classifies 1,304 route
transitions.

The source records are intentionally sufficient to regenerate the server
outputs without access to a disc image, extracted assets, or a client checkout.
If new extraction work changes a schedule, update the distilled source record
here and regenerate; do not hand-edit embedded outputs.

## Startup and lifecycle

At startup the server:

1. loads and validates the embedded manifest;
2. compiles actor variants and journeys into the runtime interpreter;
3. restores saved checkpoints from `npc_runtime_state`;
4. evaluates an initial tick against the current world clock; and
5. starts the 100 ms simulation loop and five-second checkpoint loop.

Failure to load, compile, restore, or initially evaluate the manifest stops
server startup. A final checkpoint flush runs during graceful shutdown.

## Schedule evaluation

`internal/npc` selects a schedule variant using the actor's selector slots,
calendar bounds, and available story conditions. The interpreter resolves
authored operations into a state containing world, position, facing, movement,
animation, interaction, local-object, attachment, and visual information.

The fixed world clock runs a Shenmue day in 96 real minutes. NPC movement is
evaluated against game time while obstruction delays and avoidance movement use
real elapsed time. Changing `/api/world-state` resets schedule state and forces
an immediate reevaluation.

Not every authored operation implies a visible walking actor. Lifecycle and
world-residency operations may hide an actor, place it in another world, attach
local objects, or select secondary-controller behavior. Those decisions are
made server-side and represented in `npc.State`.

## Obstruction and avoidance

Each tick receives authoritative player positions from the realtime hub.
Players in vehicles use a larger collision radius. The engine detects narrow
forward-lane blockers among players and NPCs, gives players priority, and uses
stable actor IDs to break NPC/NPC symmetry.

A blocked NPC first waits, then takes a deterministic sidestep, travels around
the obstruction, and rejoins its authored route. Accumulated schedule delay is
checkpointed so a restart does not erase that delay; transient avoidance phase
and position are recomputed rather than stored in PostgreSQL.

## Replication

NPC state is partitioned by world. Entering a world includes its current NPC
snapshot. Thereafter, `npc_state` messages carry meaningful changes and
periodic walking corrections; `npc_removed` removes an actor that becomes
hidden or leaves the room.

The server does not broadcast all 100 ms simulation ticks. Clients interpolate
or extrapolate between authoritative states, but rendering choices are a client
responsibility and are not specified here.

## Persistence

The baseline schema creates `npc_runtime_state`. Each row stores the NPC ID,
day number, accumulated delay seconds, revision, and update timestamp. The
database row is limited operational state, not a replacement for source
schedules.

## Validation

Use focused checks after changing NPC inputs or runtime behavior:

```sh
npm run generate:npcs
go test ./internal/npc ./internal/npcdata ./internal/realtime
go run ./cmd/npc-audit
```

`internal/npcdata/transition_audit_test.go` requires every generated route
boundary to have an explicit classification. The audit command reports visible
same-world discontinuities for research and review; its output is diagnostic,
not a rendered visual test.

The full repository check remains:

```sh
npm run generate
go test ./...
```
