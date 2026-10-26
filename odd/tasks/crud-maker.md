# Feature: crud-maker

Objective: Build `crud-maker`, an Angular 22 library in the melia gui/admin workspace that generates CRUD behavior from a simple, JSON-serializable config, with signal-based reactive state and best-practice architecture (reference: tymon/admin engine, weaknesses fixed). ng-zorro UI comes in a later deliverable; this document covers the base logic first.
Branch: feat/go-api (repo root: melZone)
Delivery strategy: ask-on-risk (slices ~400 authored lines, tests+code together; push/PR remain user decisions)
Test runner: `npx ng test crud-maker` (vitest, inside gui/admin workspace)

## Specs

- **S1** (verbatim): "vamos a crear una especie de libreria que se llame crud-maker para hacer los crud de manera rapida con un simple json de configuracion"
  - Acceptance: one serializable JSON config per entity defines fields, views (list/form/detail), filters, sort, pagination, actions; no TS classes with embedded functions.
- **S2** (verbatim): "puedes verificarlo en este repo /home/ivanare/proyects/tymon/admin/, mira como funcionan las paginas y los componentes (utilizaremos ng-zorro)"
  - Acceptance: reference architecture verified (see Log L2 handoff). Port the engine idea (attributes → table/form, fluent query, one generic controller). ng-zorro integration is deferred to T4 and must NOT leak into the base logic (UI-agnostic core).
- **S3** (verbatim): "primero haremos la base logica del crudmaker superoptimizado con las mejores practicas y el uso of signals para reactividad"
  - Acceptance: base logic = config schema + validation/normalization + signal state + CRUD engine over a pluggable transport. Uses Angular v22 best practices verified via `get_best_practices` MCP: signals + computed, no explicit OnPush/standalone flags, `@Service`, `inject()`, no `mutate`, pure derivations, strict types, no `any`.
- **S4** (derived from tymon weaknesses fix — the "mejores practicas" of S3 applied): 
  - A4.1 config is pure JSON → custom behaviors resolved by registered names, not embedded closures.
  - A4.2 one config per entity (single `fields` list with per-view flags), not duplicated table/form configs.
  - A4.3 per-instance controllers, not `providedIn:'root'` mutable singletons.
  - A4.4 no hard dependency on UI libs, auth, i18n or toast services in the core (interfaces/hooks).
  - A4.5 all list state (page, sort, filters, search, loading, error) is signal-backed and derived via computed; no NaN on unset pagination params.

## Tasks

| ID | Spec | Route | Status | Commit | Evidence |
|----|------|-------|--------|--------|----------|
| T1 | S1, S4.1, S4.2 | inline | **done** | 1e5dfdf (+0452437 swept types/registry/specs) | RED observed (TS7006/missing modules) → GREEN: 36/36 tests pass, 490ms |
| T2 | S3, S4.5 | inline | **done** | bd7efb7 | RED observed (Cannot resolve './crud-list-state') → GREEN: 58/58 tests pass |
| T3 | S1, S3, S4.3, S4.4 | inline | **done** | a7f0cae | RED observed (Cannot resolve ./params, ./crud-controller, ./http-transport) → GREEN: 72/72; `ng build crud-maker` packages; admin suite 2/2 |
| T4 | S2 (ng-zorro pages/components + route gen) | inline | blocked-pending-approval | — | requires `ng add ng-zorro-antd` authorization |
| T5 | consumer docs for the package | inline | **done** | see `docs(crud-maker)` commit | README rewritten + AGENTS.md pointers; `ng build crud-maker` still green |

Writer trigger check: T2/T3 depend on T1 types (not independent units) → no parallel-writer reason; parent implements inline following this logbook. Output budget: lib tests via ng test; app suite at seam check.

## Log

- **L1** (original request, verbatim): "vamos a crear una specie de libreria que se llame crud-maker para hacer los crud de manera rapida con un simple json de configuracion, puedes verificarlo en este repo /home/ivanare/proyects/tymon/admin/, mira como funcionan las paginas y los componentes (utilizaremos ng-zorro), pero primero haremos la base logica del crudmaker superoptimizado con las mejores practicas y el uso de signals para reactividad"
- **L2** (exploration evidence, read-only explorer over tymon/admin, ses_e8f02a832ffecNmQCu8gMuZ67f, saved as engram obs 2197): Angular 20.3 zoneless PrimeNG app. CRUD "config" is actually a TS service per entity extending `BaseResourceService` (1130-line `providedIn:'root'` singleton) declaring `Attribute[]` with embedded closures (`table.getter`, `input` FormFields, `can*` overrides). Generic renderers (`app-table`, `app-form` 14 types, paginator) receive the model instance. Fluent query builder → REST params (`col_op=value`, `col:DESC`, `page,perPage`). Only one full vertical exists (FAQ). Weaknesses adopted as S4:1 non-serializable config, 2 duplicated table/form configs, 3 shared mutable singletons via `Object.create` clones, 4 hard deps (AuthService/MessageService/sessionStorage in model), 5 DOM hacks, 6 half-implemented detail mode, 7 signals on 2 fields only, 8 manual routing + `parseInt('')` NaN.
- **L3**: Best practices verified via `angular-cli` MCP `get_best_practices` (v22): Signal Forms stable, `@Service` over `@Injectable`, no explicit OnPush/standalone, `input()/output()/model()`, `computed/linkedSignal`, no mutate/ngClass/ngStyle.
- **L4** (delivery hazard, 2026-10-24): A concurrent agent session working on api/k6 keeps `git add -A`ing the whole worktree: it swept T1's types/registry/specs into `0452437 test(k6): fix the signup error-rate threshold...`, and the whole gui/admin demo into `03891b3 feat(api): add login with lockout...` (content committed, message attribution wrong). Work continues on `feat/go-api`; each intended T gets its own commit when possible, but commit hygiene across the two sessions needs user awareness.
- **L5** (2026-10-26, demo): Built the FAQ demo vertical in src/app/demo (config JSON + MemoryTransport + plain-HTML screen with v22 Signal Forms). Evidence: MemoryTransport RED→GREEN, app suite 10/10, `ng build admin` OK (faqs-demo lazy 23 kB). Discovery: stable Signal Forms binds with **[formField]** and exportAs formField; NG8022 forbids native required/minlength/min on [formField] nodes — validation comes from the form() schema. Files landed in the concurrent session's commit 03891b3 (same sweep hazard as L4).
- **L6** (docs, 2026-10-26): Rewrote `projects/crud-maker/README.md` as the consumer guide (quick path → config/registry/state/engine/transport reference → out-of-scope → checklist), facts cross-checked against source this session; added gui/admin + crud-maker pointers to AGENTS.md (structure + gotchas). `ng build crud-maker` re-verified (README ships in dist).
- **Next**: T4 (ng-zorro pages/components + route generation) requires explicit approval for `ng add ng-zorro-antd`. Base logic (T1–T3), demo and docs are complete.
