# AGENTS.md — melZone

Compact guidance for OpenCode agents working in this repository.

## Stack

- **Runtime**: Go 1.27, static binary, `net/http` standard library (no framework)
- **Database**: PostgreSQL 15 (one database `melia`, schemas `seg` + `core` — spec: `database/diagram_base.txt`)
- **Message Broker**: RabbitMQ 3 with management plugin
- **Cache**: Redis 7

Laravel (`api-core/`) was removed on purpose: it held only scaffolding and a
RabbitMQ/Redis demo. The diagram names Go as the sole owner of the API and of writes,
with the mobile app as first consumer. Do not reintroduce PHP tooling.

## Repository Structure

```
melZone/
├── api/               # The only application: Go service
│   ├── cmd/api/       # HTTP entrypoint: config, wiring, signals, graceful shutdown
│   ├── cmd/worker/    # queue consumer: delivers mail over SMTP
│   ├── internal/      # config · httpapi · db · probe
│   └── Dockerfile
├── gui/admin/         # Angular 22 admin SPA (pnpm, vitest via `ng test`)
├── database/          # PostgreSQL init scripts + canonical DBML
├── tests/k6/          # Load tests (k6)
├── openspec/          # Change proposals, specs, task logs
└── docker-compose.yml # Multi-service orchestration
```

## Developer Commands

Backend commands run inside `api/`:

| Command | Purpose |
|---------|---------|
| `go run ./cmd/api` | Run the service (export env vars first) |
| `go test ./...` | Full suite, no external services required |
| `go build ./...` / `go vet ./...` | Compile / static analysis |
| `go build -o /tmp/api ./cmd/api` | Build a binary without polluting the tree |
| `k6 run tests/k6/smoke.js` | Contract smoke against a running API (writes nothing) |
| `k6 run tests/k6/signup.js` | Signup load; writes rows and queues mail |
| `docker compose run --rm --entrypoint /usr/local/bin/bootstrap -e EMAIL=... -e ROLE=SUPERADMIN api` | Grant the first administrator |
| `gh pr create --fill --base dev` | Open a PR against `dev`; `prod` rejects direct pushes |

Frontend commands run inside `gui/admin/` (its own pnpm workspace root):

| Command | Purpose |
|---------|---------|
| `pnpm install --frozen-lockfile` | Install exactly what the lockfile pins (what CI runs) |
| `npx ng test admin --watch=false` | SPA suite (vitest) |
| `npx ng build` | Production build, with the budgets |
| `npx ng serve` | Dev server on `:4200` |

