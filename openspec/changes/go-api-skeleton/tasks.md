# Tasks: Go API Skeleton — Decommission Laravel

Change: `openspec/changes/go-api-skeleton/`
Specs: `spec.md` (S1–S11) · Design: `design.md` (D1–D11)
Branch: `feat/go-api` (cut from `feat/base-domain-model`, which supplies `database/init/`)

## Tasks

| ID | Task | Specs | Route | Commit |
|----|------|-------|-------|--------|
| T1 | Tracking docs for this change | — | inline | 81b23b0 |
| T2 | Module scaffold + `internal/config` with RED→GREEN tests | S2, S3, S11 | inline | 871ee13 |
| T3 | `internal/httpapi`: router, `/health/live`, `/health/ready`, 404 JSON + tests | S4, S11 | inline | 871ee13 |
| T4 | `internal/db`: pgx pool with bounded retry; schema bootstrap loader/runner + tests | S5, S11 | inline | 871ee13 |
| T5 | `internal/probe`: postgres, redis, rabbitmq probes | S4 | inline | 871ee13 |
| T6 | `cmd/api/main.go`: wiring, logger, graceful shutdown | S3, S6 | inline | 871ee13 |
| T7 | `api/Dockerfile`, `api/.env.example`, `api/.dockerignore` | S7 | inline | 24804c0 |
| T8 | `nginx/default.conf` proxy, `docker-compose.yml` `api_core`→`api` | S8, S9 | inline | 59e350a |
| T9 | Delete `api-core/`; sync `README.md`, `AGENTS.md`, `api/README.md` | S1, S10 | inline | 0e54671 |
| T10 | Verification: `go build`, `go vet`, `go test ./...`, independent verify pass | S11 | delegated (verifier) | pending |

## Log

- L1 — User request (verbatim): "iniciemos quitando laraval vamos a cambiarlo por un api
  en Go". Scope resolved by one blocking question; the user chose "Esqueleto productivo"
  over "Esqueleto + módulo de seguridad": no domain logic in this change.
- L2 — Authorized plan (from the accepted option text): remove `api-core`, create the Go
  service with layout, environment config, pgx pool, HTTP router, `/health`, a runner
  that applies `database/init/*.sql`, multi-stage Dockerfile, compose swap, docs sync.
- L3 — Environment facts (verified): Go 1.27.2 at `/usr/local/go/bin` (not on `PATH`);
  Docker 29.1.3 installed but **daemon down**; `docker-compose` v1.29.2 broken
  (`ModuleNotFoundError: distutils`) and no `compose` v2 plugin; no `psql`. Consequence:
  `go build`/`go test` are the executable proof here, `docker build` and any live
  PostgreSQL run are deferred.
- L4 — Repo convention override: the project tracks changes under `openspec/changes/`,
  and `base-domain-model/tasks.md` already carries ODD's shape (S#/T#/route/commit/log).
  This change continues that convention instead of creating a parallel `odd/` tree.
- L5 — Design decisions recorded in `design.md`: `api/` + module path (D1); stdlib
  router, pgx, no ORM (D2); closed `internal/` layout (D3); liveness never probes
  dependencies (D4); reuse `database/init/` with a ledger and no statement splitting
  (D5); bounded pool retry, no unbounded wait (D6); RabbitMQ optional (D7); no dotenv
  (D8); injected `getenv` (D9); unverified `golang:1.27-alpine` tag (D10); Nginx keeps
  port 9000 and forwards client IPs for the audit model (D11).
- L6 — Verification evidence (executed, not assumed): `gofmt -l .` clean;
  `go vet ./...` clean; `go test ./...` green for config, db, httpapi, probe;
  `go build -o /tmp/melzone-api ./cmd/api` produces a 19 MB static binary; the
  `docker-compose.yml` parses as YAML with services `api, webserver, database,
  rabbitmq, redis, soketi`; runtime fail-fast exercised on the built binary —
  no env → exit 1 naming `DATABASE_URL`, only `DATABASE_URL` → exit 1 naming
  `REDIS_ADDR`, `LOG_LEVEL=verbose` → exit 1 naming `LOG_LEVEL`.
- L7 — Not verified here, deferred by L3: `docker build` of `api/Dockerfile`
  (including whether the `golang:1.27-alpine` tag resolves), a live run against
  PostgreSQL/Redis/RabbitMQ, `/health/ready` against real dependencies, and the
  first real application of `database/init/*.sql` through `seg.tb_schema_migration`.
- L8 — Native risk assessment over `master..HEAD` (`gentle-ai review assess
  --base-ref master --committed-only --json`): `risk: high`, `review_due: true`,
  reasons `hot_path` (deleted `api-core/config/auth.php`) and `process_boundary`
  (`api/internal/db/migrate.go`). Verification tier: independent verifier required.
- L9 — Independent verification (delegated, `gentle-ai-verify`), read-only over a clean
  copy: S1, S2, S3, S4, S6, S7, S8, S10, S11 **met** with observed evidence; two
  blockers found:
  **B1** — the PostgreSQL entrypoint and the service both applied `01–05`. On a fresh
  volume the container applied them, the service then saw an empty ledger, treated all
  files as pending, hit `CREATE SCHEMA seg;` (not idempotent) and crash-looped under
  `restart: always`. D5's "first run on an already-initialized database is safe" was
  wrong.
  **B2** — S9 claimed `database` kept "its current configuration", but the committed
  compose adds `shm_size`, UTF8 initdb args, the `melzone_pgdata` named volume and a
  `pg_isready` healthcheck. The item was unmet as written.
  Advisories: A1 malformed `DATABASE_URL` did not name the variable; A2 "distroless-style"
  wording vs `alpine:3`; A3 `probe.RabbitMQ` ignored its context; A4 the host does have
  compose v2 (the daemon is the unusable part); A5 the `api-core` deletion rode in the
  docs commit instead of the `refactor!` one.
- L10 — Correction batch (one scoped pass over both blockers):
  **B1** — single applier. `docker-compose.yml` sets `POSTGRES_DB: melzone` and no longer
  mounts `docker-entrypoint-initdb.d`; `database/init/01-create-database.sql` moves to
  `database/provision/` (provisioning, not migration); the runner gains narrow
  **adoption** for legacy volumes (empty ledger + `seg.tb_user` present → record without
  executing, warn, point at `database/verify_base_model.sql`). Covering tests:
  adopt-skip, no-adopt-when-ledger-populated, no-adopt-when-schema-absent.
  **B2** — S9 rewritten to state the `database` service changes instead of denying them.
  Also fixed: A1 (`DATABASE_URL` named in the connect error) and A3 (RabbitMQ probe
  honours the caller deadline). A2 and A5 are accepted as-is and recorded here.
