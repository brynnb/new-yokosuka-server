# Deployment

This repository builds one static Go server binary. PostgreSQL remains an
external durable service, and a TLS reverse proxy exposes `/ws`, `/api/*`, and
`/healthz` on the same origin as the browser client.

## Build

Run tests and build for Linux:

```sh
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -o new-yokosuka-server ./cmd/server
```

If Yarn scripting is enabled, publish `tools/yarn-compiler` for the deployment
target and set `YARN_COMPILER_PATH` to its executable.

## Host layout

The provided systemd example expects:

```text
/opt/new-yokosuka-server/
├── activity/
├── server.env
├── new-yokosuka-server
└── yarn-compiler/              # optional
```

Create a dedicated unprivileged user and writable activity directory, install
the binary and private environment file, then adapt and install
`deploy/new-yokosuka-server.service`.

The service's `DATABASE_URL` account must be able to apply the embedded baseline
and later forward migrations. Back up PostgreSQL before replacing a production
binary whose migration set is newer than the deployed version.

## Reverse proxy

Use `deploy/Caddyfile.server.example` inside the site's HTTPS block or
configure equivalent WebSocket-aware proxy rules. Preserve the browser's
original `Origin` header; the server validates it against `ALLOWED_ORIGINS`.

## Verification

After every release:

1. confirm the systemd service is active;
2. request `/healthz` and require HTTP 200 with database status true;
3. inspect startup logs for migration, manifest, compiler, and configuration
   failures;
4. verify login, character selection, and a WebSocket connection from the live
   browser origin; and
5. verify current PostgreSQL migration state and the activity-log write path.

Do not commit `server.env`, database credentials, Discord bridge secrets,
admin keys, activity logs, or built binaries.
