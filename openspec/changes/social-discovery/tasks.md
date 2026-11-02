# Tasks: Social Discovery — Nearby Places, Plans, and People

Change: `openspec/changes/social-discovery/`
Specs: `spec.md` (S1–S26) · Design: `design.md` (D1–D20 + open decisions Q1–Q6) ·
P1 contract detail: `p1-discovery.md`

Only **P0 and P1** are cut into executable tasks, because they are the next unit of work.
Phases 2–4 are listed as backlog with their rules pinned in `spec.md`, so their tasks can be
cut without redesigning.

Revised 2026-11-02 (see Log L7 and L10): PostGIS moves into P0/P1 and the decimal columns stop being
the query mechanism (D2 revised); the H3 column is dropped in favour of computing cells in
Go (D13); administrative polygons are out of P1 (D14); cached read paths are viewer-neutral
(D15); invitations become a lifecycle slice of their own (D16); the reviews write API joins
P1 so trending has a real signal (D17).

---

## Pre-flight (blocks P0 and P1)

| ID | Task | Specs | Notes |
|----|------|-------|-------|
| P0.1 | Branch off `dev` as `feat/api-geo-foundation` | — | `dev` is integration; no direct push |
| P0.2 | Announce the `database/diagram_base.txt` and `docker-compose.yml` edits | — | Shared files per `AGENTS.md`: the diagram is the canonical contract |
| P0.3 | Confirm which role runs migrations in production | S16 | `CREATE EXTENSION postgis` needs a privileged role; the compose user is the init superuser, production is not |
| P0.4 | Decide check-in validation depth (Q1) | — | **Only blocks P2**, not P0/P1. Recorded so it is not forgotten |

---

## Phase 0 — `geo-and-scheduler`

Two commits, ~400 authored lines each. Nothing here serves a feature on its own, and that is
the point: three later features depend on a scheduler that does not exist today, and the
image change is cheaper once than twice.

### Commit 1 — PostGIS in the deployment

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T0.1 | `docker-compose.yml`: `postgres:15-alpine` → `postgis/postgis:15-3.x`; mirror in the CI job; confirm the existing volume stays valid (same PostgreSQL major) | S16 | inline |
| T0.2 | Ledger `15-postgis-extension.sql`: `CREATE EXTENSION IF NOT EXISTS postgis` | S16 | inline |
| T0.3 | Verify the extension on a throwaway database and on the existing dev volume; confirm the rollback path (`DROP COLUMN` + previous image) is written down | S16 | inline |

### Commit 2 — spatial column, region, scheduler

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T0.4 | Ledger `16-place-geo.sql`: `co_location geography(Point,4326)` derived from the decimals — `GENERATED ALWAYS AS (...) STORED` where the expression qualifies as immutable, otherwise a trigger — plus a GiST index | S16 | test-first |
| T0.5 | Test asserting the derived value agrees with `nu_latitude`/`nu_longitude` on insert and on update | S16 | test-first |
| T0.6 | Ledger `17-place-region.sql`: `tx_region varchar(6) NULL` + `ck_tb_place_region` shape CHECK; mirror the column into `tb_place_history`; `CREATE OR REPLACE` the place history function so the payload carries it | S3, S15 | inline |
| T0.7 | Index `ix_tb_place_country_region (tx_country, tx_region)` — navigation reads use both, in that order | S3 | inline |
| T0.8 | Scheduler in `cmd/worker`: `Ticker` with configured interval, graceful shutdown on signal, a job registry, and one no-op job wired end to end | S4, S12, S19 | test-first |
| T0.9 | `database/diagram_base.txt` updated with `tx_region`, `co_location` and the index. Column comments live in the new ledger files: `13-comments.sql` is already recorded in the ledger, and an applied migration is never edited | S3, S16 | inline |
| T0.10 | Verify: `go test ./...` green, ledger `15..17` applies in lexical order on a fresh database | S14 | inline |
| T0.11 | Work-unit commits (2) — image + extension, then columns + scheduler | — | inline |

Deferred out of P0 on purpose: `ST_Contains` and any administrative polygons (D14, no data
and no consumer yet); `h3-pg` (D13, the cell is computed in Go).

---

## Phase 1 — `place-discovery`

Four commits: places read, place detail and reviews, search and filters, trending producer.
`p1-discovery.md` holds the contract-level detail (routes, payloads, SQL shape, cache keys,
error mapping, test plan) and is what a subagent implements from. Phase 1 is bigger than a
single slice on purpose — a trending read path with no producer, no signal and no search was
what the revisions in L7 and L10 exist to fix — and each commit is reviewable on its own.

