# Feature: admin-dashboard-shell

Objective: give `gui/admin` the structure of the administrative dashboard: public landing at `/`, login at `/login` (no registro, per L3), protected `/panel` with its home and the module screens — the four with a real API first, the other four as "próximamente" structure.
Branch: `feat/admin-dashboard-shell` from `dev` at `f746d3d` (repo root: melia)
Delivery strategy: ask-on-risk — the oversized-delivery menu was asked once (L5); outcome recorded in L6
Test runners: `npx ng test admin --watch=false` and `npx ng build` inside `gui/admin`; compile gate `node node_modules/typescript/bin/tsc --noEmit -p tsconfig.app.json` (precedent: L10 of `odd/tasks/front-api-login.md`)

## Specs

- **S1** (verbatim, user, L1): "comencemos con la estuctura para el dashboard administrativo con su home para el landing, login, registro, y panel con los modulos"

- **S2** (verbatim option chosen, L2 — landing): "Landing pública en / + home del panel", described as: "La SPA sirve una página de producto en / (con CTA a login/registro) y el panel tiene su propio home con KPIs en /panel". Preview shown to the user:
  ```
  Rutas:
  /            → landing pública (hero, beneficios, CTA)
  /login       → acceso
  /registro    → alta con código
  /panel       → dashboard home (KPIs, accesos)
  /panel/...   → módulos
  ```
  - The `/registro` line of that preview is superseded by S3 (see L3): no registro screen is built.
  - Acceptance: `/` renders without a session and without calling a protected endpoint; `/panel` renders the KPIs home; every module lives under `/panel/...`.

- **S3** (verbatim option chosen, L2 — registro): "Sin registro en el panel", described as: "Sólo login; el alta se hace por API/bootstrap como hoy. /login enlaza a una nota".
  - Acceptance: no route and no screen calls `POST /v1/auth/signup`; `/login` shows a short note that accounts are granted by an administrator (API signup + `cmd/bootstrap`), and the note does not leak whether an email exists.

- **S4** (verbatim option chosen, L2 — módulos): "Esos + módulos \"próximamente\"", described as: "Suma Lugares, Menús, Reseñas y Rutas como pantallas de estructura, sin backend". "Esos" is the first option's set, whose preview was:
  ```
  API disponible hoy:
  GET   /v1/admin/users
  PATCH /v1/admin/users/{id}/roles
  GET   /v1/admin/roles
  GET   /v1/admin/permissions
  GET   /v1/admin/audit-events
  GET   /v1/me
  ```
  described as: "Usuarios, Roles y permisos, Auditoría, Cuenta (+ el demo de FAQs que ya existe)".
  - Acceptance: Usuarios, Roles y permisos, Auditoría y Cuenta read real data; Lugares, Menús, Reseñas y Rutas render a structure screen with no network call to a non-existent endpoint and are marked "próximamente" in the navigation.

- **S5** (verbatim option chosen, L2 — diseño): "Primero direcciones visuales", described as: "2-3 artboards en design/ (landing, auth, panel); elegís una y después implemento esa", with preview:
  ```
  design/admin-dashboard/
    01-…html  landing
    02-…html  login/registro
    03-…html  panel + módulos
  Se abren en el canvas para comparar
  ```
  - Acceptance: artboards exist under `gui/admin/design/admin-dashboard/` (see L7 for why the SPA directory, not the repo root) with a synced `design.json`, the canvas is opened, and implementation of T2–T7 uses the direction the user chooses from them.

- **S6** (existing behaviour the change must not break; derived from `odd/tasks/front-api-login.md` S2/S3/S6/S8): the browser session transport stays exactly as it is — refresh token only in the httpOnly cookie, `csrf_token` echoed from `sessionStorage`, access token in memory only, one shared in-flight rotation. `authGuard`, the boot `provideAppInitializer`, `MemoryTransport`, the FAQ demo and the `crud-maker` public API keep working unchanged.
  - Acceptance: the existing specs (`auth.service`, `auth.interceptor`, `auth.guard`, `memory-transport`, `account`) pass without edits to their assertions; the FAQ demo still runs on `MemoryTransport`. **Exception (L8)**: `login.spec.ts` pins the post-login default, which S2 deliberately moves from `/faqs` to `/panel`; those two assertions are updated by design and nothing else in that spec changes.