`crud-maker` and `crud-maker-zorro` are no longer developed here: they live in
[`ivvanare/crud-maker`](https://github.com/ivvanare/crud-maker) (logic + adapters, MIT). Nothing
is published yet, so `gui/admin/package.json` links the built package from a **sibling checkout**
(`link:../../../crud-maker/dist/crud-maker`) — one directory beside this repository, the same
relative path locally and in CI. Consequences:

- A clean clone needs that sibling repository cloned and built before `pnpm install` will work:
  `cd ../crud-maker && pnpm install && pnpm exec ng build crud-maker`. The `admin` CI job does it.
- Editing a package means editing it there, rebuilding, and re-running the SPA suite here. Do not
  copy files back into this repository: one source of truth.
- `crud-maker-zorro` is added to this workspace's dependencies when a screen here actually consumes
  it; importing an unused package is noise.
- The one-way rule still holds inside that repository: the adapter may import the core, never the
  reverse.

## Branches

Three long-lived branches, promoting in one direction:

| Branch | Role | Protection |
|--------|------|------------|
| `dev` | integration — every feature branch merges here first | no deletion, no force-push |
| `quality` | QA / staging cut, taken from `dev` | no deletion, no force-push |
| `prod` | releases, taken from `quality` | the default branch: pull request required, `test`, `admin` and `image` green, no deletion, no force-push |

`prod` is the default branch (it replaced `master`). Work on a short-lived branch named after
its side (`feat/api-*`, `feat/admin-*`, `fix/*`), open the PR against `dev`, and promote:
`dev` → `quality` → `prod`. The required checks are matched by name, so renaming a CI job also
means updating the `protect prod` ruleset.

## Docker Services

Start everything: `docker compose up -d --build`

| Service | Container | Port |
|---------|------------|------|
| API (Go) | melzone_api | 9000 → 8080 |
| Worker (Go) | melzone_worker | — |
| Mailpit (dev) | melzone_mailpit | 8025 (UI), 1025 (SMTP) |
| PostgreSQL | melzone_db | 5432 |
| RabbitMQ | melzone_mq | 5672, 15672 (mgmt) |
| Redis | melzone_redis | 6379 |
| Soketi | melzone_soketi | 6001, 9601 |

RabbitMQ management UI: http://localhost:15672 (admin/admin_pass)

## Configuration

The service reads the **process environment only** — no dotenv loader. Required:
`DATABASE_URL`, `REDIS_ADDR`. Optional: `HTTP_ADDR` (default `:8080`), `RABBITMQ_URL`,
`DB_INIT_DIR` (default `/app/database/init`), `DB_AUTO_MIGRATE` (default `true`),
`LOG_LEVEL` (`debug|info|warn|error`). Missing required values fail startup by name; never
add a silent fallback.

## Testing

- `go test ./...` includes `internal/contract`, which fails when a route, an error code or an
  environment variable exists in code but not in `api/openapi.yaml`, `api/API.md`,
  `api/README.md` or `api/.env.example` — and when an emitted error code has no test asserting
  it. Adding an endpoint is a three-file change by design.
- Framework: standard `go test` (no testify requirement in new tests)
- Tests must not need PostgreSQL, Redis or RabbitMQ: probes and the migration ledger are
  interfaces, so tests pass fakes
- `api/internal/...` is a closed surface — keep it that way so packages stay free to move
- The frontend suite is the `admin` `@angular/build:unit-test` target on vitest/jsdom: no browser
  and no API needed. The packages carry their own suites in their own repository and their own CI.

## Key Architecture Notes

- Schema source of truth is `database/init/NN-*.sql`, and the **service is the only
  applier**: it runs pending files at startup and records them in
  `seg.tb_schema_migration`. The image copies that directory to `/app/database/init`;
  never bind-mount it, because a host dir the container user cannot read aborts startup.
  The container just creates the database (`POSTGRES_DB=melia`); never add a
  `docker-entrypoint-initdb.d` mount — two appliers collide with
  `schema "seg" already exists`. Never let a second copy of the DDL appear in the tree
  either.
- An empty ledger over an existing schema triggers **adoption** (recorded, not executed)
  with a warning. `database/verify_base_model.sql` is the drift check.
- `database/provision/01-create-database.sql` is provisioning, not a migration: it is not
  applied by compose nor by the service. psql meta-commands (`\connect`) are stripped by
  the loader for the same reason.
- The admin panel is a SPA on another origin: CORS is driven by `CORS_ALLOWED_ORIGINS`
  and enables credentials (the refresh token is an httpOnly cookie), so a wildcard origin
  is never valid and a preflight must never require authentication.
- Consumer-facing docs live in `api/API.md` (flows, error catalogue, both session transports);
  `api/openapi.yaml` is the machine-readable contract. Keep both true in the same commit as
  the endpoint change.
- The HTTP contract is `api/openapi.yaml`. Every new endpoint updates it in the same
  commit, and every error uses `{"error":{"code","message"}}` — clients parse `code`,
  never `message`.
- `/health/live` must never touch a dependency; `/health/ready` aggregates probes.
  `postgres` and `redis` are required, `rabbitmq` is optional (degrades, does not fail).
- There is no proxy in front of the API: `r.RemoteAddr` is the client address, which is
  what `seg.tb_security_event` stores. The day an edge (load balancer, ingress) appears,
  trust `X-Forwarded-For` only from that edge and never blindly.
- Security design (not implemented yet) is specified in `database/diagram_base.txt`
  §7–§11: stateless access JWT (HS256 now), rotating hashed refresh tokens, RBAC via
  user → role → permission, email ownership proven by code before `tb_user` exists,
  append-only audit log with no FK to users.

## Code Style

- `gofmt` / `go vet` clean; no formatter config needed
- Errors wrapped with `%w` and context; never discarded silently
- Logging with `log/slog`; the logger is injected, not a package global

## Commit Scope

One repository, two independent sides. A commit stays on one side; a branch belongs to one
side. This is what keeps a backend push from carrying frontend work in progress.

| Paths | Side |
|-------|------|
| `api/`, `database/`, `tests/` | backend |
| `gui/` | frontend |
| `odd/`, `openspec/`, `.github/`, `scripts/`, root docs, `docker-compose.yml` | whichever side it describes |

`scripts/check-commit-scope.sh` rejects a commit that stages both sides, reading the index so
unrelated dirty files do not matter. Install it once per clone (git shares `hooks/` across
linked worktrees, so one install covers them all):

```bash
cp scripts/pre-commit .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit
bash scripts/check-commit-scope.test.sh   # the guardrail's own tests
```

Stage by path. Never `git add -A` or `git commit -a` while the other side is dirty:

```bash
git commit -m "feat(api): ..." -- api database tests
git commit -m "feat(admin): ..." -- gui
```

A change that genuinely belongs to both sides opts out explicitly, and the hook says so:

```bash
MELIA_ALLOW_MIXED=1 git commit -m "chore(contract): ..."
```

Prefix branches by side (`feat/api-*`, `feat/admin-*`, same for `fix/`). Before opening a PR,
prove the branch kept its side — a backend branch must print nothing here:

```bash
git diff --name-only origin/dev...HEAD | grep -E '^(api|database|tests)/'
```

`gofmt` and `go vet` stay in CI instead of in the hook: they need a toolchain, and a commit
should not fail because Go is not on `PATH`.

## Gotchas

- Go may be installed at `/usr/local/go/bin` and not on `PATH`
- The canonical model expects UUID primary keys and `co_`/`tx_`/`nu_`/`dt_`/`js_` column
  prefixes — follow them, the diagram is the contract
- `database/init` runs only when the PostgreSQL volume is empty; reset with
  `docker compose down -v`
- Session tables are intentionally absent: auth uses JWT plus refresh tokens
- New CRUD entities in `gui/admin` are driven by a JSON-shaped config through external
  packages: logic in `crud-maker`, UI in an adapter such as `crud-maker-zorro`. They live in
  `ivvanare/crud-maker` and are linked from a sibling checkout until they are published. A
  working vertical: `gui/admin/src/app/demo/` (FAQs over MemoryTransport).
