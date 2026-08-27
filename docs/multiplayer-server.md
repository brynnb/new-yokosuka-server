# Server architecture and interfaces

This document describes the standalone server as implemented in this
repository. Wire payload fields are defined by `internal/protocol`, database
shape by `internal/store/migrations`, and route registration by
`cmd/server/main.go`.

## Process boundary

One Go process owns:

- guest and registered accounts, session cookies, and character selection;
- character location, inventory, progression, dialogue state, and play time;
- authenticated WebSocket connections, rooms, presence, chat, and player
  directory state;
- authoritative world time, scheduled NPC simulation, timed travel decisions,
  vending, shared forklift/cargo state, and arcade scores;
- the community script repository and, when configured, Yarn compilation,
  preview, and live scripted events; and
- migrations, activity logging, health checks, and optional Discord chat
  bridging.

PostgreSQL is required at startup. The process exits if it cannot connect,
apply migrations, load embedded runtime manifests, restore NPC checkpoints, or
open the activity log.

The browser is a separate consumer. It renders server state and must use the
protocol version and payloads in `internal/protocol/messages.go`; browser-side
implementation details are not part of this repository's contract.

## Startup and shutdown

`cmd/server/main.go` performs startup in this order:

1. connect to PostgreSQL and apply embedded migrations;
2. optionally configure the Yarn compiler and runtime bridge;
3. initialize authentication and the world clock;
4. load the embedded NPC manifest and restore checkpoints;
5. open the append-only activity log and create the realtime hub;
6. optionally configure the Discord bridge;
7. register HTTP and WebSocket routes; and
8. start world, persistence, NPC, and checkpoint loops.

On `SIGINT` or `SIGTERM`, HTTP shutdown is bounded to ten seconds, pending
character locations are flushed, and NPC checkpoints receive a final save.

## Configuration

The complete production configuration surface is:

| Variable | Required | Default | Purpose |
| --- | --- | --- | --- |
| `DATABASE_URL` | yes | none | PostgreSQL connection string. |
| `HTTP_ADDR` | no | `:8080` | HTTP listen address. |
| `ALLOWED_ORIGINS` | no | local Vite origins and `https://www.newyokosuka.com` | Comma-separated CORS and WebSocket origins. Set explicitly in production. |
| `COOKIE_SECURE` | no | `true` | Adds the Secure attribute to session cookies. |
| `ADMIN_KEY` | no | empty | Token accepted by admin endpoints through `X-Admin-Token`. Empty disables successful admin authentication. |
| `MAX_CONNECTIONS` | no | `100` | Positive concurrent WebSocket limit. |
| `ACTIVITY_LOG_PATH` | no | `activity/events.jsonl` | Append-only activity log. |
| `WORLD_EPOCH_UNIX_MS` | no | process start | Real timestamp corresponding to the start of the simulated calendar. |
| `WORLD_SEASON` | no | `summer` | `summer` or `winter`. |
| `WORLD_WEATHER` | no | `clear` | `clear`, `overcast`, `rain`, or `snow`. |
| `YARN_COMPILER_PATH` | no | empty | Enables Yarn compilation, preview, and live scripted events. |
| `DISCORD_CHAT_SHARED_SECRET` | no | empty | Discord bridge authentication secret; must be set with the webhook URL. |
| `DISCORD_CHAT_WEBHOOK_URL` | no | empty | Discord outbound webhook; must be set with the shared secret. |

`server.env.example` is the editable local template. Test-only variables
beginning with `NEW_YOKOSUKA_` are not runtime server configuration.

## HTTP surface

Routes are registered in `cmd/server/main.go`. Public reads include status,
health, world state, arcade leaderboards, the script catalog, and the script
schema. Authentication and authorization requirements for mutations are
enforced by each handler, with the world-state exception called out below.

| Route | Methods | Responsibility |
| --- | --- | --- |
| `/healthz` | GET | Process and PostgreSQL readiness. |
| `/api/status` | GET | Lightweight online status. |
| `/api/auth/guest` | POST | Create or resume a guest account and issue a session. |
| `/api/auth/login` | POST | Registered-account login. |
| `/api/auth/register` | POST | Registration or guest-account upgrade. |
| `/api/auth/logout` | POST | Revoke the current session. |
| `/api/session` | GET | Return the authenticated account. |
| `/api/characters` | GET, POST | List or create characters. |
| `/api/characters/{id}` | DELETE | Soft-delete an owned character. |
| `/api/characters/{id}/state` | GET | Character, progression, and inventory snapshot. |
| `/api/characters/{id}/dialogue` | GET, PUT | Revisioned per-character dialogue state. |
| `/api/characters/{id}/use-item` | POST | Consume an owned healing item. |
| `/api/world-state` | GET, PATCH | Read the world clock or set its current game second. The PATCH route is currently not admin-protected. |
| `/api/arcade-scores` | GET, POST | Read leaderboards or submit a score. |
| `/api/scripts` and subroutes | mixed | Script listing, authoring, versions, reviews, fixtures, collaboration, moderation, and preview. |
| `/api/script-schema` | GET | Command registry and published identifier catalog. |
| `/api/admin/stats` | GET | Live connection statistics. |
| `/api/admin/growth` | GET | Account and character growth events. |
| `/api/admin/chats` | GET | Recent persisted chat. |
| `/api/admin/logs` | GET | In-memory recent server logs. |
| `/api/discord/game-chat` | POST | Inbound Discord chat, registered only when the bridge is configured. |
| `/ws` | WebSocket upgrade | Authenticated realtime session for a selected character. |

