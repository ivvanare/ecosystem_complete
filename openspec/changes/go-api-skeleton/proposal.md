# Proposal: Go API Skeleton — Decommission Laravel

## Intent

The canonical model (`database/diagram_base.txt`) already states the outcome: "Laravel
(api-core) se da de baja: contiene solo un andamio y un demo de RabbitMQ/Redis, sin
código de producto. Go pasa a ser el único servicio dueño de la API y de las
escrituras, con el móvil como primer consumidor."

This change executes the first half of that sentence and lands the runway for the second:
`api-core` is removed and a Go service takes its place with no business logic yet.

## Scope

### In Scope
- Delete `api-core/` (Laravel 12, Blade, Vite, Composer, the RabbitMQ→Redis demo).
- New Go module at `api/`: config from environment, HTTP router on the standard
  library, health endpoints, PostgreSQL pool, Redis and RabbitMQ probes, schema
  bootstrap driven by the existing `database/init/*.sql` files, graceful shutdown.
- Multi-stage `api/Dockerfile`; `api/.env.example`; `api/README.md`.
- `docker-compose.yml`: `api_core` (PHP-FPM) replaced by `api` (Go).
- `nginx/default.conf`: FastCGI replaced by reverse proxy.
- Documentation sync (`README.md`, `AGENTS.md`).

### Out of Scope
- Any domain or security endpoint: registration, login, refresh rotation, password
  change, RBAC enforcement, audit writes, social login. Those are the next change and
  need the specification in `database/diagram_base.txt` §7–§11.
- Admin frontend (the diagram still marks it PENDIENTE).
- The `soketi` service: kept as-is because nothing in this change consumes or requires
  it; removing it is a separate decision.
- Live database verification: the Docker daemon is down in the authoring environment
  (see tasks.md L3).

## Capabilities

### New Capabilities
- `go-api-runtime`: environment-driven configuration, HTTP health endpoints, PostgreSQL
  pool, dependency probes, graceful shutdown, container image.

### Modified Capabilities
- `database-bootstrap`: the same `database/init/*.sql` files are now also applied by the
  service at startup, tracked in a ledger table, so schema state no longer depends on
  the PostgreSQL container's first-run entrypoint.
- `web-tier`: Nginx proxies HTTP to the Go service instead of executing PHP.
