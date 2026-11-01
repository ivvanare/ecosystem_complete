# Feature: admin-crud-screens

Objective: replace the hand-written tables of the panel with config-driven screens from `crud-maker` + `crud-maker-zorro`, so listings, filters and pagination come from one reusable layer instead of per-screen HTML — starting with Usuarios, and measuring the bundle before widening.
Branch: `feat/admin-crud-screens` (this repo) + `feat/crud-list-screen` (sibling `crud-maker`)
Delivery strategy: ask-on-risk — the menu was answered once for the previous feature; this feature re-asks before its first commit if the forecast stays over ~400 authored lines
Test runners: `npx ng test admin --watch=false` and `npx ng build` inside `gui/admin`; `npx ng test crud-maker-zorro --watch=false` and `npx ng build crud-maker-zorro` inside `../crud-maker`; compile gate `node node_modules/typescript/bin/tsc --noEmit -p …` where the pid cap blocks the rest (see L3)

## Specs

- **S1** (verbatim, user, L1): "que utilizaste para la ui, ng-zorro? debemos hacer los listados con crudmaker y crudmaker ng-zorro tratar de hacer componentes reutilizables y no html manual que sirvan los filtros paginacion listados que maneje la misma optimizacion y veelocidad de ahorita"

- **S2** (verbatim option chosen, L2 — reuse): "En crud-maker-zorro (repo hermano)", described as: "Agrego un `crud-list-screen` (toolbar con buscador + filtros declarados en el config, y `crud-table`) al repo hermano, rebuild y lo consumo desde acá".
  - Acceptance: the toolbar + table live in `crud-maker-zorro`, are exported from its public API, and the SPA consumes them as `<crud-list-screen [config] [controller] />` without hand-written list markup.

- **S3** (verbatim option chosen, L2 — visual): "Look Ant Design en el panel".
  - Acceptance: panel module screens render with ng-zorro components; landing and login keep their current design.

- **S4** (verbatim option chosen, L2 — scope): "Usuarios primero (medir y decidir)", described as: "Migro sólo Usuarios como vertical de prueba y mido el peso real antes de seguir".
  - Acceptance: the users screen runs on `crud-list-screen`; the measured initial/panel chunk sizes are reported before any other module is migrated.

- **S5** (constraint, derived from `AGENTS.md` "New CRUD entities in `gui/admin` are driven by a JSON-shaped config through external packages"): no listing is hand-written any more. A list screen is a config + a controller; the adapter owns presentation.
  - Acceptance: `grep` finds no `<table>` markup in the migrated screens; each screen has one `defineCrudConfig` object.