### Commit 1 — the discovery read path

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T1.1 | Contract first: `GET /v1/places` with `near=lat,long`, `radius` (default 2000 m), `bbox`, `zoom`, `country`, `region`, `category`, `sort`, paginated — into `openapi.yaml` and `API.md` **before** the handler | S14 | inline |
| T1.2 | Coordinate, radius, bbox and zoom validation at the application edge: finite, in range, bounded — `422 validation_failed` with `details.fields`, before any database work | S1, S2 | test-first |
| T1.3 | Radius query with `ST_DWithin` and viewport query with the `&&` envelope operator, both on the GiST index, with a result cap; no persisted coordinate, coordinate never logged | S1, S16 | test-first |
| T1.4 | In-memory H3 clustering: one resolution, parent cells by `cellToParent`, grouped counts, oversized bbox rejected rather than silently truncated | S20, D13, D14 | test-first |
| T1.5 | `internal/contract` satisfied: route, error codes and environment variables present in `openapi.yaml`, `API.md`, `README.md`, `.env.example` | S14 | inline |

### Commit 2 — place detail and the reviews write path

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T1.6 | Contract first: `GET /v1/places/{coPlace}` plus `POST`/`PATCH`/`DELETE /v1/places/{coPlace}/reviews` and the paginated read, into `openapi.yaml` and `API.md` before the handlers | S14, S24 | inline |
| T1.7 | Detail read: place, category, attributes, rating aggregate, open/closed state at an instant; `404` for soft-deleted or `INACTIVE`; `tx_name` and `co_owner_user` never serialized | S24 | test-first |
| T1.8 | Create: one active review per user and place, `23505` on `ux_tb_review_user_place_active` mapped to `409 review_already_exists` — never a 500 | S17 | test-first |
| T1.9 | Author-only edit; logical delete so the user may review again; `MODERATED`/`INACTIVE`/deleted hidden from other readers, visible to author and moderators | S17 | test-first |
| T1.10 | Moderation endpoint gated by the existing `review.moderate` capability; `app.current_user_id` set per transaction so the trigger records `co_actor_user`, and the service never writes history | S15, S17 | test-first |

### Commit 3 — search and filters, with their migration

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T1.11 | Ledger `18-place-attributes.sql`: `tb_place_attribute` + `tb_place_attribute_link` with composite PK, both in `core`, plus governed seed rows (`terraza`, `petfriendly`, `frente_playa`, `rumba` stays a category) | S23, D18 | inline |
| T1.12 | Ledger `19-place-search.sql`: `CREATE EXTENSION IF NOT EXISTS pg_trgm` and `unaccent` (both confirmed available), generated `tsvector` columns on `tb_place` (name) and `tb_menu_item` (name + description), GIN indexes on each | S21 | inline |
| T1.13 | `q` place-name search and `dish` menu search as separate parameters, accent- and case-insensitive, composing with the existing spatial and attribute filters | S21 | test-first |
| T1.14 | Attribute filter with conjunctive semantics, and `openAt` answered from `tb_place_schedule` in the place's own `tx_timezone`, honouring schedules that close after midnight | S23 | test-first |
| T1.15 | Price filter: `maxPrice` in USD, matches places with an active USD item under the amount via an indexed `EXISTS` join; no `currency` parameter, no rate, and an item in another currency simply does not match | S22, D19 | test-first |

### Commit 4 — the trending producer and read path

| ID | Task | Specs | Route |
|----|------|-------|-------|
| T1.16 | Worker job: per-place score (review recency velocity + volume-weighted rating), aggregated per H3 zone cell in Go, written to Redis in one pipeline with a TTL | S4, D13, D17 | test-first |
| T1.17 | Read path: order by the zone key, fall back to the nearest enclosing parent cell, then to rating-then-name — never aggregate `tb_review` in a request; `sortApplied` and `degraded` echo what actually ran | S4 | test-first |
| T1.18 | Cache payload asserted viewer-neutral, and no cache key derived from the request coordinate | S18 | test-first |
| T1.19 | Verify: `go test ./...` green, `internal/contract` green, ledger `15..19` applies in lexical order on a fresh database | S14 | inline |
| T1.20 | Work-unit commits (4) and close Phase 1 | — | inline |

Deferred out of Phase 1 on purpose, each with its reason:

- **Region assignment by reverse geocoding** — the column and its CHECK ship in T0.6, but
  nothing in the service creates places yet (D17 defers place CRUD), so an assignment hook
  would have no caller. It ships with the places write path.
- **Administrative polygons and `ST_Contains`** — no data source, and no consumer until names
  matter (D14).
