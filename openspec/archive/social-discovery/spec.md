# Spec: Social Discovery — Nearby Places, Plans, and People

Canonical sources: `database/diagram_base.txt` (schema), this change's `design.md`
(decisions D1–D20), and the user statements quoted verbatim below. Where this spec and
the diagram disagree, the diagram wins for what exists today and this change's Phase 1
task updates it.

---

## S1 — Proximity search uses the request's location, never a stored trace

A caller asks for places near a coordinate; the coordinate is evaluated and discarded.

> "vamos a necesitar saber como segmentar mis usuarios para poder recomendarles lugares de
> su zona lo que esta cer de ellos a menos de 2km por ejemplo los que estan trendy"

Rules:
- The proximity filter accepts a latitude, a longitude, and a radius in metres, and
  defaults the radius to 2000 m when the caller omits it.
- The coordinate is bound as a query parameter, never persisted, never logged, never
  written to `seg.tb_security_event`, and never cached under a key derived from it.
- `seg.tb_user` gains **no** location column. Segmentation by geography is derived from
  the places a user interacted with (reviews, votes, check-ins, plan membership), which
  already carry `tx_country`/`tx_region`.
- A coordinate that is not a finite number within valid latitude/longitude bounds is
  rejected at the application edge, before any database work, using the existing validation
  vocabulary: `422 validation_failed` with `details.fields` naming the parameter and one of
  `required | invalid | too_long`. `400` stays reserved for `invalid_json` and `invalid_code`
  as API.md §10 defines; an endpoint does not widen the catalogue for its own convenience.

## S2 — Every accepted coordinate is a valid place location

`tb_place.tx_timezone`'s precedent applies to coordinates: the CHECK asserts shape, the
application edge asserts validity.

Rules:
- The database keeps the shape guard (`nu_latitude BETWEEN -90 AND 90`,
  `nu_longitude BETWEEN -180 AND 180`); it cannot know whether a point is on land.
- The application validates the request coordinate the same way it validates
  `tx_timezone`: a bounded check before the query. An out-of-range coordinate is a
  `422 validation_failed` naming the parameter, not an empty result, and never a query.
- An empty result is a legitimate answer: no places within the radius is not an error.

## S3 — Country and region are codes, not catalogs

Country stays as it is; region is added alongside it.

Rules:
- `core.tb_place.tx_country` remains `char(2) NOT NULL` with
  `CHECK (tx_country ~ '^[A-Z]{2}$')`. It is not converted to a UUID FK: ISO 3166-1 *is*
  the catalog, and governing it would duplicate it.
- `core.tb_place` gains `tx_region varchar(6) NULL`, holding an ISO 3166-2 subdivision
  code (`VE-A`, `US-CA`) — country code, hyphen, subdivision. NULL is allowed because
  many places legitimately have no subdivision data, and because requiring it would make
  a place uninsertable where the geocoder cannot answer.
- A non-NULL `tx_region` must pass `CHECK (tx_region ~ '^[A-Z]{2}-[A-Z0-9]{1,3}$')`. It is
  assigned at creation by reverse geocoding, never typed as free text.
- `core.tb_place_history` mirrors both columns, with no FK, for the reason
  `07-history-tables.sql` gives: history must survive catalog and data changes.
- Navigation reads use `(tx_country, tx_region)` — indexed together, in that order,
  because "list regions of a country" and "list places of a region" are the two queries.

## S4 — Trending is a computed ranking, never a query-time aggregate

> "los que estan trendy"

Rules:
- A worker batch, on a schedule, computes a trending score per place and publishes the
  ranking per zone to Redis. The score weighs review recency and rating volume, not raw
  lifetime count: a place with one thousand reviews from 2024 is not trending.
- The ranking has a real producer from the day it is read, and a real signal to consume:
  the reviews write path of S17 ships in the same phase. A ranking nothing writes,
  computed from a table nothing fills, is not a feature.
- The read path never runs `COUNT(*)` or an aggregate over `tb_review` for ordering. A
  missing ranking for a zone degrades to a stable non-trending order (rating, then name);
  it is not an error and it does not block discovery.
- The zone key is an H3 cell computed by the worker (S16), not a column on `tb_place` and
  not a coordinate. It is a coarse bucket, so the cache is shareable. When the ranking has
  no bucket for the caller's zone, the nearest enclosing parent cell is used before falling
  back.
- The cached payload is viewer-neutral (S18).

## S5 — Follow is asymmetric; friendship is derived, never stored

> "seria genial poder tener seguidores"

