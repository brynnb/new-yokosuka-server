# Authoritative world state

`internal/worldstate` owns the shared accelerated calendar, time-of-day phase,
season, and weather published to HTTP and WebSocket consumers. Lighting,
textures, map-layer visibility, and all other rendering policy belong to the
client and are not defined here.

## Clock model

One Shenmue day is fixed at 96 real minutes, matching four real minutes per
in-game hour. The playable interval is 08:30 through 23:30, so one simulated
calendar day advances after 60 real minutes. The calendar begins at
1986-06-09 08:30 UTC.

`WORLD_EPOCH_UNIX_MS` is a real Unix timestamp in milliseconds. It anchors the
first simulated day; if absent or invalid, process startup time is used. A
future epoch clamps elapsed time to zero. The epoch is process configuration,
not persisted in PostgreSQL, so stable time across restarts requires an
explicit value.

The day-length constant cannot be overridden by environment configuration.

## Published snapshot

`protocol.WorldState` contains:

- server and game timestamps;
- epoch and day length;
- day start, day end, number, and progress;
- time-of-day name and index;
- season name and index;
- weather name and index; and
- a revision incremented by explicit state changes.

Time-of-day names are `day`, `sunset`, `evening`, and `night`. The phase is
derived from the current game clock. Season is `summer` or `winter`. Weather is
`clear`, `overcast`, `rain`, or `snow`.

## Distribution and mutation

`GET /api/world-state` returns the current snapshot. The WebSocket welcome
message includes the same shape. The `Manager` in `internal/worldstate` checks
once per second and broadcasts `world_state` when the day, time-of-day phase,
season, or revision changes.

`PATCH /api/world-state` accepts:

```json
{"gameSecond": 43200}
```

The value must be between 08:30:00 inclusive and 23:30:00 exclusive. A
successful change preserves the current calendar day, recalculates the epoch,
increments the revision, broadcasts immediately, and resets scheduled NPC
selection.

The PATCH handler currently has no built-in authentication. Production reverse
proxies should deny it to untrusted callers until server-side operator
authorization is added.

Season and weather are validated only at startup in the current process. There
is no HTTP endpoint for changing them at runtime.

## Tests

Clock behavior is covered by `internal/worldstate/clock_test.go`, HTTP validation
by `internal/httpapi/world_state_test.go`, and startup configuration by
`cmd/server/main_test.go`.