- **S7** (constraint, derived from `AGENTS.md` "New CRUD entities in `gui/admin` are driven by a JSON-shaped config through external packages" + `api/API.md` §1 pagination): list screens for entities with a real API are `crud-maker` configs over `HttpTransport`, not hand-written fetch tables.
  - Acceptance: Usuarios, Roles y permisos and Auditoría declare a config object per entity and reuse the engine's list state; **assumption A1 — refuted, see L9**: `HttpTransport`/`encodeListQuery` speak `perPage` and have no filter grammar for this API, so the fetch is a thin `AdminApi` inside `gui/admin` and `crud-maker` is not edited from this repository.

- **S8** (constraint, derived from `api/API.md` §6: "El panel decide qué mostrar desde `permissions`, nunca desde el nombre del rol"): navigation and module visibility derive from `permissions`, and `/panel` requires `admin.access`; a session without it gets a denied state instead of a broken panel.
  - Acceptance: with a user lacking `admin.access`, `/panel` renders a permission-denied state and no module call is issued; with `SUPERADMIN`, all modules appear.

- **S9** (assumption A2, to confirm during T4): the FAQ demo moves under `/panel/faqs` and the old `/faqs` URL redirects there, so the existing demo and its spec keep working while the panel owns one navigation tree.

## Tasks

| ID | Spec | Route | Status | Commit | Evidence |
|----|------|-------|--------|--------|----------|
| T1 | S5 | inline | **done** | — | 4 artboards + `design.json` in `gui/admin/design/admin-dashboard/`; canvas opened at http://127.0.0.1:37641/Xu3G26qWwd7ujLALQrNuiAzJkCaYQ10_/d/admin-dashboard/ |
| T2 | S2, S5 | inline or worker | **pending** | — | `/` renders for an anonymous session; tokens/global styles land here |
| T3 | S2, S3 | inline | **pending** | — | no `/registro`; `/login` keeps the CSRF cookie flow and shows the alta note |
| T4 | S2, S4, S8, S9 | inline | **pending** | — | `/panel` shell + KPI home; `admin.access` gate; `/faqs` redirect |
| T5 | S4, S7 | inline | **pending** | — | Usuarios, Roles y permisos, Auditoría, Cuenta against the running API |

Route map agreed in L10:

| Route | Screen | Access |
|-------|--------|--------|
| `/` | Landing (public) | anonymous |
| `/login` | Sign in, with the account-provisioning note | anonymous |
| `/no-access` | Authenticated but without `admin.access` | session |
| `/panel` | Overview: real module list + recent audit events | `admin.access` |
| `/panel/users` | Users list, search, role assignment | `admin.access` |
| `/panel/roles` | Roles with their permissions; permission catalogue | `admin.access` |
| `/panel/audit` | Audit events, filter by event name | `admin.access` |
| `/panel/account` | Profile and sign out (existing `Account`) | session |
| `/panel/places`, `/panel/menus`, `/panel/reviews`, `/panel/routes` | "Coming soon" structure | `admin.access` |
| `/panel/faqs` | Existing `crud-maker` demo | `admin.access` |
| `/account`, `/faqs` | Redirects to the panel routes above | — |
| T6 | S4 | inline | **pending** | — | Lugares, Menús, Reseñas, Rutas as "próximamente" screens, zero calls |
| T7 | S6 | inline | **pending** | — | full suite + build green; specs not weakened; docs updated |

Writer trigger check: T1 is one cohesive visual system (splitting it across writers would fork the design language) → parent. T2–T6 are sequential edits in the same few files (`app.routes.ts`, `styles.css`, panel layout) with no two independent units heavier than a subagent start → parent, re-evaluated at T5 once the shell exists.

## Log