Rules:
- `core.tb_follow (co_follower, co_followed, dt_created_at)` with a composite primary key.
  No soft delete: with a composite key a logically deleted row would prevent ever
  following that person again — the same reason `tb_route_place` has none.
- Self-follow is rejected by
  `CHECK (co_follower <> co_followed)`, not only by the service.
- "Friend" means mutual follow and is computed at read time
  (`EXISTS(follow(a→b)) AND EXISTS(follow(b→a))`). No `is_friend` flag is stored on any
  row, because the graph changes daily and a stored flag rots.
- Blocking is out of scope for P2 and named as a decision to take before public launch.

## S6 — A friend's activity is resolved per viewer

> "amigos como instagram para saber oye mi amigo estuvo aqui me resalta sobre las demas
> comentarios"

Rules:
- The highlight is a property of the **read**, not of the row: an existing review is not
  modified, re-weighted in storage, or duplicated when a viewer happens to follow its
  author.
- The first implementation resolves it in the read path by joining the viewer's follow set.
  It is bounded (limit plus pagination), so it never scans the whole review table.
- Fan-out-on-write (precomputed feed in Redis) is a scale response to measured latency,
  not a starting design. It must not change the response contract when it lands.

## S7 — Plan visibility is an attribute, and membership is a relation

> "la planificacion deberia de ser elegible para privado y publico asi puediera armar un
> festival publico y la comunidad se une o creo uno privado para mis panas"

Rules:
- `core.tb_route` gains `tx_visibility` using a new `public.plan_visibility` enum with the
  values `PRIVATE` and `PUBLIC`, `NOT NULL`, defaulting to `PRIVATE`. Defaulting to the
  restricted value is deliberate: a plan must never become public by omission.
- `core.tb_route_member (co_route, co_user, tx_role, dt_created_at, dt_updated_at)` with a
  composite primary key, no soft delete, `tx_role` drawn from a new `public.plan_role`
  enum (`OWNER`, `EDITOR`, `VIEWER`).
- The creator is recorded as `OWNER` in the same transaction that creates the plan; a plan
  with no owner is invalid. `tb_route.co_user` remains as the creator reference and its
  history is unaffected.
- Entry differs by visibility, not by table: `PRIVATE` requires an invitation
  (`internal/invitation`, the existing pattern) or an existing membership;
  `PUBLIC` accepts an authenticated self-join.
- Every mutation of the plan, its order, and its membership requires the caller to be a
  member with `EDITOR` or `OWNER`.
- Changing `PRIVATE` → `PUBLIC` is a deliberate action that touches everyone already
  invited; it is allowed for `OWNER` only.

## S8 — Public plans are community surface and are moderated

An open join is an open door: a public plan can be renamed, sprayed, and used for
harassment.

Rules:
- `core.tb_route.tx_status` stays `activatable_status` (`ACTIVE`/`INACTIVE`); moderation of
  a public plan uses it and takes effect immediately for non-owners.
- A non-member cannot mutate anything of a suspended plan, read or write, including its
  order. Members keep read access so they do not lose their itinerary.
- A newly joined public plan does not grant `EDITOR`. Self-join grants `VIEWER`; promoting
  to `EDITOR` is an owner action. This is the single most important abuse guard in P3.

## S9 — Collaborative ordering: manual, or by vote

> "crear rutas que las personas voten como spotify"

Rules:
- `core.tb_route_place.nu_order` keeps manual ordering and is authoritative for plans that
  have no votes.
- `core.tb_route_place_vote (co_route, co_place, co_user, nu_vote)` with a composite
  primary key, `nu_vote` in `-1, 1` (down/up), `dt_created_at` and `dt_updated_at`. A
  member has at most one vote per place per plan, enforced by the primary key.
- Only members of the plan may vote; a vote from a non-member is rejected, not silently
  dropped.
- Ranking is `net votes` descending, ties broken by `nu_order` then by `dt_created_at`
  ascending, so the result is deterministic.
- Reordering a voted plan by hand is `OWNER`-only and is recorded as an explicit override
  rather than fighting the votes silently.
- Vote changes propagate live over Soketi; a client that misses the event re-reads.

## S10 — Check-ins are explicit and are the source of "was here"

Rules:
- `core.tb_checkin (co_checkin, co_user, co_place, js_metadata, dt_created_at)` — append
  only, no update path, no soft delete; a check-in is a historical fact.
- A user may check in once per place per day; the constraint is expressed in the schema
  (a unique index over user, place, and the day bucket), not only in the service.
