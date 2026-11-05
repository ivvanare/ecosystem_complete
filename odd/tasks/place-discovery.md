# place-discovery — P1 commit 1: the discovery read path

Feature: `GET /v1/places` in list mode (radius) and map mode (viewport), with in-memory H3
clustering.
Branch: `feat/api-place-discovery` off `dev`. One work-unit commit. Do not push.
Authority: `openspec/changes/social-discovery/p1-discovery.md` §1–§4 is the operative
contract — read it, plus `spec.md` §S1, §S2, §S14, §S16, §S20 and `tasks.md` T1.1–T1.5. This
file only pins the slice's constraints. Where they disagree, `p1-discovery.md` wins.

Commit 1 was `GET`-only. This document now also carries commit 2 (reviews). Commit 3 (search
and filters) and commit 4 (trending producer) follow.

---

## Specs

**S1 — Contract first.** `openapi.yaml` (English) and `API.md` (Spanish, matching the file's
existing language) get the route before the handler exists. `internal/contract` must pass:
`TestEveryRouteIsInTheOpenAPIContract`, `TestEveryRouteIsInTheConsumerGuide`. Register with
`mux.HandleFunc("GET /v1/places", ...)` — the contract test scrapes that exact shape.

**S2 — Two modes, one locator.** `near`+`radius` → list mode; `bbox`+`zoom` → map mode. Both
or neither locator is `422 validation_failed` with `details.fields` using the **closed** field
vocabulary (`required | invalid | too_short | too_long | unknown`). No `400` for validation.

**S3 — Validation runs before the database.** Every rule in `p1-discovery.md` §3 is enforced
at the edge; a test proves the store was never called (pass a nil or panicking store).

**S4 — SQL shape.** The spatial predicate must be on `co_location` (the generated column
landed in P0), or the GiST index is not used. `ST_DWithin` for the radius, `&&` with
`ST_MakeEnvelope` for the viewport, `<->` for `sort=distance` KNN ordering, a 5000-row cap in
map mode, and an oversized bbox rejected as `422`/`bbox`/`too_long` rather than truncated.

**S5 — Clustering in Go.** H3 cells over the rows the query already returned; one resolution
per zoom as tabulated in §4; parents derived by `cellToParent`, never one column per zoom;
cell serialized as the hex string. A cluster carries counts and neutral aggregates and **no
administrative naming** (S20).

**S6 — Trending degrades from day one.** `sort=trending` is in the enum, but the ranking
source does not exist until commit 4. Define it as a small interface with a "no ranking"
result, implement the fallback to rating-then-name, and set `sortApplied` and `degraded` in
the response so a client can tell a cold ranking from an unpopular place. Do not aggregate
`tb_review` in a request.

**S7 — Verification.** `gofmt`/`go vet`/`go build ./...`/`go test ./...` clean. `go test
./...` must not need Postgres, Redis or RabbitMQ. Prove the SQL against the live dev database
**inside a transaction that rolls back** — insert a few places with known coordinates, run
each query shape, assert the ordering and the radius/bbox results, `ROLLBACK`. Leave no rows
behind. If a rule cannot be proven that way, say so instead of claiming coverage.

---

## Repo rules that bite

- A new route is a three-file change by design: `openapi.yaml`, `API.md`, handler + tests in
  the same commit. Every emitted error code must already exist in the API.md catalogue and be
  exercised by a test (`TestEveryErrorCodeIsCatalogue`, `TestEveryErrorCodeIsExercisedByATest`).
- New schema objects must declare `additionalProperties` (`TestObjectPropertiesDeclareWhetherTheyAreOpen`).
- Reuse `page`/`limit` parameters and the `Page` wrapper; do not invent pagination.
- `tx_name` (legal name) and `co_owner_user` must never be serialized. Display name is
  `tx_title`.
- Errors wrapped with `%w`; `log/slog` with an injected logger, never a package global.
- If you add a module dependency (H3), it is `github.com/uber/h3-go` and `go.mod`/`go.sum`
  change with it; justify it in the commit body.