- **S6** (constraint, S1's "misma optimizacion y velocidad"): the public landing and login keep their current weight; ng-zorro enters only through the lazily loaded panel routes.
  - Acceptance: the initial bundle stays at the current ~284 kB and any growth lands in panel chunks; both numbers are reported after T2.

- **S7** (gap found while exploring, L4): `crud-table` had no way to render a domain action per row (assigning roles is not create/update/delete), and the adapter had no search/filter UI at all.
  - Acceptance: `crud-table` gains an optional `rowActions` template input; `crud-list-screen` derives search and filter controls from `searchFields` and the fields' `filters`, driving `CrudListState` and `load()`.

## Tasks

| ID | Spec | Route | Status | Commit | Evidence |
|----|------|-------|--------|--------|----------|
| T1 | S2, S7 | inline (sibling repo) | **done, build pending** | (branch `feat/crud-list-screen`) | `crud-table` rowActions slot + `crud-list-screen` + spec; `tsc -p tsconfig.lib.json` and `-p tsconfig.spec.json` green |
| T2 | S2, S3, S6 | human (needs pnpm) | **blocked** | — | `pnpm` is unavailable in this session (L3): the human must build the package, add `ng-zorro-antd` and the `crud-maker-zorro` link, then install |
| T3 | S5, S7 | inline | **pending** | — | `AdminTransport implements CrudTransport` mapping this API's dialect (`page`/`limit`/`q`) |
| T4 | S4, S5, S7 | inline | **pending** | — | Users screen: config + controller + `<crud-list-screen>` + a `rowActions` template that assigns roles |
| T5 | S4, S6 | inline | **pending** | — | measured bundle before/after; go/no-go for the next modules |
| T6 | S1, S6 | inline | **pending** | — | suite + build green, reported by the human runner |

Route map for the migrated screen: `/panel/users` keeps its path, guard and nav entry; only its internals change.

## Log

- **L1** (verbatim, user, 2026-11-01): "que utilizaste para la ui, ng-zorro? debemos hacer los listados con crudmaker y crudmaker ng-zorro tratar de hacer componentes reutilizables y no html manual que sirvan los filtros paginacion listados que maneje la misma optimizacion y veelocidad de ahorita"
- **L2** (verbatim answers, user, 2026-11-01, offered questionnaire): reuse → "En crud-maker-zorro (repo hermano)"; visual → "Look Ant Design en el panel"; scope → "Usuarios primero (medir y decidir)".
- **L3** (environment, parent, 2026-11-01): three blockers, all environmental. (a) `pnpm` is not on PATH and the bundled `corepack` is broken (`Cannot find module './corepack.cjs'`), so no dependency can be installed from this session; adding `ng-zorro-antd` and the `crud-maker-zorro` link therefore needs the human. (b) The 64-pid cgroup cap killed `ng build` and `ng test` before (L12 of `admin-dashboard-shell`), so package builds and suites are the human's or CI's. (c) `git grep`'s default thread pool also had to be capped (`git config --local grep.threads 1`) to keep native review running.
- **L4** (facts, parent, 2026-11-01): what the UI actually is today — no component library; `users`, `roles` and `audit` are hand-written `<table>` markup with component CSS. `crud-maker-zorro` 0.1.0 exists and is built (`dist/crud-maker-zorro`, 92 kB) with `crud-table` (list, server pagination, sort, cells), `crud-form`, `crud-detail` and `crud-cell`, but **no** search/filter UI and **no** per-row action slot. Measured CSS cost of the zorro pieces a list screen needs: `table` 160 kB, `pagination` 60 kB, `button` 48 kB, `input` 32 kB, `select` 44 kB, `tag` 8 kB, `badge` 12 kB, core 24 kB (unminified, per-component entrypoints; the full bundle is 564 kB, so entrypoints are mandatory).

- **L5** (progress, parent, 2026-11-01): adapter committed in the sibling repo on `feat/crud-list-screen` (`e211420`, 376 authored lines): `crud-table` gained the optional `rowActions` template input and the new `crud-list-screen` owns the config-driven toolbar (search from `searchFields`, one control per field exposing `eq`/`like`/`gte`, debounced 250 ms, one request per pause, `Clear` only when something is set). Its specs cover toolbar rendering, single-request debouncing, filter state and the no-toolbar case. Verified with `tsc -p tsconfig.lib.json` and `-p tsconfig.spec.json`; the package build and its vitest suite are still the human's, because esbuild cannot start under the pid cap (two attempts, both starved; a stale `ng test` job was killing pids and was stopped, taking the cgroup from 52 to 43).
- **L6** (progress, parent, 2026-11-01): `AdminTransport` written in the app with its spec — maps `page`/`limit`/`q`, translates `field_kind` filter keys to the API's named params per endpoint (`status` for users; `event`/`userId`/`from`/`to` for the log), drops filters an endpoint does not expose, normalizes `{data,total}` through the engine's own helper, and rejects the mutations this API has no route for. It cannot be committed until the two `tsc` gates run without contention, and the Users migration waits on the dependency install (T2/T3).

- **L7** (verification, parent, 2026-11-01): `ng build crud-maker-zorro` succeeded once the stale `ng test` job was stopped (1940 ms, dist regenerated). The adapter's vitest suite still cannot run here: esbuild's Go service dies with the pid cap (`The service was stopped`) even at one worker, so the adapter suite and the app suite are the human's or CI's. `AdminTransport` is committed (`efd79cd`) after `tsc -p tsconfig.app.json` and `-p tsconfig.spec.json` both came back clean with no pid contention. Next gate is the dependency install, which only the human can run (L3).
