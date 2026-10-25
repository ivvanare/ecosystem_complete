# api — melZone Go service

The sole owner of the HTTP API and of writes. Go replaces the former Laravel
scaffolding (`api-core/`), which contained no product code.

This change lands the runtime only. No domain or security endpoint exists yet.

## Requirements

- Go 1.27
- PostgreSQL 15 (`melia`, schemas `seg` + `core`), Redis 7, RabbitMQ 3
- Or Docker for the whole stack

## Run

```bash
cp .env.example .env      # then export the values, the service reads the environment
go run ./cmd/api
```

With Docker (from the repository root):

```bash
docker compose up --build
```

## Endpoints

The HTTP contract lives in **[`openapi.yaml`](openapi.yaml)** (OpenAPI 3.1): 19 operations
covering health, auth, identity and the admin RBAC surface. It is the source of truth for
the mobile app and the admin panel; new endpoints update it in the same commit.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health/live` | Liveness. Never touches a dependency. `200 {"status":"ok"}` |
| `GET` | `/health/ready` | Readiness. Probes postgres + redis (required) and rabbitmq (optional). `200` or `503` |
| any | anything else | `404 {"error":"not found"}` |

`/health/ready` statuses: `ok` (all required probes answered), `degraded` (a required
probe answered but an optional one failed), `unavailable` (a required probe failed).
Both non-`ok` states return `503`.

## Configuration

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL DSN |
| `REDIS_ADDR` | yes | — | `host:port` |
| `HTTP_ADDR` | no | `:8080` | listen address |
| `RABBITMQ_URL` | no | empty | when empty, the rabbitmq probe is omitted |
| `CORS_ALLOWED_ORIGINS` | no | empty | comma-separated browser origins; empty disables CORS |
| `MAIL_QUEUE` | no | `melia.mail` | durable queue the API publishes mail to |
| `SMTP_ADDR` | no | `mailpit:1025` | SMTP target used by the worker |
| `MAIL_FROM` | no | `no-reply@melia.test` | envelope sender |
| `DB_INIT_DIR` | no | `/app/database/init` | directory holding `NN-*.sql` |
| `DB_AUTO_MIGRATE` | no | `true` | apply pending schema files at startup |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |

Missing required variables fail startup with the variable name in the error. There is no
fallback to a default database: a service that silently connects to the wrong database is
worse than one that refuses to start.

## Schema bootstrap

`database/init/NN-*.sql` is the single source of truth for the schema, and the service is
the only thing that applies it. The files are **copied into the image** at
`/app/database/init` — not bind-mounted — so the container user's uid never has to match
a host directory; rebuild the image after editing the DDL. The PostgreSQL container creates the database
(`POSTGRES_DB=melia`) and nothing else; there is no `docker-entrypoint-initdb.d` mount,
because two appliers of the same DDL cannot coexist — the entrypoint runs on a fresh
volume, the service runs on every start, and the second one would hit
`schema "seg" already exists`.

At startup the service applies the pending files and records each in
`seg.tb_schema_migration`, so restarts are no-ops.

Two adaptations are deliberate:

- `database/provision/01-create-database.sql` is not part of the set. A service
  connected to `melia` cannot create it; it exists for a manual bootstrap against a
  bare server.
- psql meta-commands (`\connect ...`) are stripped. They are psql directives, not SQL.

When the ledger is empty but `seg.tb_user` already exists — a volume created before the
ledger, or a manual `psql` run — the service **adopts** the schema: it records every file
as applied without executing it, logs a warning, and points at
`database/verify_base_model.sql`. Adoption is gated on that table, so a database without
it is never adopted and a failed application stops startup loudly. Adoption cannot tell a
complete schema from a half-built one that still has `seg.tb_user`, which is exactly why
the drift script exists.

Set `DB_AUTO_MIGRATE=false` when another tool owns the schema.

## Signup and mail

`POST /v1/auth/signup` writes the pending registration to `seg.tb_signup_request` and
publishes one message to the `melia.mail` queue. A separate process (`cmd/worker`) consumes
it and delivers it over SMTP. Keeping them apart means a slow mail server cannot hold an
HTTP request.

In development the SMTP target is **Mailpit** (`docker compose up`): it accepts everything
and shows it at <http://localhost:8025>. Nothing leaves the machine.

The code is never stored in clear: the row keeps `tx_code_hash` (SHA-256) and the mail
carries the only copy of the digits. If the queue is unreachable the request fails with
`500` rather than pretending a code was sent.

## Tests

```bash
go test ./...
```

No test needs a database, Redis, or RabbitMQ: probes and the migration ledger are
interfaces, and the tests pass fakes.

## Layout

```
cmd/api/            entrypoint: config, wiring, signals, graceful shutdown
internal/config/    environment parsing and validation
internal/httpapi/   router and health handlers
internal/db/        pgx pool, retry, schema bootstrap
internal/probe/     postgres, redis, rabbitmq readiness probes
```
