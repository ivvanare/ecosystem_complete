# geo-foundation — P0 of social discovery

Feature: geographic foundation for `melia` — PostGIS in the deployment, the derived spatial
column, `tx_region`, and a scheduler in the worker.
Branch: `feat/api-geo-foundation`, branched from `dev`; two work-unit commits. Do not push.
Authority: `openspec/changes/social-discovery/` is the specification of record. Read
`tasks.md` (T0.1–T0.11), `spec.md` §S3, §S15, §S16, and `design.md` §D2, §D13, §D14 until
`## Log` before writing anything. Where this file and the change disagree, the change wins.

Why this slice exists at all: three later features depend on a scheduler that does not exist
in this repository today (no `Ticker`, no `cron`, verified), and the Postgres image change is
cheaper once than twice.

---

## Specs

**S1 — Branch and scope.** Work on `feat/api-geo-foundation` off `dev`. Only these paths
change: `docker-compose.yml`, `.github/workflows/`, `database/init/15..17-*.sql`,
`database/diagram_base.txt`, `database/verify_base_model.sql`, `api/cmd/worker/`,
`api/internal/worker/` (new), `api/internal/config/`, `api/README.md`, `api/.env.example`.
Nothing else. No `openspec/` edits in this slice.

**S2 — PostGIS in the deployment.** `docker-compose.yml` image `postgres:15-alpine` becomes a
`postgis/postgis:15-3.x` tag; mirror it in the CI job that builds or runs Postgres, if one
exists. Same PostgreSQL major, so the existing named volume stays valid. Record in the ledger
file's header comment that `CREATE EXTENSION postgis` needs a privileged role, and that the
compose user is the init superuser while production's runner must be confirmed before release.

**S3 — `tx_region`.** `core.tb_place` gains `tx_region varchar(6) NULL` holding ISO 3166-2,
with `CHECK (tx_region ~ '^[A-Z]{2}-[A-Z0-9]{1,3}$')`. NULL is deliberate: requiring it makes
a place uninsertable wherever a geocoder cannot answer. Plus
`ix_tb_place_country_region (tx_country, tx_region)` in that order.

**S4 — The derived spatial column.** `core.tb_place` gains `co_location geography(Point,4326)`
derived from `nu_latitude`/`nu_longitude`, with a GiST index. Prefer
`GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(nu_longitude, nu_latitude), 4326)::geography) STORED`
— note longitude first. If PostgreSQL rejects the expression as non-immutable, use a trigger
instead, and say in the report which one it was and why. `nu_latitude`/`nu_longitude` stay
`NOT NULL decimal(9,6)` with their existing CHECK: they remain the contract.
Do **not** add `tx_h3_cell`, and do **not** install `h3-pg` (D13).

**S5 — History coverage.** `tb_place` is in the point-in-time set. Mirror `tx_region` into
`core.tb_place_history` and into the payload the place trigger writes, replacing the function
with `CREATE OR REPLACE` — never `DROP`+`CREATE`, because a trigger binds by name. Decide
whether `co_location` belongs in the history payload and justify it: the defensible answer is
**no**, because it is derived from two columns the payload already carries, and duplicating it
would make the payload claim a change that is not one.

**S6 — Scheduler in the worker.** `api/cmd/worker` gains a scheduler: a ticker with a
configured interval, a job registry, and graceful shutdown on `SIGINT`/`SIGTERM` sharing the
existing shutdown path. Register exactly one real job to prove the mechanism end to end; it
may be a no-op that logs. No feature job in this slice. The interval comes from a new
environment variable; missing or invalid means the scheduler is disabled with a warning, never
a failed startup, because the worker's mail duty must survive a bad config. Document the
variable in `api/README.md` and `api/.env.example` — `TestEveryEnvironmentVariableIsDocumented`
fails otherwise.

**S7 — Canonical model.** Update `database/diagram_base.txt` with `tx_region` and
`co_location`, marked as derived/off-contract where appropriate, and the new index. Check
`database/verify_base_model.sql`: if it enumerates columns it must be updated, and if it
compares against the diagram the drift check must still pass.

**S8 — Comments live in the new file.** `13-comments.sql` is already recorded in the ledger.
**Never edit an applied migration.** Column comments for the new columns go in `15`/`16`/`17`.

**S9 — Tests.** `go test ./...` must keep passing without PostgreSQL, Redis or RabbitMQ:
probes and the ledger are interfaces, so tests pass fakes. Add focused tests for the scheduler
(registry, interval parsing, shutdown, disabled-on-bad-config). For the SQL, add whatever
assertion is possible without a live database; if a rule can only be proven against Postgres,
say so in the report instead of pretending it is covered.

**S10 — Verification.** `gofmt`/`go vet`/`go build ./...`/`go test ./...` clean in `api/`.
Then prove the DDL against a **throwaway database**, not the shared dev one: create a scratch
database in the running container, apply `02..17` in lexical order, run the drift check, and
drop the scratch database. Report the exact commands and their output summary. Do not apply
the new ledger to the live `melia` database in this slice; state that it remains pending.

---

## Repo rules that bite in this slice

- Only the service applies migrations; the loader strips psql meta-commands (`\c`,
  `\connect`), so ledger files must be plain SQL the loader can execute.
- The ledger (`seg.tb_schema_migration`) is environment state, not source. Do not hand-apply
  DDL to the live dev database and do not touch the ledger table manually.
- `api/internal/...` is a closed surface: keep new packages free to move.
- Errors wrapped with `%w`; `log/slog` with an injected logger, not a package global.
- Conventional Commits, two units: (1) image + extension ledger, (2) spatial column + region +
  scheduler. Tests and docs ride with their behaviour, not in a separate commit.

## Allowed edit surfaces

```
docker-compose.yml
.github/workflows/
database/init/15-postgis-extension.sql
database/init/16-place-geo.sql
database/init/17-place-region.sql
database/diagram_base.txt
database/verify_base_model.sql
api/cmd/worker/
api/internal/worker/
api/internal/config/
api/README.md
api/.env.example
```

## Tasks

- T1 — branch `feat/api-geo-foundation` off `dev`.
- T2 — compose image + CI mirror + ledger `15-postgis-extension.sql` (commit 1).
- T3 — ledger `16-place-geo.sql`: derived `co_location` + GiST (commit 2).
- T4 — ledger `17-place-region.sql`: `tx_region` + CHECK + index + history mirror + function
  replaced (commit 2).
- T5 — scheduler + registry + graceful shutdown + env var + docs (commit 2).
- T6 — `diagram_base.txt` and `verify_base_model.sql` updated (commit 2).
- T7 — tests for the scheduler and anything else provable without a database.
- T8 — verify per S10 and report the scratch-database evidence.

---

## Log

- L1 — Human authorization, verbatim: "comencemos con p0, necesitare mas contexto de lo que te
  dije para recordar que estaba diciendo en saber de mi," — the trailing clause belongs to an
  earlier, truncated product requirement and does **not** change this slice's scope.
- L2 — Scope taken from `openspec/changes/social-discovery/tasks.md` T0.1–T0.11, which were
  written and reviewed before this authorization.
