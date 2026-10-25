# Spec: Go API Skeleton — Decommission Laravel

Source of truth for the target runtime: `database/diagram_base.txt` (CONTEXTO DE ESTA
ITERACIÓN, §7–§11). Where that file is silent, the decision is recorded in `design.md`.

## Specs

### S1 — Laravel is gone

`api-core/` no longer exists in the tree after this change. No PHP file, `composer.json`,
`artisan`, Blade template, Vite config, or Laravel-specific documentation remains, and
nothing in `docker-compose.yml` or `nginx/default.conf` references it.

### S2 — One Go service owns the HTTP API

New module at `api/` with module path `github.com/ivvanare/ecosystem_complete/api`,
declaring Go 1.27. Routing uses `net/http.ServeMux` method patterns; no third-party
router or framework is introduced.

### S3 — Configuration comes from the environment and fails fast

The service reads:

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL DSN, e.g. `postgres://user:pass@host:5432/melzone?sslmode=disable` |
| `REDIS_ADDR` | yes | — | `host:port` |
| `HTTP_ADDR` | no | `:8080` | listen address |
| `RABBITMQ_URL` | no | empty | `amqp://user:pass@host:5672/` |
| `DB_INIT_DIR` | no | `/app/database/init` | directory holding `NN-*.sql` |
| `DB_AUTO_MIGRATE` | no | `true` | apply pending schema files at startup |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |

A missing required variable or an unparseable value makes startup fail with a message
naming the offending variable, and the process exits non-zero. No silent fallback to a
default database or cache address.

### S4 — Two health endpoints with different contracts

- `GET /health/live` returns `200` with `{"status":"ok"}` unconditionally; it is a
  liveness probe and must not touch external systems.
- `GET /health/ready` runs every configured dependency probe and returns `200` when all
  required dependencies answer, `503` when at least one required dependency fails.
  The body is `{"status":"ok|degraded|unavailable","checks":[{"name":...,"status":"ok|error","required":bool}]}`.
- `postgres` and `redis` are required dependencies. `rabbitmq` is optional: when
  `RABBITMQ_URL` is empty it is omitted from the report; when set but unreachable it is
  reported with `"status":"error"` and the response becomes `503` with
  `"status":"degraded"`. A non-required failure never reports plain `200 ok`.
- Unknown paths return `404` with `{"error":"not found"}`.

### S5 — The service applies the canonical schema files

`database/init/NN-*.sql` stays the single source of truth for the schema. At startup,
when `DB_AUTO_MIGRATE` is true, the service:

1. ensures its ledger table `seg.tb_schema_migration (tx_name text primary key, dt_applied_at timestamptz not null default now())`;
2. reads `DB_INIT_DIR`, keeps files matching `NN-*.sql`, sorts them lexically;
3. skips `01-create-database.sql` — a service connected to `melzone` cannot create it;
4. strips psql meta-command lines (any line whose trimmed form starts with `\`, such as
   `\connect melzone`);
5. executes each pending file's text as one statement batch and records its filename;
6. skips files already recorded, so a restart is a no-op.

A failure stops startup with the failing filename in the error; files that already ran
stay recorded.

### S6 — Graceful shutdown

`SIGINT`/`SIGTERM` stops accepting connections and drains in-flight requests within a
10-second window, then closes the PostgreSQL pool and the Redis client before exit.

### S7 — Container image

`api/Dockerfile` is multi-stage: `golang:1.27-alpine` builds a static binary
(`CGO_ENABLED=0`), the runtime stage is `alpine` with CA certificates, runs as a
non-root user, and exposes `8080`. No source tree and no toolchain in the final image.

### S8 — Nginx proxies instead of executing PHP

`nginx/default.conf` forwards every request to the Go service (`proxy_pass`) and no
longer contains `fastcgi_pass`, `SCRIPT_FILENAME`, or a document root for PHP.

### S9 — Compose runs the Go service

The `api_core` service is replaced by `api` (`build: ./api`, `container_name:
melzone_api`), receiving `DATABASE_URL`, `REDIS_ADDR`, `RABBITMQ_URL`, `DB_INIT_DIR`
and mounting `./database/init` read-only. `database`, `redis`, `rabbitmq`, `webserver`
and `soketi` keep their current configuration.

### S10 — Documentation matches the runtime

`README.md`, `AGENTS.md` and `api/README.md` describe the Go service and the commands
that actually exist (`go build`, `go test ./...`, `docker compose up`). No instruction
to run `composer`, `artisan`, or Vite survives.

### S11 — Verification without a live database

`go build ./...`, `go vet ./...` and `go test ./...` pass with no external service
running. Unit tests cover configuration validation, both health handlers with fake
probes, and the migration loader/runner (ordering, bootstrap-file exclusion,
meta-command stripping, already-applied skipping) with fake store and executor.
