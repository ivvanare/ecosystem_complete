# Proposal: Social Discovery — Nearby Places, Plans, and People

## Intent

melZone stops being "a places catalog with auth" and becomes a social discovery
product. The user's vision, verbatim:

> "vamos a necesitar saber como segmentar mis usuarios para poder recomendarles lugares
> de su zona lo que esta cer de ellos a menos de 2km por ejemplo los que estan trendy"

> "quiero lograr algo como la info que se ve en esta web https://llegateapp.com, pero
> general por pais y por estado poara poderlo escalar a nivel global como si fuera un
> https://www.airbnb.co.ve pero de lugares que visitar planificar con amigos, cosas asi,
> algo muy social"

> "la planificacion deberia de ser elegible para privado y publico asi puediera armar un
> festival publico y la comunidad se une o creo uno privado para mis panas, tambien seria
> genial poder tener seguidores y crear rutas que las personas voten como spotify y
> amigos como instagram para saber oye mi amigo estuvo aqui me resalta sobre las demas
> comentarios"

Five product capabilities follow from that: **proximity discovery** (<2 km), **trending
per zone**, **country → region navigation**, **plans that are public or private**, and a
**social graph** (follow, vote, check in, "my friend was here").

The model already carries most of the raw material — `tb_place` has mandatory
`nu_latitude`/`nu_longitude`, `tb_review` carries rating, `tb_route`/`tb_route_place`
already model an ordered itinerary, `tb_place.tx_timezone` makes "open now" answerable per
row, and the worker/Redis/RabbitMQ/Soketi services already exist. This change adds the
missing relations, the missing attributes, and the rules — including the privacy rule
that governs what location data is allowed to leave a request.

## Scope

### In Scope
- **Decisions record** (this change's `design.md`): the architectural choices that were
  open in discussion, settled with their rationale, so later slices do not relitigate
  them.
- **Phased delivery plan** (`tasks.md`): six implementation slices, each small enough to
  review, in dependency order.
- Phase 0 and Phase 1 design in executable detail: spatial column and index strategy, proximity
  and viewport queries, zone clustering, the reviews write path, and the trending producer.
- Phases 2–4 designed at the level a spec needs (tables, enums, rules, read-path
  strategies), implementation deferred to their own change when they start.

### Out of Scope
- Any DDL in this change. `database/diagram_base.txt` and `database/init/NN-*.sql` are
  touched by Phase 0 and Phase 1, not here.
- Any Go code. Endpoints arrive contract-first (`openapi.yaml` + `API.md` + contract test)
  in Phase 1.
- The admin panel (`melia-admin`) — it consumes the API, it does not own this domain.
- Recommendation model quality (ML ranking, embeddings). Phase 4 ships deterministic
  ranking from explicit signals; a learned model is a later, separate decision.
- Administrative polygons and `ST_Contains`. No data source and no consumer yet — see D14.
- `h3-pg` and a stored H3 column. The cell is computed in Go — see D13.
- Payments, reservations, and provider dashboards for places.

## Capabilities

### New Capabilities
- `place-discovery`: proximity search by radius and by map viewport, free-text search by
  place name and by dish, price and attribute filters, country/region navigation, in-memory
  zone clustering, trending-per-zone read path, and the place detail read.
- `place-reviews`: create, edit, delete and moderate a review. It is also the only trending
  signal the service can record today, which is why it ships with discovery instead of
  after it.
- `social-graph`: asymmetric follow with derived friendship, check-ins, and the
  viewer-relative activity highlight.
- `plan-collaboration`: plan visibility (public/private), membership with roles, and
  collaborative ordering by vote.
- `taste-profile`: derived per-user affinity computed by the worker from explicit signals,
  feeding recommendations and notifications.

### Modified Capabilities
- `base-domain-model`: `core.tb_place` gains `tx_region` (ISO 3166-2) and its history
  payload mirrors it; `core.tb_route` gains `tx_visibility`; new `core` tables and new
  `public` enums.

## Approach

Six phases in dependency order. Each is delivered in reviewable slices of roughly 400
authored lines — Phase 1 needs four, because search, detail, reviews and trending are
separate reviewable units:

| Phase | Delivers | Unblocks |
|---|---|---|
| **P0 — Geo & scheduler** | PostGIS image and extension, derived `co_location` + GiST, `tx_region`, a scheduler in the worker | Three later features that silently needed a scheduler nobody built |
| **P1 — Discovery** | `GET /places` by radius and viewport, in-memory zone clustering, the reviews write API, and the trending producer that consumes it | The visible product: "places near me, right now" |
| **P2 — Social graph** | `tb_follow`, check-in, "my friend was here" in the review read path | Trust signal over anonymous reviews |
| **P3a — Plans** | `tb_route` visibility + `tb_route_member` + `tb_route_place_vote`, public join | Festivals, group trips, Spotify-style votes |
| **P3b — Invitations** | `tb_plan_invitation`, RSVP, expiry | "Invite the group, they say yes or no, the invitation expires" |
| **P4 — Personalization** | `tb_user_taste_profile` batch in the worker, notifications | Retention; uses only what P1–P3 recorded |

P0 and P1 are the only slices this change specifies to implementation detail, because they
are the next unit of work and they are self-contained. P2–P4 are specified as rules and
schemas, so their tasks can be cut without redesign.

## Why this is not one change

Six slices in one change would be one enormous, unreviewable delivery that cannot be tested
in isolation and cannot be rolled back per capability. Each slice is independently shippable
and independently verifiable, and each depends on the previous one's tables (P3a votes need
P2's graph and P1's places; P3b's invitation grants a membership P3a defines; P4 reads all
three). Delivering them in order is the only sequence that keeps every slice observable.