- **Cross-currency price search** — Phase 1 is USD-only (D19, resolved). An optional
  `currency` parameter plus `tb_fx_rate` is additive when it is needed.
- **A stored H3 cell and country-scale map views** — additive later, with a backfill (D13).

## Phase 2 — `social-graph` (backlog, rules pinned)

Tables: `core.tb_follow`, `core.tb_checkin`.
- Follow with composite PK, no soft delete, `CHECK (co_follower <> co_followed)` (S5).
- Friend = mutual follow, computed at read (S5, D6).
- Check-in append-only, unique per user/place/day (S10).
- Viewer-relative highlight in the review read path, bounded and paginated (S6, D10).
- **Any cache introduced here is viewer-neutral; the highlight is applied after it** (S18,
  D15). This is the rule that makes the feed cacheable at all.
- Blocking before public exposure (Q2).

## Phase 3a — `plan-collaboration` (backlog, rules pinned)

Enums: `public.plan_visibility`, `public.plan_role`.
Tables: `core.tb_route_member`, `core.tb_route_place_vote`; `tb_route.tx_visibility`.
- Visibility defaults to `PRIVATE`; public plans are moderated (S7, S8).
- Self-join grants `VIEWER` only; promotion is an owner action (S8, D8).
- Vote ranking deterministic; owner override recorded (S9, D9).
- Live vote propagation over Soketi (S9).
- `dt_scheduled_at` and `nu_party_size` on `tb_route`, mirrored into its history payload with
  the trigger function replaced, not recreated (S25, S15).
- Party size numeric, not an enum; capacity on public plans deferred (Q6).
- `FOLLOWERS_ONLY` deferred (Q3).

## Phase 3b — `plan-invitations` (backlog, rules pinned)

Enum: `public.plan_invitation_status`. Table: `core.tb_plan_invitation`.
- State machine `PENDING → ACCEPTED | DECLINED | EXPIRED | REVOKED`, one live invitation per
  plan and user via a partial unique index (S19, D16).
- Accepting creates the `VIEWER` membership in the same transaction; declining is terminal
  and creates nothing (S19, S8).
- Expiry enforced by the read predicate; the scheduled sweeper only flips stale rows (S19).
- Registered users only: no anonymous shareable token in this phase (S19).
- **Host RSVP view** (the requirement behind the human's truncated sentence "saber que mi ,",
  resolved in L12): `GET /v1/plans/{coRoute}/invitations` for `EDITOR`/`OWNER`, returning
  invitee, status, expiry and response time; no migration, contract-first like any route.
- An invitee reads only their own invitation and the accepted-member list — never who declined
  (S19).
- `tx_mode` (`PLAIN` default | `SURPRISE`), `tx_message` and `tx_hint` on the invitation.
  `SURPRISE` returns a redacted projection: message, date and time, hint, coarse area — never
  the place name, address, coordinates, category, menu or photos (S26, D20).
- `SURPRISE` is rejected without `dt_scheduled_at` and without at least one of message or
  hint; the host picks `AT_SCHEDULED` (default) or `ON_ACCEPT` as the reveal rule.
- Expiring invitations ride the P0 scheduler; no new job runner is introduced here.

## Phase 4 — `taste-profile` (backlog, rules pinned)

Table: `core.tb_user_taste_profile`.
- Worker batch computes aggregate affinities from explicit signals only (S11, S12).
- Profile is deletable and rebuilt on next batch; absence falls back to trending (S12).
- Notifications derive from enumerable, individually disableable triggers (S13).

---

## Log

- L1 — Original request (verbatim): "segun los ultimos cambios de bd que tenemos que
  ajustar en la api?" Answered read-only: no corrective work — the API touches only `seg.*`
  today and nothing references `tb_place`/`tb_menu_item`; the pending items are forward
  requirements (mandatory locale columns, timezone validation at the edge, optional
  `app.current_user_id` actor).
- L2 — User question (verbatim): "sera una tabla con relaciones a las otras para decidir si
  estar disponibles en una region ? y eso afecta los usuarios si se quieren segmentar por
  region". Settled in D4: country stays an ISO code, region is a column, availability is a
  relation that is not built without a consumer.
- L3 — Product vision (verbatim): "vamos a necesitar saber como segmentar mis usuarios
  para poder recomendarles lugares de su zona lo que esta cer de ellos a menos de 2km por
  ejemplo los que estan trendy ... general por pais y por estado ... como si fuera un
  airbnb ... pero de lugares que visitar planificar con amigos, cosas asi, algo muy social".
  Redirected the framing from "segment users by region" to query-driven proximity (D1).
