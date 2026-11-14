# Tasks: Base Domain Model — One Database, Two Schemas

Change: `openspec/changes/base-domain-model/`
Specs: `spec.md` (S1–S15) · Design: `design.md` (D1–D10)
Canonical source: `database/diagram_base.txt`

## Tasks

| ID | Task | Specs | Route | Commit |
|----|------|-------|-------|--------|
| T1 | Rework init: remove two-database `init-db-postgres.sql`; add `database/init/01-create-database.sql` (`CREATE DATABASE melzone`); mount `./database/init` in `docker-compose.yml` | S1 | inline | c12c1a3 (deletion rode in 3b94db6) |
| T2 | `02-schemas-enums.sql`: `\connect melzone`, `CREATE SCHEMA seg/core`, 6 enums in `public` | S1, S13 | inline | c12c1a3 |
| T3 | `03-seg-tables.sql`: 10 `seg` tables, indexes, 4 partial uniques, `tb_security_event` RANGE partitioning (2026 months + DEFAULT) | S2–S5, S7–S12, S14 | inline | 884bccb |
| T4 | `04-core-tables.sql`: 8 `core` tables, checks (S6), indexes, 1 partial unique | S2, S4–S6, S14 | inline | 127eefb |
| T5 | `05-foreign-keys.sql`: 17 FKs incl. 3 cross-schema; no ON DELETE/UPDATE actions | S3, S14, S15 | inline | 127eefb |
| T6 | Sync docs that describe the two-database topology (`AGENTS.md`, both READMEs) | S1 | inline | 822f2c6 |
| T7 | Verification: real PG15 parse (libpg_query); S#→DDL checklist; committed `verify_base_model.sql` runnable assertions | S1–S15 | inline | 5fdbe96 |
| T8 | Work-unit commits and close | — | inline | 3b94db6, c12c1a3, 884bccb, 127eefb, 5fdbe96, 822f2c6 + this one |

## Log

- L1 — Original request (verbatim): "donde quedamos". After reconciliation from git
  state (no memory tools, no `odd/` dir, no active openspec change), the in-flight
  uncommitted work was: (1) two-database split `apiseg`/`coreapp` in
  `api-core` config + `User` model + users migration + `.env.example`;
  (2) durable operations persistence (`Operation` model + migration,
  `SendNotification` → PostgreSQL, `DashboardController` fallback);
  (3) untracked `database/diagram_base.txt`.
- L2 — User decision (verbatim option text): "Descartar split, ir al esquema único —
  Tiro los cambios de dos bases y arrancamos el modelo del diagrama: una base,
  esquemas seg + core." Executed: `git checkout --` on the 4 split files (reverted);
  operations-persistence WIP preserved in `git stash` ("wip: operations persistence
  demo (Laravel)"); diagram kept untracked to be committed with this change.
  Branch created: `feat/base-domain-model`.
- L3 — Environment facts: docker unavailable, no local postgres/psql, no pip.
  Verification = `pgsql-ast-parser` (Node 24, scratch `/tmp/sqlverify`) for syntax
  truth + committed `verify_base_model.sql` for the deferred live run (design D9).
- L4 — Scope decisions recorded in design.md: init SQL scripts over Laravel
  migrations (D1); database name `melzone` (D2); declarative 2026 monthly partitions
  + DEFAULT, no plpgsql (D6); Laravel demo untouched (D10).
- L5 — Verification evidence (T7): all `database/init/*.sql` + `verify_base_model.sql`
  parsed GREEN with libpg_query (real PostgreSQL 15 grammar, @pgsql/parser WASM,
  scratch /tmp/sqlverify): 01→1 stmt, 02→8 (2 schemas + 6 enums), 03→37 (10 tables +
  13 partitions + 14 indexes), 04→19 (8 tables + 11 indexes), 05→17 FKs,
  verify→13 stmts. Counts reconcile exactly with the diagram (10 seg + 8 core tables,
  17 Refs, 5 partial uniques, 12+1 partitions). First attempt with pure-JS
  pgsql-ast-parser could not parse CREATE DATABASE / PARTITION BY (library
  limitation), replaced by the real parser. Structural checklist S1–S15 mapped to
  DDL: complete. Behavioral live run deferred (no docker/PG here): script committed
  at database/verify_base_model.sql.
- L6 — Slice boundary (1787 authored lines on feat/base-domain-model, past the
  ~400-line review budget). Suggested delivery chain if a PR is opened:
  PR1 = 3b94db6 (docs + canonical diagram) → PR2 = c12c1a3 + 884bccb (init +
  seg schema) → PR3 = 127eefb + 5fdbe96 + 822f2c6 (core + FKs + verify + doc
  sync). Push/PR remain the user's decision.