## Decisions already taken

Recorded in `design.md` as D1–D20, summarized:

1. **Discovery is query-driven.** The user's location is a request parameter, never a
   stored trace. What is stored are explicit signals and a derived, regenerable profile.
2. **Both geo layers, with distinct roles (revised).** PostGIS is the source of truth and
   answers exact questions — radius with `ST_DWithin`, viewport with `ST_MakeEnvelope`. H3
   is a derived bucket for clustering, zone keys and cache keys. The decimal columns stay
   the contract, so the spatial column can be dropped and the deployment returned to a
   bounding box; `h3-pg` is not installed and the cell is computed in Go.
3. **Trending is computed, never counted on request** — a worker batch writes the ranking.
   It also has a producer and a real signal from day one, which is why the reviews write
   path is part of the same phase.
4. **Country stays a plain ISO code.** `tx_region` (ISO 3166-2) is added as a column, not
   a catalog. Catalogs remain reserved for governed vocabulary with metadata.
5. **The social graph lives in `core`, not `seg`.** Follow is asymmetric; friendship is
   derived as a mutual follow at read time, never denormalized as a flag.
6. **Plan visibility is an attribute of the plan**, not two entity types. Public plans are
   community surface and therefore moderator-governed.
7. **Category is one, attributes are many.** "rumba" is a category; "terraza",
   "petfriendly" and "frente_playa" are attributes; "de noche" is a schedule question the
   existing schedule model already answers.
8. **Prices are USD in Phase 1.** No `currency` parameter and no rate; an item priced
   elsewhere does not match, and adding multi-currency later is additive.
9. **A surprise hides the venue, and the host chooses the reveal.** `SURPRISE` delivers the
   host's message, the date and time, the hint and the coarse area; `PLAIN` is the default and
   arrives with the place name. The reveal is `AT_SCHEDULED` or `ON_ACCEPT`.
10. **Zones are not neighbourhood names.** No administrative polygons means a hexagon cannot
   call itself a barrio; the API reports zones until real data exists.
11. **A cached read path is viewer-neutral.** The friend highlight is applied after the cache,
   never inside a cell-keyed entry.
12. **An invitation is a lifecycle, not a flag.** Accepting creates the least-privileged
   membership; expiry is enforced by the read predicate, not by the scheduled sweeper.
13. **Registered users only.** No anonymous shareable invitation token in this phase.