- A check-in is what S6 highlights. A proximity ping is not, and is never recorded to
  produce one.
- Check-in validation depth (free claim vs. proximity proof) is an open decision recorded
  in `design.md`; until it is taken, check-ins are a self-assertion and must not be
  presented in the UI as verified presence.

## S11 — Location data: the precise lives in the request, the taste lives in signals

This is the privacy rule of the whole feature and it is not negotiable per endpoint.

Rules:
- Stored location-bearing data is limited to: the place coordinates (public business
  data), and, opt-in, a single coarse zone per user for notifications, with a TTL and
  explicit consent. No history of positions, no traces, no background pings.
- `seg.tb_security_event.tx_country`/`tx_city` keep their existing meaning — IP-derived
  security context, documented as approximate — and are never reused as a taste signal.
- Taste is learned from explicit actions only: check-ins, reviews, votes, saved places,
  plan membership. Each is a deliberate user act.
- The derived profile is regenerable from source signals, so it can be deleted on request
  without losing anything the product cannot recompute.

## S12 — The taste profile is derived, versioned, and deletable

Rules:
- `core.tb_user_taste_profile (co_user PK, js_profile, nu_version, dt_computed_at)` — one
  row per user, replaced wholesale by the worker; not append-only, because it is a cache
  with a policy, not history.
- Contents are aggregate, never raw: category affinities, coarse zone affinities, price
  band, activity time bands. No coordinate, no per-event log.
- Deleting the row is a complete and safe operation; the next batch rebuilds it.
- Recommendations consume the profile when it exists and fall back to non-personalized
  trending when it does not. Absence of a profile is never an error.

## S13 — Notifications are derived, and their source is auditable

> "recomendarle a ese usuario en el futuro o notificaciones"

Rules:
- Notifications are produced by the worker from triggers the user can enumerate: trending
  near a saved zone, a followed user's new public plan, a plan you joined changing.
- Deviation from the delivery design in `design.md` D11 must not introduce a new source of
  location data; if a notification needs location, it reads the coarse opt-in zone.
- Every notification type must be individually disableable by the user.

## S14 — Every new endpoint is contract-first

Rules:
- Each phase adds its routes to `api/openapi.yaml`, documents the flow in `api/API.md`, and
  satisfies `internal/contract` — a route, error code, or environment variable that exists
  in code but not in the contract files fails the suite.
- Endpoint shape follows the existing conventions: `{"error":{"code","message"}}` for
  failures, clients parse `code` and never `message`.
- Discovery endpoints are read-only and require no elevated permission; plan mutation
  requires membership per S7; moderation requires `admin.access`.

## S15 — History coverage follows the existing rule, not convenience

Rules:
- `tb_route` and `tb_place` are already in the point-in-time set; a new column on them
  (`tx_region`, `tx_visibility`) is added to both the entity and its history payload, and
  the triggering function is replaced, not recreated — a trigger binds by name.
- New append-only facts (check-in) and new caches (taste profile) are **not** added to the
  history set: history answers "what was the state at instant T" for entities that change
  in place, and neither of these does.
- `tb_route_member` and `tb_route_place_vote` are membership relations with composite keys
  and no soft delete. Adding them to the history set is a separate, later decision — it is
  not assumed here.

## S16 — Geometry has one source of truth and one derived index

Rules:
- `nu_latitude`/`nu_longitude` remain the contract: `NOT NULL decimal(9,6)` with their shape
  CHECK. Every other spatial representation is derived from them and can be dropped and
  rebuilt without data loss.
- `co_location geography(Point, 4326)` is derived, never authored: a generated stored column
  where the expression qualifies as immutable, otherwise a trigger. A test asserts it agrees
  with the decimals.
- A GiST index serves the viewport (`ST_MakeEnvelope`) and the radius (`ST_DWithin`).
  Distances use geography; polygon containment, when it arrives, uses geometry with
  `ST_Contains`. Mixing the two costs either correctness or the index.
- The extension ships as its own ledger file; an applied migration is never edited.
- H3 cells are computed in Go where they are used — the read path for grouping, the worker
  for zone keys — and are not stored on `tb_place` in Phase 1. Zoom levels are derived from
  one resolution by parent-cell arithmetic, never by one column per zoom.

## S17 — Reviews are a domain write path, contract-first

Rules:
- Creating a review is a contract-first change: `openapi.yaml`, `API.md` and
  `internal/contract` in the same commit as the handler (S14).
- One active review per user and place, enforced by `ux_tb_review_user_place_active` and
  surfaced as a defined conflict error code, never as a server error.
