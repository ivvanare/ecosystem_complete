# Design: Go API Skeleton — Decommission Laravel

## D1 — Directory `api/`, module `github.com/ivvanare/ecosystem_complete/api`

The PHP app lived in `api-core/`. Reusing that name for a Go service invites stale
tooling assumptions, and `api/` reads correctly next to `database/` and `nginx/`. The
module path follows the repository remote so the packages are importable if the service
ever splits into its own repo.

## D2 — Standard library over framework

`net/http.ServeMux` has handled method+path patterns since Go 1.22, and this service has
three routes. A router dependency would buy nothing today. `database/sql` is not used:
pgx is the driver the model's PostgreSQL features (enums, schemas, partitioning,
`timestamptz`) will need, so the skeleton starts there instead of migrating later.

Dependencies, deliberately few: `jackc/pgx/v5`, `redis/go-redis/v9`,
`rabbitmq/amqp091-go`, plus `google/uuid` only when the security module needs it.

## D3 — Layout

```
api/
├── cmd/api/main.go            # wiring, signals, lifecycle
├── internal/config/config.go  # env parsing and validation
├── internal/httpapi/          # router + health handlers
├── internal/db/               # pool + schema bootstrap
└── internal/probe/            # postgres/redis/rabbitmq readiness probes
```

`internal/` keeps the surface closed: nothing outside the module can import it, so the
package structure stays free to change while the module is young.

## D4 — Health: liveness never touches dependencies

A liveness probe that pings PostgreSQL turns a database hiccup into a container restart
loop, which is exactly the wrong response — the process is healthy, the dependency is
not. Hence `S4`'s split. Readiness aggregates probes behind:

```go
type Probe interface {
    Name() string
    Required() bool
    Check(ctx context.Context) error
}
```

Handlers take `[]Probe`, so tests pass fakes and no test needs a server.

## D5 — Schema bootstrap: reuse `database/init/`, add a ledger

`design.md` of `base-domain-model` (D1) already anticipated this: "When the Go service
lands, it can baseline its migration tool (golang-migrate or similar) from these files."
Two options were considered:

- **Adopt golang-migrate with a new `migrations/` tree.** Duplicates 18 tables of DDL.
  Two copies drift silently, and the diagram is already the third artifact to keep in
  sync.
- **Apply the existing files and record what ran.** One source of truth; the DDL that
  the PostgreSQL entrypoint uses is the DDL the service uses.

The second wins. Consequences handled explicitly:

- **One applier, not two.** The PostgreSQL entrypoint mount is removed and the container
  only creates the database (`POSTGRES_DB=melzone`). An earlier draft kept both: the
  entrypoint applied 01–05 on a fresh volume while the service applied the same files
  against a freshly created ledger, hit `schema "seg" already exists` on the first
  non-idempotent `CREATE SCHEMA`, and `restart: always` turned it into a crash loop.
  One applier is not a style preference; the two paths are mutually exclusive.
- **Adoption instead of a crash on legacy volumes.** A volume created before the ledger
  existed already has the schema, so its ledger is empty and every file looks pending.
  When the ledger is empty *and* `seg.tb_user` exists, the service records every file as
  applied without executing it and logs a warning pointing at
  `database/verify_base_model.sql`. The alternative — refusing to start and asking for
  `down -v` — is correct for a database with data and hostile in this pre-production
  project; the drift check keeps adoption honest.
- `01-create-database.sql` moves to `database/provision/`: it is provisioning, not a
  migration. You cannot `CREATE DATABASE melzone` from a connection already inside
  `melzone`. The loader still refuses an `01-create-database.sql` in `DB_INIT_DIR` so a
  stray copy cannot reintroduce the failure.
- psql meta-commands (`\connect`, `\c`) are shell-level directives, not SQL. Lines whose
  trimmed form starts with `\` are stripped. `02-schemas-enums.sql` starts with
  `\connect melzone`, so without this every startup would fail.
- No statement splitting: pgx's `Exec` without arguments uses the simple query protocol,
  which accepts multiple statements. Parsing SQL to split on `;` would break on any
  future dollar-quoted body.
- `seg.tb_schema_migration` is infrastructure, not domain: it is absent from
  `database/diagram_base.txt`, which deliberately covers business tables only. It lives
  in `seg` because the model owns that schema, and its columns follow the model's prefix
  convention (`tx_`, `dt_`).

The ledger is `IF NOT EXISTS` and `seg` is created with `IF NOT EXISTS`, so the first run
on an already-initialized database is safe.

## D6 — Startup ordering

`depends_on` starts containers in order but does not wait for readiness. With
`restart: always`, a failed first boot becomes a crash loop. The pool therefore retries
with backoff for a bounded window (5 attempts, exponential, capped at 2s) and only then
returns an error. No unbounded wait: a wedged database should surface as a failing
container, not a silent one.

## D7 — RabbitMQ is optional, and readiness says so

Nothing in this skeleton publishes. Making `rabbitmq` a required readiness dependency
would turn an unused broker into an outage for the mobile client. It stays a probe that
reports `error` and degrades the report to `503 degraded`, which is visible in
orchestration without pretending the API cannot serve.

## D8 — No `.env` loader

Compose supplies `environment:`. A dotenv dependency exists to paper over a missing
environment; the service reads the process environment only, and `api/.env.example`
documents the contract for humans.

## D9 — Config is a value, not a global

`config.Load(getenv func(string) string) (Config, error)` takes the lookup function.
Tests pass a map closure; `main` passes `os.Getenv`. No `os.Getenv` calls outside
`main`, no package-level `Config`.

## D10 — Image tag risk

The authoring host has Go 1.27.2 but a stopped Docker daemon, so `golang:1.27-alpine`
could not be pulled or built here. The Dockerfile is written against that tag and the
limitation is recorded in `tasks.md`; a `docker build` on a host with a running daemon
is the deferred check.

## D11 — Nginx keeps the port, changes the protocol

The public port stays `9000:80` so existing muscle memory and any local script keep
working; only `fastcgi_pass` becomes `proxy_pass http://api:8080`. `X-Forwarded-For`
and `X-Real-IP` are forwarded because the security model stores client IPs in
`tb_security_event` — dropping them later would silently write proxy IPs into audit
rows.
