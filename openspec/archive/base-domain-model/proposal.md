# Proposal: Base Domain Model — One Database, Two Schemas

## Intent

Replace the two-database topology (`apiseg` + `coreapp`) with the base domain model
specified in `database/diagram_base.txt`: **one PostgreSQL database with two schemas**
(`seg` for identity/permissions, `core` for domain) plus shared enums in `public`.

The diagram is the canonical spec. Its own context section states: Laravel (`api-core`)
is scaffolding + RabbitMQ/Redis demo and gets decommissioned; Go becomes the sole owner
of the API and of the writes, with the mobile app as first consumer.

## Scope

### In Scope
- SQL DDL for the full base model, faithful to `database/diagram_base.txt`:
  - Schemas `seg` + `core`; 6 enums in `public`.
  - 10 `seg` tables (users, refresh tokens, user tokens, signup requests, federated
    identities, RBAC: roles/permissions/bridges, append-only security event log).
  - 8 `core` tables (places, schedules, photos, menus, menu items, reviews, routes,
    route-place bridge).
  - Real FKs including cross-schema; partial unique indexes where the diagram marks
    `[PARTIAL UNIQUE]`; column CHECK constraints; monthly RANGE partitioning for
    `seg.tb_security_event`.
- Rework of the PostgreSQL docker init: one database `melzone`, init directory mounted
  by `docker-compose.yml`; the old two-database `init-db-postgres.sql` is removed.
- Commit `database/diagram_base.txt` as the canonical spec source.
- `database/verify_base_model.sql`: runnable assertion script for a live PostgreSQL 15.

### Out of Scope
- The Go API service (no spec exists yet; named as the future owner by the diagram).
- Decommissioning `api-core` (Laravel demo stays untouched; separate decision).
- Admin frontend (diagram marks it PENDIENTE).
- Session tables and FK ON DELETE/UPDATE actions — excluded on purpose by the diagram:
  > "Fuera del alcance a propósito: tablas de sesión (con JWT no hacen falta) y las
  > acciones ON DELETE/UPDATE de las FK (son una decisión de negocio aparte)."
- The discarded two-database split work in `api-core` (uncommitted, dropped by user
  decision 2026-05-05; demo persistence WIP preserved in git stash).

## Capabilities

### New Capabilities
- `base-domain-model`: Complete PostgreSQL DDL for seg + core schemas, enums, FKs,
  indexes, checks, and partitioning, applied at container init.

### Modified Capabilities
- `database-topology`: Docker PostgreSQL init creates one `melzone` database instead of
  `apiseg` + `coreapp`.

## Approach

1. Init scripts under `database/init/` run alphabetically via
   `docker-entrypoint-initdb.d`: `01` creates the database, `02` schemas + enums,
   `03` seg tables, `04` core tables, `05` foreign keys.
2. Partial uniques become `CREATE UNIQUE INDEX ... WHERE dt_deleted_at IS NULL`.
3. `seg.tb_security_event` is `PARTITION BY RANGE (dt_created_at)` with a helper
   function and initial partitions (diagram note: DBML cannot express partitioning).
4. Verification: every `.sql` parsed with the real PostgreSQL grammar (pglast), plus
   `verify_base_model.sql` with structural + behavioral assertions for a live server
   (docker is unavailable in this environment, so the live run is deferred and scripted).