The world-state PATCH exposure is a verified implementation fact, not a
security recommendation. A public deployment should restrict it at the reverse
proxy or add server authorization before treating it as an operator control.

## Authentication and sessions

Sessions use the `new_yokosuka_session` HTTP-only, SameSite=Lax cookie. Raw
session tokens are never stored; PostgreSQL stores SHA-256 token hashes.
Sessions last 30 days. Registered passwords are bcrypt hashes. Login, guest
authentication, and registration share an in-process limit of ten attempts per
source IP per minute.

Guest identity is based on a caller-supplied opaque token after length and
printability validation. A guest can be upgraded in place to a registered
account. Accounts may own at most eight active characters.

## Realtime protocol

The WebSocket protocol version is `8`. Each message carries `v` and `type`.
The exact message set and JSON field names live in
`internal/protocol/messages.go`; changing them requires coordinated consumer
work and a protocol-version decision.

After authentication and upgrade, the server sends a welcome snapshot with the
selected character, inventory, dialogue state, recent chat, world state, and
connected-client count. Room-scoped updates include player presence, chat,
NPCs, vehicles, cargo, vending results, transition authorization, arcade high
scores, and scripted-event yields.

Inbound messages are capped at 4 KiB. The connection loop uses 20-second pings
and a 60-second pong deadline. Presence, chat, diagnostics, vending, vehicle
sound, and other state-changing messages have server-side validation or rate
limits in their respective `internal/realtime` handlers.

Character locations are accumulated from accepted presence and transition
updates, flushed to PostgreSQL every two seconds, reconciled before character
list reads, and flushed again on disconnect and shutdown.

## Persistence

`internal/store/store.go` embeds every SQL file under
`internal/store/migrations`. Startup takes a PostgreSQL advisory transaction
lock, applies unapplied files in lexical filename order, and records the full
filename in `schema_migrations`.

`001_baseline.sql` is the complete initial schema, including constraints,
indexes, functions, triggers, and the small reference-data set required by an
otherwise empty database. Future changes start with `002_...sql`.

The schema owns accounts and sessions; characters, inventory, economy and
progression events; NPC checkpoints; vending and chat history; arcade scores;
dialogue state; and the complete script authoring, review, publication, event,
trace, and per-character story-state model.

Character experience, current HP, and maximum HP are persisted. Levels are
derived from experience using ten explicit New Yokosuka thresholds. Gaining
experience never changes HP. Damage and healing clamp current HP between zero
and the character's stored maximum, and healing items use the same health
operation as future combat and scripted effects.

Migrations after the baseline are forward-only. Back up the database before
deploying a binary with newer migrations. PostgreSQL is the only supported
database.

## Package map

- `cmd/server`: process wiring, configuration, routes, and lifecycle.
- `internal/auth`: sessions, cookies, guest identity, and passwords.
- `internal/httpapi`: REST handlers and request ownership checks.
- `internal/realtime`: WebSocket lifecycle and shared-world systems.
- `internal/protocol`: versioned JSON wire structures.
- `internal/store`: PostgreSQL queries and embedded migrations.
- `internal/worldstate`: accelerated calendar, season, and weather.
- `internal/npc` and `internal/npcdata`: scheduled-NPC runtime and manifests.
- `internal/travelaccess`: timed transition rules.
- `internal/vending`: vending catalog and validation.
- `internal/scriptcontent`, `internal/scriptruntime`, and `internal/scriptevent`:
  script authoring, compiler bridge, and live execution.
- `internal/officialscript`: reviewed built-in server scripts.

## Verification

Use the root README for build commands. Ordinary `go test ./...` does not need
PostgreSQL or .NET. PostgreSQL integration tests activate with
`NEW_YOKOSUKA_TEST_DATABASE_URL`; compiler-backed tests activate with
`NEW_YOKOSUKA_YARN_COMPILER`. CI regenerates embedded data before testing so a
source/output mismatch fails the build.
