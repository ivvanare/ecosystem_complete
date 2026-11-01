# db-history — point-in-time history for the entities whose past has analytical value

Adds append-only `*_history` tables with validity intervals, maintained by database
triggers, so "what was the price when the review was written" and "who was ADMIN on
date X" become answerable. Additive: no existing column changes, no Go model changes.

Branch: `feat/api-db-history` · Worktree: `/home/ivanare/proyects/melia-worktrees/db-history`

## Specs

S1. Point-in-time on mutable business attributes is required. User answer to the
    question "¿Te importa reconstruir el pasado — «qué precio tenía este plato cuando se
    escribió esta reseña», «cómo evolucionó el rating de este lugar»?" was, verbatim:
    "Sí → hay que agregar historia".

S2. Scope is exactly six entities, the option the user selected verbatim:
    "Recomendado: 6 entidades con valor analítico".
    - `core.tb_menu_item` — price, name, description, status, menu, soft delete
    - `core.tb_place` — owner, name, title, description, category, lat/long, address,
      social media, status, soft delete
    - `core.tb_place_schedule` — day, open, close (this table has physical deletes, so
      today its past is destroyed outright)
    - `core.tb_review` — rating, comment, status, soft delete
    - `seg.tb_user_role` — user/role link (physical deletes today)
    - `seg.tb_role_permission` — role/permission link (physical deletes today)

S3. Out of scope, verbatim from the selected option: "tokens, signup y refresh (estado
    operativo efímero, ya cubierto por `tb_security_event`); `tb_place_photo`,
    `tb_route`, `tb_route_place` (valor analítico bajo hoy)."

S4. Mechanism is trigger-maintained history tables with validity intervals, because it
    cannot be bypassed by any future Go code path and it covers physical deletes. It is
    additive: no existing column, index or constraint changes, so the `api` session's
    in-flight Go models stay valid.

S5. History must not duplicate `tb_security_event`. The event log records the act and
    the actor; history tables record the resulting state at an instant. Neither replaces
    the other.

S6. No backfill is required or attempted: on the day this lands the six source tables
    are empty (0 places, 0 menus, 0 items, 0 schedules, 0 reviews, 0 routes; the only
    `tb_user_role` row is a dev fixture). Seeds that already ran produce no history, by
    design.

S7. `database/verify_base_model.sql` is the runnable drift check and must assert both the
    new structure and the new behavior, and must keep rolling back so the database is
    left untouched.

S8. `database/diagram_base.txt` is the canonical spec and stays the source of truth:
    every history table and the `change_operation` enum are declared there, with triggers
    documented as an implementation note (DBML cannot express them).

## Tasks

- T1 · S1, S2, S7 — Structural checks in `verify_base_model.sql` (observe RED) · inline · C1
- T2 · S2, S8 — `07-history-tables.sql` + diagram tables/enum · inline · C1
- T3 · S2, S4, S7 — `08-history-triggers.sql` + behavioral checks (RED → GREEN) · inline · C2
- T4 · S4 — Apply 07/08 to the dev database with matching ledger rows · inline · C2
- T5 · S1..S8 — Full verify + `go test ./...` + commit slices · inline · C1, C2
- T6 · S5 — Tell the `api` session history is additive and trigger-owned · inline · —

## Log

L1 (user, verbatim): "buenao analizando el modelo que nos falta, seria buena idea crea un modelo BI para analitics esperemos a madurar mas el modelo de datos?"

L2 (user, verbatim): "Sí → hay que agregar historia"

L3 (user, selection, verbatim): "Recomendado: 6 entidades con valor analítico"

L4 (evidence, read-only): `database/verify_base_model.sql` against the live database
printed "ALL CHECKS PASSED — base model matches database/diagram_base.txt" before this
feature started, and the ledger held 02, 03, 04, 05 and 06 as applied.

L5 (decision): separate worktree `feat/api-db-history` instead of switching branches in
the main worktree, because the `api` session is actively working there and must not lose
its HEAD or its uncommitted work.

L6 (decision): intervals use `now()` and `CHECK (dt_valid_to >= dt_valid_from)` so a
same-transaction change still closes and opens at the same instant with no gap and no
constraint violation. Zero-length intervals are accepted and documented.