- L4 — User question (verbatim): "deberia de guardarme la ubicacion para aprender de los
  gustos y recomendarle a ese usuario en el futuro o notificaciones". Settled in D11: no
  raw location storage; explicit signals plus a regenerable derived profile, with one
  bounded opt-in coarse zone for notifications.
- L5 — User request (verbatim): "ok tomemos las decisiones ps dame el plan y las acciones a
  tomar de lo que hablamos". Produced this change: D1–D12 settled, S1–S15 pinned, Phase 1
  cut into tasks, Q1–Q3 left explicitly open.
- L6 — Additions to the plan (verbatim): "la planificacion deberia de ser elegible para
  privado y publico asi puediera armar un festival publico y la comunidad se une o creo uno
  privado para mis panas, tambien seria genial poder tener seguidores y crear rutas que las
  personas voten como spotify y amigos como instagram para saber oye mi amigo estuvo aqui me
  resalta sobre las demas comentarios" → S6–S10, D6–D10, Phase 2 and Phase 3.
- L7 — Architecture revision (2026-11-02). The human settled the geo layers and added scope.
  Two gaps had been recorded against the previous version: the map viewport and clustering
  were specified nowhere, and the trending ranking had a read path with no producer. This
  entry closes both, and finds a third while doing it.
  - Layering, verbatim: "1. PostGIS: El cerebro de tus datos y reservas (Capa de detalle) ...
    Búsquedas de precisión: Localizar exactamente los locales que están a menos de 1 km del
    usuario (ST_DWithin) o dentro del marco visible de la pantalla (bounding box /
    ST_MakeEnvelope). Zonificación real ... polígonos exactos con ST_Contains. Consultas
    relacionales ... 2. H3: El motor visual del mapa y feeds populares (Capa tipo
    Instagram/Airbnb) ... Clustering ultra rápido (estilo Airbnb) ... Mapas de calor / "Zonas
    Trending" ... Caché y Feed geoespacial ... el ID del hexágono como clave."
  - Product framing, verbatim: "mas meno lo que mostramos aqui es lo que planeo realiza
    http://localhost:4321 tener esas funcionalidades y que pueda hacer invitaciones a grupos
    de amigos que desidan si van o no que estas asu vez expiren en un tiempo, claro esta,
    hacer planes abiertos tipos conciertos publicos etc, saber que mi ," — the last clause
    arrived truncated and was **not** guessed. **[Resolved in L12: it was the host's RSVP
    view, "saber que mi invitado aceptó".]**
  - Decisions taken by the human this turn: PostGIS enters in P1, paying the image change
    once (D2 revised); no administrative polygons, an H3 cell stands in for a neighbourhood
    until real data exists (D14); invitations address registered users only, no shareable
    token (D16); the reviews write API joins P1 so trending has a real signal (D17).
  - Consequence corrected before any code: a cell-keyed cache serving viewer-relative
    highlights would leak one user's friend set to another; the cache is now required to be
    viewer-neutral (D15, S18).
  - Consequence corrected before any code: `tx_region`'s reverse-geocoding assignment had no
    caller, because the service has no place write path; it is deferred with its reason
    rather than written as a task that silently does nothing.
- L9 — Planning correction found while writing `p1-discovery.md`: S1 and S2 said an
  out-of-range coordinate answers `400`, but the error catalogue reserves `400` for
  `invalid_json`/`invalid_code` and answers invalid input with `422 validation_failed` plus
  `details.fields` from a closed code vocabulary. The spec was corrected to the catalogue
  instead of the catalogue being widened for one endpoint. Result: Phase 1 introduces exactly
  one new error code, `review_already_exists`.

- L12 — The truncated requirement is resolved by the human (2026-11-02). Verbatim answer to
  the four readings offered: "si era la A" — "saber que mi [invitado aceptó]": the host wants
  to see who answered the invitation, not a feed, not a discovery badge.
  - Cost: one read endpoint on top of state the slice already stores. No migration.
  - Added to P3b as `GET /v1/plans/{coRoute}/invitations` for `EDITOR`/`OWNER` (S19, D16).
  - The privacy rule came with it, because the obvious implementation leaks: a guest must not
    learn who declined. A guest sees their own invitation plus accepted members; the host sees
    every status. Meeting someone at a plan is public; turning a plan down is not.
  - The three other readings are explicitly not adopted and stay out of scope: B is already
    P2 (review/check-in highlight), C (friends-going badge on discovery) is P4 work that
    stresses the cell cache and needs its own batched-query rule, D (activity feed) is a
    separate product with real fan-out cost.