## Tasks

- T1 — `openapi.yaml` + `API.md`: route, parameters, `mode` discriminator, `PlaceSummary`,
  `Cluster`, error responses.
- T2 — `internal/geo`: coordinate, radius, bbox and zoom validation; zoom→resolution table.
- T3 — `internal/places`: query building for both modes, KNN ordering, result cap, scanning.
- T4 — `internal/httpapi`: handler, response mapping, `sortApplied`/`degraded`.
- T5 — tests: the §8 items 1–14 that belong to this commit, RED first.
- T6 — verify per S7 and report the rollback-transaction evidence.

---

# Commit 2 — the reviews write path

The first domain write path this product has: until now the only writer is auth. It is also
the signal the trending producer consumes (D17), which is why it lands inside P1.

## Specs

**S8 — Contract first, five routes.** `openapi.yaml` (English) and `API.md` (Spanish) get
every route before the handlers: `GET`/`POST /v1/places/{coPlace}/reviews`,
`PATCH`/`DELETE /v1/places/{coPlace}/reviews/{coReview}`, and
`PATCH /v1/admin/reviews/{coReview}`. Register with the scraped `mux.HandleFunc("METHOD /path")`
shape. Reuse `page`/`limit` and the `Page` wrapper for the list.

**S9 — The only new error code is `review_already_exists` (409).** Everything else reuses
the catalogue: `validation_failed` (422, closed field vocabulary), `not_found` (404),
`permission_denied` (403), `unauthorized` (401), `internal_error` (500).
`TestEveryErrorCodeIsInTheCatalogue` and `TestEveryErrorCodeIsExercisedByATest` must pass.

**S10 — One active review per user and place.** The conflict is the partial unique index
`ux_tb_review_user_place_active`, surfaced by mapping SQLSTATE `23505` on that constraint to
409 — never a pre-check, which races, and never a 500.

**S11 — Author-only mutations.** `PATCH` and `DELETE` require the caller to be the review's
author (`403 permission_denied` otherwise). `DELETE` is logical (`dt_deleted_at`), so the
author may review that place again later — which is exactly why the index is partial. A
moderated or logically deleted review is not returned to other readers; its author and
holders of `review.moderate` still see it.

**S12 — Moderation reuses what exists.** `PATCH /v1/admin/reviews/{coReview}` sets `tx_status`
to `ACTIVE | INACTIVE | MODERATED` and requires the already-seeded `review.moderate`
capability. No new permission, no new enum value.

**S13 — The history actor.** Every write sets `app.current_user_id` with
`set_config('app.current_user_id', <user>, true)` at the start of the transaction, so the
existing trigger records `co_actor_user`. The service never inserts into
`core.tb_review_history` itself: `tb_review` is in the point-in-time set and the trigger owns
that row.

**S14 — Reading is bounded and never aggregates for ordering.** The review list is paginated,
returns the author's display-safe fields, and never computes a place average in the request
(the aggregate belongs to the worker, commit 4). A place that is soft-deleted or `INACTIVE` is
`404` for every review route.

## Repo rules that bite here

- Auth and permission checks follow the existing handlers: `TokenParser` for identity and the
  existing `Authorizer` for capability. Do not invent a second authentication path.
- The JSON body is decoded with the existing strict decoding used by the other endpoints, and
  a malformed body is `400 invalid_json`.
- `rating` is an `int` bounded by `1..5` (table CHECK); `comment` is bounded to 2000
  characters at the edge, not in the database.

## Tasks

- T7 — `openapi.yaml` + `API.md`: five routes, the request/response schemas (with
  `additionalProperties` declared), the new error code in the catalogue, and the flow in
  Spanish.
- T8 — `internal/reviews`: store (pgx) with the five operations, the actor `set_config`, and
  the `23505` mapping.
