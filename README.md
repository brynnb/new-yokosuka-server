# New Yokosuka Server

New Yokosuka is a fan-made online recreation of the world of *Shenmue*.
*Shenmue* is a story-driven adventure game first released for the Sega
Dreamcast in 1999. It follows a young man named Ryo Hazuki as he explores 1980s
Japan. This repository contains the server that lets New Yokosuka players share
that world and saves their progress.

The server provides account and character persistence, authenticated
WebSockets, world-state broadcasts, scheduled NPC simulation, chat, economy,
vending, arcade scores, and the server-authoritative Yarn event runtime.

The browser client is maintained separately and not yet source-available. The transport contract is a versioned JSON protocol
over same-origin HTTP and WebSocket endpoints; see
[the architecture guide](docs/multiplayer-server.md) for the current authority
boundaries and route inventory.

## Requirements

- Docker with Docker Compose for the quickest setup; or
- Go 1.24.2 or newer and PostgreSQL for a manual setup.
- Optional: .NET 9 SDK for Yarn authoring, compilation, and scripted events.

MySQL and SQLite are not supported.

## Quick start

Clone the repository and run:

```sh
docker compose up --build
```

Compose starts PostgreSQL, waits for it to become ready, builds the server, and
creates the complete empty database automatically. The server is then available
at `http://localhost:8080`; `GET /healthz` reports database readiness.

The database is kept in a Docker volume between runs. To discard all local
server data and create a fresh empty database again:

```sh
docker compose down --volumes
```

The credentials in `compose.yaml` are local-development defaults only.

## Manual setup

Create a database and local environment file:

```sh
createdb new_yokosuka
cp server.env.example server.env
```

Edit `DATABASE_URL`, then start the server:

```sh
set -a
. ./server.env
set +a
go run ./cmd/server
```

Keep `COOKIE_SECURE=false` for plain-HTTP local development. Production should
serve the API behind HTTPS with `COOKIE_SECURE=true` and an explicit
`ALLOWED_ORIGINS` list.

The server embeds `internal/store/migrations/001_baseline.sql` and applies it
automatically to a new database. Future schema changes begin at migration 002.

The activity log defaults to `activity/events.jsonl`. Both that directory and
the private `server.env` file are ignored by Git.

## Build and test

```sh
go test ./...
go build -o new-yokosuka-server ./cmd/server
```

Compiler-backed integration tests are enabled when
`NEW_YOKOSUKA_YARN_COMPILER` points to the Yarn compiler executable. Ordinary
tests do not require .NET or PostgreSQL; database integration tests activate
only when their documented environment variables are configured.

## Yarn scripting

The bridge under `tools/yarn-compiler` pins the official Yarn Spinner compiler
and runtime. Build it with:

```sh
dotnet publish tools/yarn-compiler/NewYokosuka.YarnCompiler.csproj \
  --configuration Release \
  --output .build/yarn-compiler
export YARN_COMPILER_PATH="$PWD/.build/yarn-compiler/NewYokosuka.YarnCompiler"
```

With PostgreSQL running, import the reviewed built-in scripts with:

```sh
go run ./cmd/script-import -builtin all
```

When `YARN_COMPILER_PATH` is absent, the rest of the server remains available
but Yarn authoring, preview, and server-authoritative scripted events are
disabled explicitly.

## Generated server data

Runtime manifests are embedded in the Go binary. Their distilled source inputs
live in `data/source`, and dependency-free Node scripts regenerate them:

```sh
npm run generate
```

The command regenerates and verifies:

- scheduled NPC runtime and transition-audit manifests;
- timed player-access rules;
- vending-machine definitions; and
- the NPC avatar allowlist.

These inputs are reverse-engineering research records, not redistributed game
assets. See [the data notice](data/README.md).

## Repository layout

- `cmd/server`: production server process.
- `cmd/script-import`: reviewed and recovered script importer.
- `cmd/npc-audit`: scheduled-NPC audit command.
- `internal`: application packages, embedded migrations, and runtime data.
- `data/source`: distilled inputs for generated runtime manifests.
- `tools/generate`: deterministic server-data generators.
- `tools/yarn-compiler`: pinned .NET Yarn compiler/runtime bridge.
- `docs`: architecture and gameplay-authority documentation.
- `deploy`: example systemd and Caddy configuration.

## Documentation

The [documentation index](docs/README.md) links the maintained server guides:
architecture and interfaces, scheduled NPCs, world state, scripted events, and
deployment. Extraction research, client rendering, and narrative canon are
intentionally not duplicated in this repository.

## Deployment

Build a static Linux binary with:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o new-yokosuka-server ./cmd/server
```

The examples in `deploy` expect installation under
`/opt/new-yokosuka-server`. Review paths, user names, environment values, TLS,
database backups, and reverse-proxy policy before using them in production.

## License

New Yokosuka Server source code is available under the [MIT License](LICENSE).
That license does not grant rights to Shenmue, its trademarks, or underlying
game content.