- L11 — Q4 and Q5 settled by the human (2026-11-02):
  - Currency, verbatim: "si por ahora sera moneda USD". Phase 1 price filtering is USD-only
    with no `currency` parameter; an item priced in another currency does not match, and that
    is documented rather than patched (D19 revised, S22 rewritten).
  - Surprise model, verbatim: "la idea es que yo invito a alguien elijo si le muestra una pista
    o no, por ejemplo, y ese usuario recibe la invitación, ve mi mensaje custom, la fecha y
    hora y la pista si lo marqué, pero si no marqué que sea así modo sorpresa, sí le llega todo
    normal con el nombre del local. La pista es por ejemplo lleva atuendo cómodo y cálido,
    porque en una cena de pasta en un rooftop."
  - This overrides my earlier recommendation in D20, and the record says so instead of quietly
    rewriting it. `SURPRISE` now hides the venue entirely and the host chooses the reveal rule
    (`AT_SCHEDULED` default, `ON_ACCEPT`); `PLAIN` is the default mode and arrives with the
    place name (D20 revised, S26 rewritten).
  - Design constraints kept because they cost nothing and prevent real failures: `SURPRISE`
    requires a scheduled instant (otherwise there is no reveal moment), requires at least one
    of message or hint (otherwise it is an empty envelope), and keeps the coarse area visible
    (an invitation to an unknown city is not a surprise). The earlier objection about consent
    is withdrawn as a rule and retained as a stated cost.
  - Field shape simplified from a JSON `js_reveal` blob to `tx_message` (≤ 500) plus
    `tx_hint` (≤ 200), because the human described exactly one message and one hint.

- L10 — Search, filters, plan scheduling and surprise invitations (2026-11-02). The human
  asked for the discovery surface in full and closed with a viability question:
  - Verbatim: "falta agregar en la planificación que el usuario puede buscar cualquier lugar
    pir nombre, por precio (ejemplo lugares con platos por menos de 10$), nombre de un
    restaurante, o por comida y me dice los lugares que la tienen, por supuesta veo los
    comentarios del lugar y su calificación, filtrar por categoria, ejemplo rumba, playa noche,
    terrasa, petfriendly etc, la app tendra un mapa donde podre ver lo que esta cerca de mi y
    asi podre explorar y establer el lugar a visitar en el momento o planificarlo para algun dia
    o hora, si va solo o en pareja para hacer la invitación a el otro usuario con un toque de
    dinamismo si wuiere que sea sorpresa para el invitado que solo le de pistas o un mensaje
    que el anfitrión agrega, estaria genial todo esto, ahora es viable ??"
  - Answered: viable in full, with one modelling correction and two product decisions left to
    the human. Recorded as S21–S26, D18–D20, and Q4–Q6.
  - Correction taken rather than deferred: the four examples are not one list. `rumba` is a
    category, `terraza` and `petfriendly` are attributes, and `playa noche` is an attribute
    plus a schedule question — the schedule model already handles closing after midnight, so
    no new machinery was invented for the night case (D18, S23).
  - Correction taken rather than deferred: "menos de 10$" across currencies needs a rate, and
    in VES the choice of rate is a business decision. Phase 1 compares inside one named
    currency and says so; the FX path is written down instead of guessed (D19, S22, Q4).
    **[Superseded by L11: Phase 1 is USD-only, no `currency` parameter.]**
  - Correction taken rather than deferred: a surprise that hides the destination from the guest
    is an app asking for consent without disclosure. The surprise moves to the detail layer,
    the guest always sees plan, place and day, and the reveal timing stays open (D20, S26, Q5).
    **[Superseded by L11: the human chose the opposite model — `SURPRISE` hides the venue and
    the host picks the reveal rule. D20 and S26 were rewritten, not appended to.]**
  - Gap this requirement exposed: `GET /v1/places/{coPlace}` was never planned, and comments,
    rating, menu and attributes all hang from it (S24, T1.6–T1.7).
  - Verified rather than assumed: `pg_trgm` 1.6 and `unaccent` 1.1 are available in the
    current image, `postgis` is not (confirming the P0 image change is real); `tb_menu_item`
    already carries `nu_price decimal(10,2)` plus a mandatory `co_currency`; and
    `tb_place_schedule` already models overnight closing.

- L8 — `melia-landing` (`http://localhost:4321`) is a one-page marketing site: Hero, Radar,
  EventCard, Creators, Steps. It states the visual contract this change has to make true
  ("Radar urbano en vivo", "42 planes y mesas activas a < 2 km", "Explorar mapa"), and it
  names neighbourhoods ("Malasaña", "Salamanca", "La Latina") that an H3 zone cannot yet
  name (D14).