- T9 — `internal/httpapi`: handlers, author checks, pagination, moderation capability.
- T10 — wiring in `cmd/api/main.go` next to the other services; the routes must appear only
  when the service is wired, like the broker-gated routes.
- T11 — tests: RED first, no service needed. Cover the conflict mapping, author-only
  mutations, the logical delete allowing a re-review, the visibility filter for other
  readers, the moderation capability, and that every write sets the actor.
- T12 — verify: `go test ./...` and `internal/contract` green, plus the SQL proven against the
  dev database inside a transaction that rolls back.

---

---

# Commit 3 — search, filters and their migration

The discovery surface gets the three questions a person actually asks: "what is this place
called", "who serves this dish", and "which of these is a terrace that is open now and cheap".
Two ledger files carry it: `18` for the attribute vocabulary and `19` for the text search.

## Specs

**S15 — Attributes are a governed many-to-many, categories stay one-per-place.**
`core.tb_place_attribute` (`co_attribute`, `tx_code` unique among live rows, `tx_name`,
`tx_description`, `tx_status`, timestamps) plus `core.tb_place_attribute_link`
(`co_place`, `co_attribute`) with a **composite primary key** and no soft delete — the same
reason `tb_route_place` has none. Seed the vocabulary in the migration: `terraza`,
`petfriendly`, `frente_playa`, `aire_libre`, `musica_en_vivo`. Categories that the product
names (`rumba`, and at most `restaurante`/`bar`) are seeded into `core.tb_place_category` by
the **same new ledger file** — `06-seeds.sql` is already recorded and an applied migration is
never edited.

**S16 — Text search needs an immutable unaccent.** `unaccent()` is `STABLE`, so it cannot
appear inside a `GENERATED ALWAYS AS … STORED` column: PostgreSQL rejects the expression.
Create an `IMMUTABLE` SQL wrapper (`SELECT public.unaccent('public.unaccent', $1)`) and build
the generated `tsvector` columns from it, so the stored vector and the query vector are
produced by the same expression. Columns: `tb_place` on `tx_title`, `tb_menu_item` on
`tx_name || ' ' || coalesce(tx_description,'')`. GIN index on each. Configuration `simple`,
not `spanish`: the analyzer must not stem place and dish names into each other, and accent
folding already happens through the wrapper.

**S17 — `q` and `dish` are different questions.** `q` searches the place's commercial name.
`dish` searches menu items and returns the **places** that carry a match, via an `EXISTS` over
`tb_menu` → `tb_menu_item` that respects both `dt_deleted_at IS NULL` and
`tx_status = 'ACTIVE'` on each. They are separate parameters with separate indexes; a single
box would make one of the two questions unanswerable.

**S18 — Attribute filters are conjunctive.** `attributes=terraza,petfriendly` means *both*.
Implement it by counting the intersection against the requested cardinality — a plain
`JOIN … IN` silently degrades to "any of them", which is a different product. An attribute
code that is not in the catalogue is `422` with field `attributes`, code `unknown`.

**S19 — `openAt` is a schedule question, not an attribute.** Answered from
`core.tb_place_schedule` in the **place's own** `tx_timezone`, with both branches: a normal day
and the overnight case (`dt_close_time <= dt_open_time`, where the interval belongs to the day
it opened). Without the second branch a place open 20:00–02:00 reports closed at 23:00.

**S20 — `maxPrice` is USD.** No `currency` parameter (D19). The filter matches a place with at
least one active item under the amount, on an active menu, whose currency row is `USD`, via an
indexed `EXISTS`. An item priced in another currency does not match, and that is documented
behaviour.

**S21 — Contract and vocabulary, no new error codes.** Every new parameter goes into
`openapi.yaml` (English) and `API.md` (Spanish) before the code, with the closed field
vocabulary: `q`/`dish` shorter than 2 or longer than 80 characters, `openAt` not an ISO-8601
instant, `maxPrice` negative or non-numeric, `attributes` unknown. Filters compose with the
spatial predicates and with each other; `q`/`dish`/`attributes`/`maxPrice`/`openAt` are valid
in list mode and in map mode.

