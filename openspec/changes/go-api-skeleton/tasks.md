# Tasks: Go API Skeleton — Decommission Laravel

Change: `openspec/changes/go-api-skeleton/`
Specs: `spec.md` (S1–S11) · Design: `design.md` (D1–D11)
Branch: `feat/go-api` (cut from `feat/base-domain-model`, which supplies `database/init/`)

## Tasks

| ID | Task | Specs | Route | Commit |
|----|------|-------|-------|--------|
| T1 | Tracking docs for this change | — | inline | pending |
| T2 | Module scaffold + `internal/config` with RED→GREEN tests | S2, S3, S11 | inline | pending |
| T3 | `internal/httpapi`: router, `/health/live`, `/health/ready`, 404 JSON + tests | S4, S11 | inline | pending |
| T4 | `internal/db`: pgx pool with bounded retry; schema bootstrap loader/runner + tests | S5, S11 | inline | pending |
| T5 | `internal/probe`: postgres, redis, rabbitmq probes | S4 | inline | pending |
| T6 | `cmd/api/main.go`: wiring, logger, graceful shutdown | S3, S6 | inline | pending |
| T7 | `api/Dockerfile`, `api/.env.example`, `api/.dockerignore` | S7 | inline | pending |
| T8 | `nginx/default.conf` proxy, `docker-compose.yml` `api_core`→`api` | S8, S9 | inline | pending |
| T9 | Delete `api-core/`; sync `README.md`, `AGENTS.md`, `api/README.md` | S1, S10 | inline | pending |
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
