# api — melZone Go service

The sole owner of the HTTP API and of writes. Go replaces the former Laravel
scaffolding (`api-core/`), which contained no product code.

This change lands the runtime only. No domain or security endpoint exists yet.

## Requirements

- Go 1.27
- PostgreSQL 15 (`melzone`, schemas `seg` + `core`), Redis 7, RabbitMQ 3
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
| `DB_INIT_DIR` | no | `/app/database/init` | directory holding `NN-*.sql` |
| `DB_AUTO_MIGRATE` | no | `true` | apply pending schema files at startup |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |

Missing required variables fail startup with the variable name in the error. There is no
fallback to a default database: a service that silently connects to the wrong database is
worse than one that refuses to start.

## Schema bootstrap

`database/init/NN-*.sql` stays the single source of truth for the schema — the same files
the PostgreSQL entrypoint runs. At startup the service applies the pending ones and
records each in `seg.tb_schema_migration`, so restarts are no-ops.

Two adaptations are deliberate:

- `01-create-database.sql` is skipped. A service connected to `melzone` cannot create it.
- psql meta-commands (`\connect ...`) are stripped. They are psql directives, not SQL.

Set `DB_AUTO_MIGRATE=false` when another tool owns the schema.

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
