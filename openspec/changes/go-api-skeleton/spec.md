# Spec: Go API Skeleton — Decommission Laravel

Source of truth for the target runtime: `database/diagram_base.txt` (CONTEXTO DE ESTA
ITERACIÓN, §7–§11). Where that file is silent, the decision is recorded in `design.md`.

## Specs

### S1 — Laravel is gone

`api-core/` no longer exists in the tree after this change. No PHP file, `composer.json`,
`artisan`, Blade template, Vite config, or Laravel-specific documentation remains, and
nothing in `docker-compose.yml` references it.

### S2 — One Go service owns the HTTP API

New module at `api/` with module path `github.com/ivvanare/ecosystem_complete/api`,
declaring Go 1.27. Routing uses `net/http.ServeMux` method patterns; no third-party
router or framework is introduced.

### S3 — Configuration comes from the environment and fails fast

The service reads:

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL DSN, e.g. `postgres://user:pass@host:5432/melia?sslmode=disable` |
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

`database/init/NN-*.sql` stays the single source of truth for the schema, and the service
is its only applier: the PostgreSQL container creates the database and applies no DDL. The
files are copied into the runtime image at `/app/database/init`, so the container user's
uid never has to match a host directory. At startup, when `DB_AUTO_MIGRATE` is true, the
service:

1. ensures its ledger table `seg.tb_schema_migration (tx_name text primary key, dt_applied_at timestamptz not null default now())`;
2. reads `DB_INIT_DIR`, keeps files matching `NN-*.sql`, sorts them lexically;
3. never executes `01-create-database.sql` — a service connected to `melia` cannot
   create it. The provisioning script lives in `database/provision/`; the loader still
   guards against a copy reappearing in `DB_INIT_DIR`;
4. strips psql meta-command lines (any line whose trimmed form starts with `\`, such as
   `\connect melia`);
5. executes each pending file's text as one statement batch and records its filename;
6. skips files already recorded, so a restart is a no-op;
7. when the ledger is empty but `seg.tb_user` already exists (a volume predating the
   ledger, or a manual `psql` run), records every file as applied **without executing
   it** and logs a warning. Adoption is gated on that table: a database without it is
   never adopted, so a failed application still stops startup loudly.
   `database/verify_base_model.sql` is the drift check for an adopted database.

A failure stops startup with the failing filename in the error; files that already ran
stay recorded.

### S6 — Graceful shutdown

`SIGINT`/`SIGTERM` stops accepting connections and drains in-flight requests within a
10-second window, then closes the PostgreSQL pool and the Redis client before exit.

### S7 — Container image

`api/Dockerfile` is multi-stage: `golang:1.27-alpine` builds a static binary
(`CGO_ENABLED=0`), the runtime stage is `alpine` with CA certificates, runs as a
non-root user, and exposes `8080`. No source tree and no toolchain in the final image;
`database/init` is copied in so the image can start standalone.

### S8 — The Go service is the only HTTP entry point

There is no proxy in front of the API: `nginx/` is gone and the `webserver` service no
longer exists. The `api` service publishes `9000:8080`, so the same public URL keeps
working while every request reaches the Go handler directly. TLS and rate limiting are
deferred to the deployment edge, which is not chosen yet.

### S9 — Compose runs the Go service

The `api_core` service is replaced by `api` (`build: ./api`, `container_name:
melia_api`), receiving `DATABASE_URL`, `REDIS_ADDR`, `RABBITMQ_URL`, `DB_INIT_DIR`
and shipping `database/init` inside the image (no bind mount), published as `9000:8080`. `redis`, `rabbitmq`
and `soketi` keep their current configuration; `webserver` (Nginx) is removed entirely.
`database` keeps its image, credentials and port, and additionally carries a
named data volume (`melia_pgdata` instead of the `./database/data` bind mount),
`shm_size`, UTF8 initdb arguments, a `pg_isready` healthcheck, `POSTGRES_DB: melia`
(the container now creates the database that the provisioning script used to create) and
no init-script mount, because the service is their only applier.

### S10 — Documentation matches the runtime

`README.md`, `AGENTS.md` and `api/README.md` describe the Go service and the commands
that actually exist (`go build`, `go test ./...`, `docker compose up`). No instruction
to run `composer`, `artisan`, or Vite survives.

### S11 — Verification without a live database

`go build ./...`, `go vet ./...` and `go test ./...` pass with no external service
running. Unit tests cover configuration validation, both health handlers with fake
probes, and the migration loader/runner (ordering, bootstrap-file exclusion,
meta-command stripping, already-applied skipping) with fake store and executor.
