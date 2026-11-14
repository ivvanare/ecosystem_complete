# Design: Base Domain Model — One Database, Two Schemas

Source of truth: `database/diagram_base.txt` (DBML, committed with this change).
Every rule below traces to it; where the DBML is ambiguous the decision is recorded here.

## D1 — Delivery format: init SQL scripts, not Laravel migrations

The diagram's context section decommissions Laravel ("Laravel (api-core) se da de baja")
and names Go as the future API owner. Writing 18 tables as Laravel migrations in a
decommissioned app would be dead work. The repo convention is PostgreSQL init scripts
(`database/init-db-postgres.sql` mounted into `docker-entrypoint-initdb.d`). This change
keeps that convention with an init **directory** (`database/init/NN-*.sql`, applied
alphabetically). When the Go service lands, it can baseline its migration tool
(golang-migrate or similar) from these files.

## D2 — One database `melzone`, schemas `seg` + `core`, enums in `public`

The diagram: "TOPOLOGÍA: UNA BASE DE DATOS, DOS ESQUEMAS" and "public → solo los Enum
compartidos". Database name: `melzone` (DBML project `melzone_base`). The old init
script creating `apiseg` + `coreapp` is removed — the diagram: "Este modelo REEMPLAZA
la topología anterior de dos bases (apiseg + coreapp)."

File layout (docker-entrypoint-initdb.d runs `*.sql` alphabetically):

| File | Content |
|------|---------|
| `database/init/01-create-database.sql` | `CREATE DATABASE melzone;` |
| `database/init/02-schemas-enums.sql`   | `\connect melzone`; `CREATE SCHEMA seg/core`; 6 enums in `public` |
| `database/init/03-seg-tables.sql`      | 10 `seg` tables + indexes + partial uniques + `tb_security_event` partitioning |
| `database/init/04-core-tables.sql`     | 8 `core` tables + checks + indexes + partial uniques |
| `database/init/05-foreign-keys.sql`    | All 17 FKs (including 3 cross-schema) as `ALTER TABLE ... ADD CONSTRAINT` |

FKs live in a separate file so every table exists before any FK references it
(cross-schema references make inline ordering fragile).

## D3 — `timestamp without time zone`, faithful to the DBML

Every `dt_*` column is declared `timestamp` in the DBML (= `timestamp without time
zone`). Kept as-is. Consequence recorded: the Go writer must store UTC instants;
switching to `timestamptz` is a future spec change, not an implementation liberty.

## D4 — UUID PKs without DB-side default

The DBML declares `co_* uuid [pk]` with no default. Writers (the Go service) generate
UUIDs application-side. PostgreSQL 15 has built-in `gen_random_uuid()` if a future
change wants DB defaults; not added here to stay faithful.

## D5 — Partial uniques as indexes, never table constraints

Diagram rule 4: "Todos los uniques de tablas con soft delete son PARCIALES (WHERE
dt_deleted_at IS NULL)". Implemented as `CREATE UNIQUE INDEX ... WHERE dt_deleted_at
IS NULL` with the exact names from the DBML (`ux_tb_user_email_active`,
`ux_tb_user_identity_provider_sub`, `ux_tb_role_name_active`,
`ux_tb_permission_code_active`, `ux_tb_review_user_place_active`).

## D6 — `seg.tb_security_event` monthly RANGE partitioning

The DBML cannot express partitioning: "va como nota de implementación... la retención
se aplica por partición mensual (PARTITION BY RANGE (dt_created_at), borrar = DROP
PARTITION)". Implementation without plpgsql (keeps the DDL declarative and parseable):

- `PARTITION BY RANGE (dt_created_at)`.
- 12 monthly partitions for 2026, named `tb_security_event_YYYY_MM` (deterministic,
  no generation function).
- One `DEFAULT` partition as a safety net for out-of-range rows; ops note: creating a
  future monthly partition requires the default partition to hold no overlapping rows,
  and retention = `DROP TABLE seg.tb_security_event_YYYY_MM`.

## D7 — FK actions: default `NO ACTION`

Excluded on purpose by the diagram ("las acciones ON DELETE/UPDATE de las FK ... son
una decisión de negocio aparte"), so no `ON DELETE`/`ON UPDATE` clauses — PostgreSQL
defaults to `NO ACTION`.

## D8 — docker-compose mount change + data reset note

`docker-compose.yml` mounts `./database/init:/docker-entrypoint-initdb.d` instead of
the single old file. Init scripts only run on an **empty** data directory; any existing
`./database/data` volume must be recreated locally (`docker compose down -v` /
delete `database/data`) for the new topology to apply. Dev-only impact: `database/data`
is not committed.

## D9 — Verification strategy in this environment

Docker and PostgreSQL are unavailable here, so:

1. **Syntax**: every `.sql` parsed with `pgsql-ast-parser` (real PostgreSQL grammar),
   stripping psql meta-commands (`\connect`) first.
2. **Structure**: checklist mapping every spec item (S1–S15) to DDL evidence.
3. **Behavior**: `database/verify_base_model.sql` ships with the change — runnable
   assertions for a live PG15 (schemas/enums/tables exist; partial unique admits the
   same email after soft delete; CHECKs reject bad rating/latitude; cross-schema FK
   enforced; partition routing works). Deferred live run, honestly reported.

## D10 — The Laravel demo stays untouched

`api-core` (RabbitMQ/Redis demo, its tests, its `operations` persistence WIP in stash)
is not modified by this change. Its committed `.env.example`/`config/database.php`
still reference the two-database variables; reconciling the demo with the new topology
belongs to the future decommissioning decision, not to this DDL change.
