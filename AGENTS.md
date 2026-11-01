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
├── database/          # PostgreSQL init scripts + canonical DBML
├── tests/k6/          # Load tests (k6)
├── openspec/          # Change proposals, specs, task logs
└── docker-compose.yml # Multi-service orchestration
```

The Angular console used to sit here as `gui/admin/`. It now lives in its own repository,
`melia-admin`, with the same `dev` → `quality` → `prod` promotion. Nothing here builds, tests
or serves it.

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

Frontend commands live with the SPA in `melia-admin`: the toolchain, the budgets, the suite and
the `crud-maker` sibling link are documented there, not here.

`crud-maker` and `crud-maker-zorro` are not developed here: they live in
[`ivvanare/crud-maker`](https://github.com/ivvanare/crud-maker) (logic + adapters, MIT). Nothing
is published yet, so the SPA links the built package from a **sibling checkout** — one
directory beside whichever repository consumes it, the same relative path locally and in CI.
Consequences:

- A clean clone of the SPA needs that sibling cloned and built before `pnpm install` will work:
  `cd ../crud-maker && pnpm install && pnpm exec ng build crud-maker`. Its CI job does it.
- Editing a package means editing it there, rebuilding, and re-running the SPA suite. Do not copy
  files between repositories: one source of truth.
- `crud-maker-zorro` is added to the SPA's dependencies when a screen actually consumes it;
  importing an unused package is noise.
- The one-way rule still holds inside that repository: the adapter may import the core, never the
  reverse.

## Branches

Three long-lived branches, promoting in one direction:

| Branch | Role | Protection |
|--------|------|------------|
| `dev` | integration — every feature branch merges here first | no deletion, no force-push |
| `quality` | QA / staging cut, taken from `dev` | no deletion, no force-push |
| `prod` | releases, taken from `quality` | the default branch: pull request required, `test` and `image` green, no deletion, no force-push |

`prod` is the default branch (it replaced `master`). Work on a short-lived branch named after
its side (`feat/api-*`, `feat/db-*`, `fix/*`), open the PR against `dev`, and promote:
`dev` → `quality` → `prod`. The required checks are matched by name, so renaming a CI job also
means updating the `protect prod` ruleset. The SPA has its own repository and its own ruleset;
it left in the split that removed `gui/`.

## Working alongside other sessions

Two agent sessions work in this repository — one on `api/`, one on `database/` — and a third owns
`melia-admin`. These rules exist because the same collision has cost real work: a session switched
branches under another, and a migration changed the behaviour of another session's tests while
those tests were running.

**The shared resource is the live database, not the files.** A `.sql` file is inert until someone
applies it; `melia` is live for both sessions at once. Before applying a migration to the dev
database, announce the file and its observable effect — for example "applying 14: it relaxes the
`tb_place.tx_timezone` CHECK, it cannot break existing inserts". Applied DDL has no cheap undo,
and the session that applies it is not always the one that pays for it.

**The ledger is environment state, not source.** `seg.tb_schema_migration` is written at runtime by
the service; `database/init/*.sql` is the source of truth. Rebuilding the image and restarting
touches the ledger, so it belongs to the shared instance rather than to one side.

**Files both sessions touch — announce before editing:** `AGENTS.md`, `README.md`,
`database/diagram_base.txt` (the canonical spec the HTTP contract is written against),
`database/verify_base_model.sql`, `docker-compose.yml`, `odd/tasks/*`.

**A schema change that reaches Go arrives as a message, not a surprise.** When DDL affects models,
say: tables touched, columns added with type and nullability, columns removed, defaults, and any
seeded UUID a test needs. A `NOT NULL` without a default plus a shape `CHECK` that rejected valid
IANA zones is exactly the case this prevents — it was found late and only by running the path that
nobody had run.

**One worktree per session.** Two sessions on one checkout is what let a branch switch happen
under someone else's feet. Reads and diagnostics need no turn; writes do.

**`dev` is integration: branch, then pull request.** No required check gates `dev`, so nothing but
discipline stops a direct push there. A PR body states what was verified, not just what changed —
that is what makes the merge auditable later.

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
  the loader for the same reason. Applying the files **by hand** needs the ledger first:
  create `seg.tb_schema_migration` (`tx_name text PRIMARY KEY`, `dt_applied_at timestamptz
  NOT NULL DEFAULT now()`) before `02-schemas-enums.sql`, because `13-comments.sql`
  comments that table and fails without it. The service is immune: it runs `Ledger.Ensure`
  before touching any file, which is why the Docker path needs nothing extra.
- Point-in-time history is database-owned and additive: ledger files 07-08 add six
  append-only `*_history` tables (`core.tb_place_history`, `core.tb_menu_item_history`,
  `core.tb_place_schedule_history`, `core.tb_review_history`,
  `seg.tb_user_role_history`, `seg.tb_role_permission_history`) maintained by
  PostgreSQL triggers on the source tables. Never write to them from the service —
  writes collide with the single-open-interval unique and the closed-interval guard.
  Physical delete leaves a `DELETE` row born closed
  (`dt_valid_from = dt_valid_to`); logical delete (`dt_deleted_at`) arrives as an
  UPDATE; `dt_updated_at`-only updates record nothing. Optional actor per
  transaction: `SELECT set_config('app.current_user_id', '<uuid>', true)` before
  mutating (stored in `co_actor_user`, NULL if unset). A future history endpoint is
  contract-first: `openapi.yaml` + `API.md` + contract test, never improvised.
- Locale catalogs and time zones (ledger 09-14): `ALTER DATABASE melia SET timezone
  TO 'UTC'` is active (new sessions report UTC; `now()` returns UTC).
  `core.tb_place` dropped `tx_category`, added `co_place_category uuid NOT NULL` (FK
  `tb_place_category`), `tx_country char(2) NOT NULL`, `tx_timezone varchar NOT NULL` —
  no DEFAULT, INSERTs missing them fail with `not_null_violation`. `core.tb_menu_item`
  added `co_currency uuid NOT NULL` (FK `tb_currency`). Seeded catalogs: categories
  hotel=0d05c7ce-21b6-4548-9498-d64267ec8ea5,
  arepera=43c43189-8813-4f55-8636-f2aff3c37c5d, parque=fa004a4f-5fb0-429b-a9bd-81ee40440679;
  currencies USD=259078f5-a8e4-4319-bd89-21cf448ea8d9, VES=7d4cf35b-7bbe-4845-949c-d56004a14fd6,
  COP=d3748ccb-77a0-4f51-b2fb-0a0d5fcda746 (all nu_decimals=2). Timezone is per-row:
  `tb_place_schedule` open/close times are place-local; group reports by
  `dt_created_at AT TIME ZONE p.tx_timezone`.
- `ck_tb_place_timezone` is a SHAPE guard only, relaxed by `14-relax-timezone-check.sql`
  to one to three slash-separated components: IANA carries two levels for real zones
  (`America/Argentina/Buenos_Aires`) and bare names such as `UTC` are valid on their
  own. The original one-slash pattern in `11` rejected both and made an Argentine
  place impossible to insert. Membership in `pg_timezone_names` cannot be expressed in
  a CHECK and belongs at the application edge. Never edit an applied migration: the
  fix shipped as a new ledger file.
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

There is one side here now. `api/`, `database/`, `tests/` and the documentation that describes
them are all backend, so the old two-sides rule and its guardrail are gone: the script went out
with the frontend, and nothing here can mix a frontend change into a backend commit because
there is no frontend in this repository.

Branch names keep the side prefix (`feat/api-*`, `fix/*`) because they still read as intent.
`crud-maker` and `melia-admin` keep their own repositories, with their own commits and their own
CI.

Before opening a PR, confirm the branch only touched this repository's side:

```bash
git diff --name-only origin/dev...HEAD | grep -vE '^(api|database|tests|odd|openspec|\.github|scripts)/'
```

## Gotchas

- Go may be installed at `/usr/local/go/bin` and not on `PATH`
- The canonical model expects UUID primary keys and `co_`/`tx_`/`nu_`/`dt_`/`js_` column
  prefixes — follow them, the diagram is the contract
- `database/init` runs only when the PostgreSQL volume is empty; reset with
  `docker compose down -v`
- Session tables are intentionally absent: auth uses JWT plus refresh tokens
- New CRUD entities in the console are driven by a JSON-shaped config through external packages:
  logic in `crud-maker`, UI in an adapter such as `crud-maker-zorro`. Both live in
  `ivvanare/crud-maker` and are linked from a sibling checkout until they are published. The
  working vertical is `src/app/demo/` in the SPA's own repository.