- Only the author may edit or delete their own review; deletion is logical, so the user can
  review that place again later, which is why the index is partial.
- Moderation reuses what exists: the `review.moderate` capability and the `review_status`
  enum. No new permission and no new enum value is invented for it.
- A moderated or deleted review is not returned to other users' reads; its author and
  moderators may still see it.
- The handler sets `app.current_user_id` for the transaction so `co_actor_user` is recorded
  by the trigger. The service never writes a history row itself (`tb_review` is in the
  point-in-time set).
- Reading reviews is paginated and bounded, and never aggregates for ordering.

## S18 — A cached read path is viewer-neutral, or it is not cached

Rules:
- A cache entry keyed by a geographic cell or any other non-user key contains no data that
  depends on who is asking: no friend highlight, no personal order, no "your friends are
  going".
- The viewer-relative layer is applied after the cached payload is read, per request.
- A payload that is inherently viewer-relative carries the viewer, or the viewer's
  follow-set version, in its key — or it is not cached at all.
- This is checked on every new cached read path, because the failure is silent: a leaked
  highlight looks exactly like a working feed.

## S19 — Invitations are a lifecycle, and expiry is enforced by the read

> "invitaciones a grupos de amigos que decidan si van o no, que a su vez expiren en un
> tiempo"

Rules:
- `core.tb_plan_invitation` carries `tx_status` from a new `public.plan_invitation_status`
  enum (`PENDING`, `ACCEPTED`, `DECLINED`, `EXPIRED`, `REVOKED`), `dt_expires_at`,
  `dt_created_at`, `dt_responded_at`, the invited user, and the inviter.
- At most one `PENDING` invitation exists per plan and user, enforced by a partial unique
  index. Past invitations remain as history.
- Accepting inserts the `tb_route_member` row with role `VIEWER` in the same transaction that
  flips the status. Accepting never grants `EDITOR` (S8).
- Declining is terminal and creates no membership. Re-inviting is a new row, not a reopened
  one, so the audit trail stays readable.
- Expiry is enforced by the read predicate: every read and every accept requires
  `dt_expires_at > now()`. A scheduled sweeper only flips stale rows to `EXPIRED` for
  hygiene; if it stops, expired invitations are still correctly unusable.
- Invitations address registered users only. No anonymous shareable token in this phase.
- Only a member with `EDITOR` or `OWNER` of the plan may invite; a non-member invitation is
  rejected, not silently dropped.
- **The host reads every answer.** A member with `EDITOR` or `OWNER` reads the full RSVP list
  — invitee, `tx_status`, `dt_expires_at`, `dt_responded_at` — for the plan they manage.
- **An invitee reads only their own.** No endpoint discloses another invitee's status to a
  guest: a guest sees their own invitation and the accepted-member list, never who declined.
  Meeting someone at a plan is public; turning a plan down is not.

## S20 — The map groups by zone; zones are not administrative names

> "42 planes y mesas activas a < 2 km" is a statement the data can support today.
> "Malasaña" is not.

Rules:
- The viewport query is bounded by the visible frame and a result cap; clustering is applied
  to what the query returned, in memory, by hexagonal bucket.
- A grouped result reports counts and neutral aggregates. It never presents a hexagon as a
  neighbourhood, a district, or any administrative area.
- Phase 1 has no administrative polygons, so nothing claims `ST_Contains` semantics. When
  polygons are loaded, the same read contract gains names rather than changing shape.
- `tx_region` on `tb_place` continues to come from reverse geocoding and remains nullable
  (S3); it is not derived from cells.

## S21 — Search by name and by dish are two different questions

> "buscar cualquier lugar por nombre ... o por comida y me dice los lugares que la tienen"

Rules:
- `q` searches the place's commercial name (`tx_title`); `dish` searches menu item names and
  descriptions and returns the **places** that carry a matching item. They are separate
  parameters because they have separate indexes and separate result shapes — collapsing them
  into one box makes "arepa" return a place named Arepa's and miss the restaurant that
  serves them.
- Matching is accent- and case-insensitive (`unaccent` + a text search configuration), so
  "terraza", "Térraza" and "TERRAZA" are the same query. Both extensions are confirmed
  available in the current image.
- A typo-tolerant path via trigram similarity is allowed as a ranking signal; it never widens
  the result set beyond the same bounded page.
- A search that returns nothing is `200` with an empty page, never `404`.
- Search never aggregates `tb_review` and never bypasses the spatial or attribute filters: it
  composes with them.