**S22 — The drift check keeps telling the truth.** `database/diagram_base.txt` gains both
tables and the generated columns; `database/verify_base_model.sql` gains checks for the
attribute pair, the composite key, the immutable wrapper existing, both `tsvector` columns
being `attgenerated = 's'`, the GIN indexes, and a real match through the wrapper. The check
must still pass on a scratch database and on the live one.

**S23 — The seed demonstrates the filters.** Extend `database/seed/` so the demo places carry
attributes (`terraza`, `petfriendly`, `frente_playa` spread across them) and at least two menu
items with USD prices, one under 10 and one over. Idempotent, still outside `database/init`.

## Repo rules that bite here

- Extensions ship in their own ledger file (`19`), with `CREATE EXTENSION IF NOT EXISTS`.
- The immutable wrapper must live in a schema the search path reaches and must be documented
  in the file header, because marking a function immutable when it is not is a correctness
  decision, not a syntax one.
- `go test ./...` must stay green without PostgreSQL: the SQL shape is asserted against the
  recording fake, the vocabulary and validation against pure functions.

## Tasks

- T13 — Ledger `18-place-attributes.sql`: attribute catalogue + link table + seeds + the new
  categories.
- T14 — Ledger `19-place-search.sql`: `pg_trgm`, `unaccent`, the immutable wrapper, the two
  generated `tsvector` columns and their GIN indexes.
- T15 — `internal/places`: the new predicates in the list and map queries (`q`, `dish`,
  attributes, `maxPrice`, `openAt`), composing with the existing ones.
- T16 — `internal/httpapi`: parse and validate the new parameters, map them down, keep
  `page`/`limit` behaviour unchanged.
- T17 — `openapi.yaml` + `API.md`: the new parameters, their bounds, the field vocabulary
  entries, and the flow in Spanish.
- T18 — Seed update with attributes and priced menu items (idempotent).
- T19 — Verify: suite and `internal/contract` green, drift check green on a scratch database,
  and the SQL proven on the live database inside a transaction that rolls back.

---

## Log

- L1 — Human authorization, verbatim: "no pushemos todavia, hasta que se pruebe, la imagen la
  puedes reconstruir esta bien aun estamos en desarrollo del mvp ... continua con P1".
- L2 — P0 was verified on the live dev database before this slice started: ledger 16 rows,
  postgis 3.5.7, `co_location` stored-generated, both indexes, `tx_region` mirrored, drift
  check `ALL CHECKS PASSED`.
- L3 — Commit 1 shipped as 233f8fb (plus 5c0e3a7 seed and 05f08d1 plan docs), rebased onto
  feat/api-geo-foundation because P1 depends on P0. Two defects were found past a green
  suite: `cmd/api/main.go` never wired the discovery service (the real route answered 404),
  and the branch lacked P0, so compose recreated the database on plain postgres over a
  PostGIS volume. Both fixed.
- L4 — Human authorization for this slice, verbatim: "si dale continuemos con el plan".
- L5 — Commit 2 verified end to end against the running API with real signed-up users: 201,
  409 review_already_exists, 403 permission_denied, 204 delete, 201 re-review after the
  logical delete (the partial index proven on the real table), and 422 for rating 9. History
  rows for the three operations carry co_actor_user. Two contradictions were corrected: §6 of
  p1-discovery said DELETE was author-or-moderator while S17 says author-only, and the diagram
  claimed the partial index frees moderated reviews when the predicate is only
  dt_deleted_at IS NULL (recorded as Q7).
- L6 — Human instruction for this slice: continue, and tell the melia-admin session the
  endpoints exist. Done: a message went to that session with the routes, the coordinate-order
  trap (near is lat,long; bbox is longitude-first), the MapLibre decision with the warning not
  to use its own clustering, and the note that sort=trending still degrades.
