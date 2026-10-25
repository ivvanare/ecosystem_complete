# melZone Ecosystem

Infrastructure-first backend for melZone: an API in Go, one PostgreSQL database, and the
supporting services in Docker.

## Stack

| Component | Technology |
|-----------|-------------|
| Runtime | Go 1.27 (static binary, distroless-style image) |
| HTTP | `net/http` standard library (`ServeMux` method patterns, no framework) |
| Database | PostgreSQL 15 (one database `melia`, schemas `seg` + `core`) |
| Message Broker | RabbitMQ 3 with management plugin |
| Cache | Redis 7 |

Laravel (`api-core/`) was decommissioned: it held only scaffolding and a RabbitMQ/Redis
demo, with no product code. The canonical model states Go as the sole owner of the API
and of writes — see `database/diagram_base.txt`.

## Quick Start

### Prerequisites
- Docker & Docker Compose
- Go 1.27 (only for running the API outside Docker)
- Git

### 1. Clone the repository
```bash
git clone https://github.com/ivvanare/ecosystem_complete.git
cd ecosystem_complete
```

### 2. Start all services
```bash
docker compose up -d --build
```

This starts:
- **melzone_api** (Go) - port 9000 → 8080, the only HTTP entry point
- **melzone_db** (PostgreSQL) - port 5432
- **melzone_mq** (RabbitMQ) - ports 5672, 15672 (management UI)
- **melzone_redis** - port 6379

### 3. Check the API
```bash
curl http://localhost:9000/health/live    # {"status":"ok"}
curl http://localhost:9000/health/ready   # per-dependency report
```

---

## Repository Structure

```
melZone/
├── api/               # Go service (the only application)
│   ├── cmd/api/       # entrypoint: config, wiring, signals, shutdown
│   ├── internal/      # config, httpapi, db, probe
│   ├── Dockerfile
│   └── README.md      # service documentation
├── database/          # PostgreSQL schema: canonical DBML + init scripts
│   ├── diagram_base.txt
│   ├── init/          # 01..05 SQL, applied by the container and by the service
│   └── verify_base_model.sql
├── openspec/          # Change proposals, specs and task logs
├── docker-compose.yml # Multi-service orchestration
├── AGENTS.md          # Guidelines for AI agents
└── README.md          # This file
```

---

## API

Only the runtime exists today; domain and security endpoints are the next change.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health/live` | Liveness, never touches a dependency |
| `GET` | `/health/ready` | Readiness: postgres + redis required, rabbitmq optional |

Configuration is environment-only: `DATABASE_URL`, `REDIS_ADDR`, `HTTP_ADDR`,
`RABBITMQ_URL`, `DB_INIT_DIR`, `DB_AUTO_MIGRATE`, `LOG_LEVEL`. The service refuses to
start without the required ones. Full table in **[api/README.md](api/README.md)**.

---

## Schema

`database/init/NN-*.sql` is the single source of truth for the schema, and the Go service
is the only applier (the files ship inside the image, so a DDL change needs a rebuild): it runs pending files at startup and records each in
`seg.tb_schema_migration`, so restarts are no-ops. The PostgreSQL container only creates
the empty database (`POSTGRES_DB=melia`) — no `docker-entrypoint-initdb.d` mount, since
two appliers of the same DDL would collide on a fresh volume.

A volume whose schema predates the ledger is adopted (recorded, not re-executed) with a
warning; `database/verify_base_model.sql` is the drift check.

The canonical model is `database/diagram_base.txt` (DBML for dbdiagram.io): 18 tables
across `seg` (identity, RBAC, audit) and `core` (places, schedules, menus, reviews,
routes), 6 enums in `public`, 17 real foreign keys including cross-schema ones.

---

## Developer Commands

Inside `api/`:

| Command | Purpose |
|---------|---------|
| `go run ./cmd/api` | Run the service (export the env vars first) |
| `go test ./...` | Run the test suite (no external services needed) |
| `go build ./...` | Compile |
| `go vet ./...` | Static analysis |

---

## Testing

- **Framework**: standard `go test`
- **Isolation**: probes and the migration ledger are interfaces; tests pass fakes, so no
  PostgreSQL, Redis or RabbitMQ is required
- **Coverage of the skeleton**: configuration validation, both health handlers, schema
  loader and runner, connection retry

```bash
cd api && go test ./...
```

---

## RabbitMQ Management UI

Access the management interface at:
```
http://localhost:15672
```

**Credentials:**
- Username: `admin`
- Password: `admin_pass`

Use this to monitor queue health, message rates, and failed jobs.

---

## Key Architecture Notes

- **Go is the only application service**: `net/http` + pgx + go-redis + amqp091
- **One PostgreSQL database**: `melia` with schemas `seg` (identity/permissions) and
  `core` (domain) — canonical spec: `database/diagram_base.txt`
- **Schema bootstrap**: the service applies `database/init/*.sql`, tracked in
  `seg.tb_schema_migration`; `01-create-database.sql` is provisioning and is skipped
- **No proxy in front**: the Go service listens on `:8080` and is published as `9000`. TLS and rate limiting belong at the deployment edge (load balancer or ingress) when the target is decided, not in a hop that terminates nothing
- **Sessions**: not modelled — the security design uses stateless JWT plus rotating
  refresh tokens (`seg.tb_refresh_token`)
- **Testing**: isolated from production services by design

---

## Environment Configuration

The service reads the process environment; see `api/.env.example` for the full list.

```bash
export DATABASE_URL='postgres://admin:my_admin_pass@localhost:5432/melia?sslmode=disable'
export REDIS_ADDR='localhost:6379'
export DB_INIT_DIR='../database/init'
cd api && go run ./cmd/api
```

---

## Links

- **Repository**: https://github.com/ivvanare/ecosystem_complete
- **Go Docs**: https://go.dev/doc
- **pgx**: https://github.com/jackc/pgx
- **RabbitMQ Docs**: https://www.rabbitmq.com/documentation.html
- **Redis Docs**: https://redis.io/docs/