## S22 — Price is dollars in Phase 1

> "lugares con platos por menos de 10$"

Rules:
- `maxPrice` is an amount **in USD**. The filter matches a place when it has at least one
  active menu item under that amount, on an active menu, whose `co_currency` is the USD row of
  `tb_currency`. There is no `currency` parameter (D19).
- An item priced in any other currency does not match a price filter, and this is documented
  behaviour, not a bug. It costs nothing while the catalogue is entered in USD.
- A place with no matching item does not match.
- `tb_currency` remains the only authority for decimals; the API never formats money itself.
- Multi-currency, when it lands, is an optional `currency` parameter defaulting to USD plus a
  rate with a stated source and effective interval — never an embedded constant.

## S23 — Attributes are many per place; categories are one

Rules:
- `core.tb_place_category` stays one-per-place and governed (D18). "Is this a rumba or a
  restaurant" is a category question.
- `core.tb_place_attribute` + `core.tb_place_attribute_link` model everything that is
  *present on* a place: `terraza`, `petfriendly`, `frente_playa`, …
- A multi-attribute filter is conjunctive: every named attribute must be present.
- Time-of-day requirements ("de noche") are answered by `openAt` against
  `core.tb_place_schedule` in the place's own `tx_timezone`, honouring schedules that close
  after midnight. They are not attributes.
- Both catalogs are seeded as governed vocabulary in a ledger file; an attribute that does
  not exist in the catalog is rejected, not created on the fly.

## S24 — A place has a detail read

Rules:
- `GET /v1/places/{coPlace}` returns the place, its category, its attributes, its rating
  aggregate and its open/closed state at a given instant. It is the resource the review list
  and the plan picker hang from.
- A place that is soft-deleted or `INACTIVE` is `404`, not an empty detail.
- The detail never exposes `tx_name` (legal name) or `co_owner_user`.

## S25 — A plan has a time and a party size

> "planificarlo para algun dia o hora, si va solo o en pareja"

Rules:
- `core.tb_route` gains `dt_scheduled_at timestamp NULL` and `nu_party_size smallint NULL`
  with `CHECK (nu_party_size BETWEEN 1 AND 50)`.
- Both are nullable: a plan can exist without a date, and "no definido todavía" is a real
  state, not a default of "today".
- Party size is numeric rather than an enum, because "solo" and "en pareja" are 1 and 2 and a
  festival is 50; an enum would need a migration the first time a group is four.
- Both columns are mirrored into the `tb_route` history payload, and the triggering function
  is replaced rather than recreated, because a trigger binds by name (S15).
- An invitation's `dt_expires_at` must not outlive `dt_scheduled_at` when both exist: an
  invitation to a past plan is noise, and the accept predicate already rejects it.
- A `SURPRISE` invitation is only valid on a plan that has `dt_scheduled_at`, because the
  reveal rule needs an instant (S26).

## S26 — A surprise invitation hides the venue, and the host chooses the reveal

> "yo invito a alguien, elijo si le muestra una pista o no ... ese usuario recibe la
> invitación, ve mi mensaje custom, la fecha y hora y la pista si lo marqué, pero si no marqué
> que sea así modo sorpresa, sí le llega todo normal con el nombre del local. La pista es por
> ejemplo 'lleva atuendo cómodo y cálido, porque en una cena de pasta en un rooftop'."

Rules:
- `tb_plan_invitation` gains `tx_mode` (`PLAIN` | `SURPRISE`, default `PLAIN`), `tx_message`
  (host's text, ≤ 500 chars) and `tx_hint` (≤ 200 chars, optional).
- `PLAIN`: the invitation carries the plan with its place, exactly as any plan read.
- `SURPRISE`: the invitation carries the host's message, the scheduled date and time, the
  hint when one was written, and the coarse area (country and region). It carries **no** place
  name, address, coordinates, category, menu or photos.
- A `SURPRISE` invitation is rejected unless the plan has `dt_scheduled_at` and at least one
  of message or hint is present: no reveal moment and no content is not a surprise.
- The reveal rule is `AT_SCHEDULED` (default) or `ON_ACCEPT`, chosen by the host per
  invitation. From the reveal instant the guest reads the plan like any member.
- The hide is a projection produced by the read path, not a full object with blanked fields.
  A pre-reveal guest read returns no place-identifying field at all.
- Declining never reveals the venue, and no endpoint shows it to a guest who declined.
- Only a member with `EDITOR` or `OWNER` may author `tx_message` and `tx_hint`.
