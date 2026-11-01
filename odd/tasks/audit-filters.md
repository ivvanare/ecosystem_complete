# Feature: audit-filters

Objective: let the audit screen narrow by user, email hash and a date range, and make truncated cells readable — a tooltip on the value the formatter shortened.
Branches: `feat/audit-filters` (melia) + `feat/crud-list-screen` (sibling `crud-maker`, where the adapter changes live)
Test runners: `go test ./internal/httpapi/ ./internal/identity/ ./internal/contract/` inside `api/`; `npx ng test admin --watch=false` and `npx ng build` inside `gui/admin`; `npx ng test crud-maker-zorro --watch=false` in the sibling repo

## Specs

- **S1** (verbatim, user, L1): "en auditoria agrega filtro por usuario e email hash, y la fecha en rango"
- **S2** (verbatim, user, L1): "cuando una tabla o el dato es muy largo se trunca y no hay manera de leerlo completo seria bueno un tooltip"
- **S3** (constraint, found in L3): `GET /v1/admin/audit-events` accepts `event`, `userId`, `from` and `to` only. User and date range are already supported by the API; **email hash is not**, so it needs a contract change (param + handler + store + docs + tests).
- **S4** (constraint): `from`/`to` are RFC 3339 (`api/API.md`), while a date input yields `YYYY-MM-DD`; the panel normalises to day boundaries before sending, and a malformed value still answers 422 `validation_failed`.
- **S5** (constraint, adapter): the toolbar must drive `between`, which it rejected by design until now (only `eq`, `like` and `gte` were supported).

## Tasks

| ID | Spec | Status | Evidence |
|----|------|--------|----------|
| T1 | S3 | in progress | API: `emailHash` on the audit filter, handler, SQL, openapi + API.md, tests |
| T2 | S2 | pending | adapter: `crud-cell` exposes the untruncated value as a title when the text differs |
| T3 | S5, S4 | pending | adapter: toolbar renders a from/to pair for `between`, with native date inputs |
| T4 | S1, S3, S4 | pending | app: audit config gains user, email hash and date-range filters; transport maps them |
| T5 | all | pending | suites + builds green (human/CI for the SPA) |

## Log

- **L1** (verbatim, user, 2026-11-01): "ahora si paso el test, en auditoria agrega filtro por usuario e email hash, y la fecha en rango lo otros que cuando una tabla o el dato es muy largo se trunca y no hay manera de leerlo completo seria bueno un tooltip"
- **L2** (baseline, parent, 2026-11-01): the suite the human ran is green after the CSRF seeding fix (84 tests). The audit config already declares `user_id`, `email_hash` and `created_at` as columns, so the screen shows the values but cannot filter on two of them.
- **L3** (finding, parent, 2026-11-01): `api/internal/httpapi/admin.go:131` builds `identity.AuditFilter` from `event`, `userId`, `from`, `to`; the store query (`admin_postgres.go:139`) has exactly four `$n` parameters. Adding a filter means a fifth parameter and renumbering `LIMIT`/`OFFSET`.

- **L4** (layout defect, user, 2026-11-01): the date range overlapped the next control — "los btn de when se superpone sobre event". Cause is CSS, not width of the window: a native date input refuses to shrink below its intrinsic width, `.crud-toolbar-field` capped every control at 320 px, and the pair sat in a `display: flex` wrapper without `min-width: 0`, so it painted over its neighbour. Fixed in `bfddb7e`: the range field gets a modifier class with room for two dates and each input gets `flex: 1 1 0; min-width: 0`. `6cc61d6` also moved `event` to the front of the audit config, so the toolbar reads Event → When → User → Email hash and the column that names the row leads the table.
- **L5** (delivery note, parent, 2026-11-01): the adapter branch now carries **5 commits / 481 authored lines** since `main`, past the ~400-line heuristic, and it serves two features (the list screen and, on top, the theme colours and the range fix). Nothing is pushed, so no rule is broken; when it is published the natural slice boundary is `e211420` (list screen + its spec) against `38afee9` + `ba69d7b` + `bfddb7e` (theming + range), which keeps each PR reviewable on its own.

- **L6** (verification, human runner, 2026-11-01, feature close): everything green — SPA **19 files / 88 tests**, adapter **5 files / 31 tests**, `ng build` clean with no budget warnings, `go vet` plus the `httpapi`, `identity` and `contract` packages, and the filter verified against the rebuilt container (emailHash 14 rows all matching, userId 28, range 52). Open, not forgotten: the email-hash column is truncated to 12 characters, so the value can be read in the tooltip but not copied into the filter — the human has three options on the table (a copy action per cell, the full hash in the column, or prefix matching which would need `LIKE` in the store). Nothing is pushed; publishing order is recorded in L19 of `admin-crud-screens`.