- **L1** (verbatim, user, 2026-10-31): "comencemos con la estuctura para el dashboard administrativo con su home para el landing, login, registro, y panel con los modulos"
- **L2** (verbatim answers, user, 2026-10-31, offered questionnaire): landing → "Landing pública en / + home del panel"; registro → "Sin registro en el panel"; módulos → "Esos + módulos \"próximamente\""; diseño → "Primero direcciones visuales".
- **L3** (decision, parent, 2026-10-31): L1 asks for "registro" and L2 answers "Sin registro en el panel". The later, explicit answer governs: no registro screen is built, and S2's `/registro` line is dropped in favour of S3. Rationale: the panel is an internal backoffice where accounts are granted (`cmd/bootstrap`, `PATCH /v1/admin/users/{id}/roles`); a public self-signup would let anyone verify an email and hold a session with zero permissions, and `/v1/auth/signup` already exists for whoever needs it by API. Adding the screen later is additive — it needs the two-step code form and nothing else.
- **L4** (baseline, parent, 2026-10-31): `dev` at `f746d3d`, working tree clean, branch `feat/admin-dashboard-shell` created from it. Existing front surface: `/login`, `/account`, `/faqs` (demo), `AuthService` on the browser transport, `authGuard`, `HttpTransport` available in the linked `crud-maker` build. No design sources exist (`design/` absent, `src/styles.css` is only `@import 'tailwindcss'` with no `@theme`), so the design is authored freely and no design-system sync applies.
- **L5** (process, parent, 2026-10-31): authored-line forecast for T2–T7 is ~1400–1900, over the ~400 delivery budget, so the oversized-delivery menu is asked once before the first commit.
- **L6** (design, parent, 2026-10-31): `gui/admin/design/admin-dashboard/` holds four artboards — two different landing strategies (`01-landing-editorial.html`, `02-landing-product.html`, same content, different visual strategy), the auth screen (`03-auth-split.html`) and the panel shell with modules (`04-panel-modules.html`). Design system: none exists in the project, so the artboards propose one (warm editorial vs cool operational) and `design/system.md` is only written if the user asks for the sync.
- **L7** (decision, parent, 2026-10-31): the design lives under `gui/admin/design/` instead of the repo root. `gentle_design` resolves `design/` from the session cwd, which is `gui/admin`, and the folder also stays on the frontend side of the commit-scope guardrail (`AGENTS.md` "Commit Scope"). No other consequence: nothing in the SPA build reads it.
- **L8** (decision, parent, 2026-10-31): the user chose the product direction (L2 answer) and the `feature-branch-chain` delivery strategy. Implemented copy is **English**, following the SPA's existing convention (`login.html`, `account.html`), while the artboards keep Spanish marketing copy for the Venezuelan audience — the artboards are design deliverables, not source. One consequence, recorded because it contradicts S6 as first written: the post-login default moves from `/faqs` to `/panel`, so the two assertions in `login.spec.ts` that pin that default are updated. Every assertion about the session transport, the error code and the submit guard stays untouched.
- **L9** (finding, parent, 2026-10-31, T5 prep): assumption A1 is false. `crud-maker`'s `encodeListQuery` sends `perPage` (`params.ts:11`) while this API reads `limit` (`api/openapi.yaml` `components.parameters.Limit`), and it has no grammar for `event`/`userId`/`from`/`to`. Remedy inside `gui/admin` only: `AdminApi` maps `CrudListState.query` to this API's dialect, and `HttpTransport` is left for endpoints that speak its dialect. `crud-maker` is not edited from this repository (one source of truth, its own repo).
- **L10** (decision, parent, 2026-10-31): route map and access rules are recorded in `## Tasks`. `/no-access` renders outside the panel chrome so a session without `admin.access` never sees the sidebar; `/panel` requires `authGuard` + a new `adminAccessGuard`; the old `/account` and `/faqs` URLs redirect, so nothing that points at them breaks.
- **L11** (incident, parent, 2026-10-31): both writer subagents handed off for T4–T6 failed with `provider_unavailable` before writing a single file. No output existed to merge, so the two units moved back to inline execution; the delegation attempt cost two handoffs and no code. Nothing was relaunched, because the units are now being implemented directly.
- **L12** (environment, parent, 2026-10-31): this session's cgroup caps pids at **64** (`/sys/fs/cgroup/.../pids.max`), and `pids.current` sits around 52 with an active dev server. `npx ng build` and `npx ng test` therefore die at startup with `Cannot fork` / `uv_thread_create` assertion when two run at once, and can still starve when run alone. The compile gate used instead is `node node_modules/typescript/bin/tsc --noEmit -p tsconfig.app.json` and `-p tsconfig.spec.json`, both green. Suite and production build are attempted one job at a time; whatever the cap still blocks is reported as unverified here and belongs to CI, never claimed as passing.
