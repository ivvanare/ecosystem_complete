# Design: Social Discovery — Decisions and Phased Delivery

Source of truth for what exists today: `database/diagram_base.txt`. This document records
the decisions that were open in discussion and are now settled, so later slices do not
relitigate them, plus the delivery plan that makes the whole thing reviewable.

Nothing here is implemented by this change. Phase 1 is specified to implementation detail;
phases 2–4 are specified as rules so their tasks can be cut without redesigning.

---

## D1 — Discovery is query-driven: the coordinate is an input, never a stored trace

The first framing in discussion was "segment users by region so we can recommend places".
That framing is rejected: proximity recommendation does not need to know where a user *is
from*, only where the user *is* when asking. Airbnb answers the same question the same way
— you search where you are going, not where you live.

Consequences:
- The coordinate travels as a request parameter and is discarded with the response.
- `seg.tb_user` gets no location column; there is no column to retain, purge, or export.
- Regional segmentation for reporting is derived from the places a user interacted with,
  because every `tb_place` row already carries `tx_country`.

What this buys: no geolocation PII store, no retention policy for traces, no consent
surface for background location, and recommendations that are always fresh instead of
stale by the age of a stored last-known position.

## D2 — PostGIS in Phase 1; H3 computed in Go, stored nowhere

> Revised. This decision originally deferred PostGIS to a third stage and planned an
> `tx_h3_cell` column in a second one. The human settled that both layers are used, with
> distinct roles (D13), which moves PostGIS into Phase 1 and removes the column. The
> reasoning below replaces the staging table.

### The two layers answer different questions

| Layer | Question | Nature |
|---|---|---|
|| PostGIS | "what is *exactly* inside this radius / this viewport / this polygon?" | Correctness. Exact predicates, spatial index, relational filters in one SQL statement |
| H3 | "how many are up there, grouped?" | Cardinality reduction. Integer arithmetic, groupable, cacheable by a stable key |

They are not alternatives and they are not competitors. PostGIS is the source of truth for
geometry; H3 is a derived bucket used for aggregation, clustering and cache keys.

### What Phase 1 adds

- `nu_latitude`/`nu_longitude` stay `NOT NULL decimal(9,6)` with their shape CHECK. They
  remain the contract; the spatial column is derived from them.
- `co_location geography(Point, 4326)` derived from the two decimals, with a GiST index.
  Prefer `GENERATED ALWAYS AS (...) STORED`; if the expression is not accepted as
  immutable, a trigger. Either way a test asserts the derived value agrees with the
  decimals, because silent drift is the only new failure mode this column introduces.
- `ST_DWithin` for the radius (metres, exact) and `ST_MakeEnvelope` for the map viewport.
- Polygons are **not** added (D14): `ST_Contains` waits until administrative data exists.

### What Phase 1 does not add

- **No `tx_h3_cell` column.** H3 is computed in Go (D13). With PostGIS narrowing the
  candidate set, a viewport returns hundreds of rows, not fifty thousand; grouping those
  in memory costs nothing and adds no extension.
- **No `h3-pg` extension.** The service and the worker are both Go, so the cell is
  computed where the code already lives, and the image carries one extension, not two.

### Cost, stated rather than hidden

The image `postgres:15-alpine` (`docker-compose.yml`) becomes `postgis/postgis:15-3.x`.
Same PostgreSQL major, so the existing volume stays valid; `CREATE EXTENSION postgis`
ships as its own ledger file instead of editing an applied migration. This is item 5 of
the risk list (delivery/environment), taken deliberately: the map screen is a stated Phase
1 product element, and a viewport query is what the decimals-plus-btree pair answers badly.

`CREATE EXTENSION postgis` needs a privileged role. In development the compose user is the
init superuser; in production the migration runner's privilege must be confirmed before
the release, not during it.

### The retirement path stays open

`DROP COLUMN co_location` plus the previous image returns the deployment to bounding box
plus haversine with the decimals untouched. That is the property that made staging
attractive, and keeping the decimals as the contract is what preserves it.

`decimal(9,6)` is sufficient: 6 decimals is ~11 cm of precision, and the latitude range
needs 2 integer digits, longitude 3 — both fit in 9 total.

## D3 — Trending is computed, not counted on request

"Trendy" is a ranking, not a column and not an aggregate. The worker computes a score
per place (recent review velocity and volume-weighted rating, later saved places and
check-ins), aggregates it per coarse zone, and writes the ordered result to Redis.

Why not compute on read: `COUNT(*)`/`AVG` over `tb_review` in a discovery request turns the
most frequent endpoint into a full aggregate over the fastest-growing table. It also makes
"trending" mean lifetime popularity, which is not what the word means.

Degradation rule: no ranking for a zone falls back to rating-then-name. A cold cache is a
degraded answer, never an error.

## D4 — Country stays a plain ISO code; region is added as a column, not a catalog

The question raised was whether country should become a table with relations, to decide
availability per region and to segment users. Settled as:

- **Country:** unchanged. `tx_country char(2)` with a shape CHECK. ISO 3166-1 is already
  the governing catalog; a `tb_country` table would duplicate it and add a FK to every
  row that references a country, including `tb_place_history`, which must survive catalog
  changes anyway.
- **Region:** `tx_region varchar(6) NULL` holding ISO 3166-2 (`VE-A`, `US-CA`). A column,
  not a catalog, for the same reason.
- **Availability per region** (if it is ever requested) is a *relation*, not a column:
  a rollout flag/table between a country or region and a capability. It is explicitly not
  built now — there is no consumer, and building it speculatively is the same mistake as
  the free-text `tx_category` that migration 12 removed.
- **Segmenting users by region** is served by activity, not by a new user column: reviews,
  votes, check-ins, and plan membership all point at places that carry country and region.

NULL for `tx_region` is deliberate. Requiring it would make a place uninsertable in every
country where the geocoder cannot answer, which is the exact class of bug that
`14-relax-timezone-check.sql` fixed for timezones.

## D5 — The social graph lives in `core`, not `seg`

`seg` is identity and security: users, tokens, roles, permissions, audit. The follow graph,
plans, votes, and check-ins are domain behavior that happens to reference users. Putting
them in `seg` would mix "who you are" with "what you do", and `seg` is the schema designed
to be extractable (`pg_dump -n seg`).

Cross-schema FKs are already the model's convention (`tb_review.co_user → seg.tb_user`), so
`core` referencing `seg.tb_user` costs nothing new.

## D6 — Follow is asymmetric; friendship is derived at read time

Instagram's model, not Facebook's: `A follows B` says nothing about `B follows A`.
"Friend" is the mutual case and is computed, never stored.

Rejected: an `is_friend` flag on the review or the follow row. The graph changes daily;
a denormalized flag would be wrong within a day of any follow or unfollow and would need
the fan-out machinery it was meant to avoid.

Rejected: a separate `tb_friend` table. It would be a materialized copy of a two-row
intersection that the database already answers with a self-join, and keeping two sources of
truth for "are we friends" is the mistake the history tables' design notes call out
explicitly.

Scale path: the viewer-relative highlight starts as a JOIN with `LIMIT`; if it becomes hot,
it is fanned out to Redis. That is a response to measurement, and it must not change the
response contract.

## D7 — Plan visibility is an attribute of the plan; membership is a relation

> "armar un festival publico y la comunidad se une o creo uno privado para mis panas"

A public festival and a private trip are the same entity with different doors, not two
entity types. `tx_visibility` (`PRIVATE` | `PUBLIC`) on `tb_route` selects the door:
invitation-only versus self-join.

`tx_visibility` defaults to `PRIVATE`. A plan must never become public by omission — the
safe value is the default, and publishing is an explicit act.

`tb_route_member` carries the role, not the plan: `OWNER` (creator, and the only role that
can change visibility or suspend), `EDITOR` (orders the plan, adds places), `VIEWER`
(read, vote). `tb_route.co_user` stays as the creator reference, so existing history and
queries keep working; ownership for authorization reads the member row.

Enums over free-text `tx_role`: roles are a closed set whose values participate in
authorization checks. A typo in a free-text role is a silent privilege bug; an enum value
that does not exist is a rejected write.

## D8 — Self-join grants the least privilege

A public plan open to self-join with `EDITOR` is a plan anyone can rearrange, rename, and
empty — the single most likely abuse of this feature, and a festival is the worst case
because it is public *and* large.

Settled: self-join grants `VIEWER` (read + vote). `EDITOR` is granted by an `OWNER`.
This is a policy decision, and it lives in the database-facing service layer, tested
directly, because getting it wrong silently is worse than a visible failure.

Anonymous (unauthenticated) participation is out of scope. Voting and joining require a
session, consistently with every existing write.

## D9 — Collaborative ordering: manual is the base, votes are an overlay

> "crear rutas que las personas voten como spotify"

A Spotify-style collaborative list has one ranking, but plans also have an intended
sequence — a festival has a stage order, a day trip has a driving order. So:

- `nu_order` remains authoritative when a plan has no votes.
- With votes, ranking is net votes desc, ties by `nu_order`, then creation time, so the
  order is deterministic and reproducible.
- An `OWNER` override is recorded rather than fought: silently ignoring votes produces the
  worst UX (votes appear to do nothing) and quietly ignoring the override loses the owner's
  intent.

`nu_vote ∈ {-1, 1}` and the composite primary key `(co_route, co_place, co_user)` are what
make votes idempotent and reversible without an update storm.

## D10 — The friend highlight is viewer-relative, so it cannot be a column

If "my friend was here" were a stored attribute, it would have to be recomputed for every
possible viewer, which is impossible for a table. It is a projection of
`review × viewer's follow set`, resolved per request.

Consequence for the UI: the same review is highlighted for one user and plain for another.
That is correct behavior, and the API must therefore never return a cached review list
shared across users unless the cache key includes the viewer.

## D11 — Location and privacy: what is allowed to be stored

Question raised: "should we store the user's location to learn their taste and recommend to
them later?"

Settled: **no raw location storage.** Storing position becomes a liability with a
retention policy and a consent surface for a weak signal — being near a place is not
liking it, and a user who walks past the same bar daily is not expressing a preference.

What is stored instead:

| Stored | Nature | Deletable |
|---|---|---|
| Place coordinates | Public business data | n/a |
| `tb_checkin` | Explicit user act | Yes (user's own) |
| `tb_review`, votes, plan membership | Explicit user acts | Yes |
| `tb_user_taste_profile.js_profile` | Derived aggregate, no coordinates | Yes — recomputable |
| Opt-in coarse zone, TTL | Notification bucket, H3 cell at low resolution | Yes — off by default |

The one concession to a stored location, explicitly bounded: a single coarse cell (a city
or district, never a point) set by an explicit opt-in, used only to answer "notify me when
this area has news". Off by default, removable, and it has a TTL so it cannot become a
silent archive.

And the rule that makes the whole design testable: `tb_security_event.tx_country`/`tx_city`
keep their documented meaning (IP-derived, approximate) and are never reused as taste
input. Two sources of truth for "where is this user" is the failure mode this decision
exists to prevent.

## D12 — Six phases, dependency-ordered, each reviewable in slices

| Phase | Change | Delivers | Depends on |
|---|---|---|---|
| P0 | `geo-and-scheduler` | PostGIS image + extension, `co_location` + GiST, `tx_region`, scheduler in the worker | — |
| P1 | `place-discovery` | `GET /places` by radius and viewport, search by name and dish, price/attribute/schedule filters, place detail, reviews write API, real trending producer | P0 |
| P2 | `social-graph` | `tb_follow`, `tb_checkin`, viewer-relative highlight | P1 (places and reviews exist) |
| P3a | `plan-collaboration` | `tx_visibility`, `tb_route_member`, `tb_route_place_vote`, public join, plan date and party size | P2 (voting needs the graph for social proof) |
| P3b | `plan-invitations` | `tb_plan_invitation`, RSVP, expiry, surprise reveal | P3a (a membership is what accepting creates) |
| P4 | `taste-profile` | `tb_user_taste_profile`, worker batch, notifications | P1–P3 (all signals) |

The Phase 1 contract wholesale — routes, payloads, SQL shape, cache keys, error mapping and
test plan — lives in `p1-discovery.md`, so this document stays a decision record.

Review workload drives the split: ~400 authored changed lines per slice, with its
migration, its enum, its contract files, its tests, and its docs in the same commit. Each
phase is independently verifiable and independently revertable.

Order is not arbitrary. P0 comes first because three later features depend on a scheduler
that does not exist today, and because the image change is cheaper once than twice. P2
without P1 has nothing to be near; P3a without P2 has plans but no social proof and no way
to check that self-join grants the least privilege safely; P3b without P3a has an
invitation that grants nothing; P4 without P1–P3 has no signals to learn from.

## D13 — H3 is computed in Go, at the moment it is used, and stored nowhere

H3 is a *derived bucket*, never a stored fact. There is no `tx_h3_cell` column in Phase 1
and no `h3-pg` extension.

Two consumers, one rule:

- **The read path** groups the rows a spatial query already returned. A viewport bounded
  by `ST_MakeEnvelope` and indexed by GiST yields hundreds of rows; grouping them into
  hexagonal buckets in memory is arithmetic, not a database problem.
- **The worker** computes the cell to build the trending zone key it writes to Redis. That
  is the one place a cell is *persisted*, and it is persisted as a cache key, not as a row
  attribute.

Zoom levels do not multiply storage. One resolution is chosen, and parent cells at any
other level are derived by bit-shifting the index (`cellToParent`). Storing one column per
zoom level is the shape this decision exists to prevent.

The reason is not purity: nothing in the service writes `core.tb_place` today — the only
Go production code touching places is absent — so a stored cell would have no writer, and a
column filled by hand is a column that drifts.

When a stored cell eventually earns its place (country-scale map views, or a pre-bucket
that measurably beats the spatial index), it arrives as an additive migration with a
backfill, and D2's invariant — the decimals are the contract, everything else is derived —
still holds.

## D14 — H3 zones are not barrios, and the API must not pretend they are

> Decision taken with the human: no administrative polygons in Phase 1; an H3 cell stands
> in for a neighbourhood until real data exists.

Consequence, stated plainly: a hexagon does not know it is called Malasaña. It does not
align with municipal boundaries, so it cannot name a barrio, cannot answer "is this inside
the official district", and cannot be presented to a user as one.

Rules that follow:

- Phase 1 groups and reports by **zone**, never by neighbourhood name. Copy says "42 places
  within 2 km", not "Malasaña".
- A cell-to-display-name table per launch city is a product decision, not a free by-product:
  it is a manual data set that goes stale the moment a city's barrios change.
- `tx_region` keeps coming from reverse geocoding, tolerant of "no answer" (NULL), exactly
  as originally designed. It is not derived from cells.
- When administrative polygons are loaded, `ST_Contains` resolves both the barrio and
  `tx_region` from one source of truth, and the read contract does not change — only the
  labels gain precision. That is why loading polygons later is additive rather than a
  redesign.

Rejected for now: importing OpenStreetMap `admin_level` polygons. It is real data work
with real licensing and refresh questions, and it buys naming, not discovery.

## D15 — The cell cache is viewer-neutral; the friend highlight is applied after it

> "If several users are in the same hexagon, they get the feed from cache without touching
the database." That is a good idea with one sharp edge.

D10 says the friend highlight is a projection of `review × viewer's follow set`: the same
review is highlighted for one user and plain for another. A cache keyed only by cell would
serve one user's highlights to another user. That is not a performance detail; it is
visible cross-user data corruption.

Settled, in this order:

1. The cell cache holds **only viewer-neutral payload**: places, counts, trending rank,
   aggregate signals. Nothing that depends on who is asking.
2. The viewer-relative layer (friend highlight, "your friends are going", personal order) is
   applied **after** the cached payload is read, per request.
3. A cache entry that is inherently viewer-relative must carry the viewer — or better, the
   viewer's follow-set version — in its key. Preferred answer: do not cache those payloads
   at all until measurement forces it.

This is the rule to check in review on every new cached read path, because the failure is
silent: it looks like a working feed.

## D16 — Invitations are a state machine with a read-enforced expiry

> "invitaciones a grupos de amigos que decidan si van o no, que a su vez expiren en un
tiempo"

An invitation is not a flag and not a member row with a status. It is its own entity with
its own lifecycle:

- `core.tb_plan_invitation (co_invitation, co_route, co_user, co_inviter, tx_status,
  dt_expires_at, dt_created_at, dt_responded_at)`.
- `public.plan_invitation_status`: `PENDING`, `ACCEPTED`, `DECLINED`, `EXPIRED`, `REVOKED`.
- A partial unique index on `(co_route, co_user) WHERE tx_status = 'PENDING'`: one live
  invitation per person per plan, while history keeps every past attempt.
- Accepting inserts the `tb_route_member` row with role `VIEWER` in the same transaction
  that flips the invitation. D8 stands: accepting never grants `EDITOR`.
- Invitations target **registered users only**. No shareable token in this phase: a token is
  issueable to anyone, revocable, rate-limited and enumerable — a different feature with a
  different threat model.
- **Response visibility is asymmetric on purpose.** The host and any `EDITOR`/`OWNER` read
  every invitee's status; an invitee reads only their own invitation, plus the list of members
  who accepted. Who declined is not community information, and a plan is not a leaderboard.

The expiry rule is the part that decides whether a dead scheduler is harmless:

- **The read predicate is the truth.** Every read and every accept requires
  `dt_expires_at > now()`; an invitation past its expiry cannot be accepted even if no
  sweeper ever ran.
- **The sweeper is hygiene.** A scheduled job flips stale `PENDING` rows to `EXPIRED` so the
  table and the admin views stay honest. If it stops, invitations are still correctly
  unusable.

Inverting that order — trusting cron to enforce — is how an expired invitation stays
acceptable for a month after a deploy that broke the job.

## D17 — Reviews are the first domain write path, and therefore the first real signal

> Decision taken with the human: the reviews API ships in Phase 1 rather than later.

The honest finding behind it: `core.tb_review` has no write path in the service, the live
database holds zero places and zero reviews, and `core.tb_place` appears in Go only inside
test fixtures. A trending ranking computed from "review velocity" would therefore have
computed over an empty table — the producer missing was the visible half of a larger gap.

Settled: Phase 1 adds the reviews write API, contract-first, so the trending batch has a
genuine signal from the day it starts running.

What that write path owes the rest of the model:

- **Contract first.** `openapi.yaml` + `API.md` + `internal/contract` in the same commit as
  the handler. Adding a domain write path is a three-file change by design.
- **One active review per user per place**, enforced by the existing partial unique index
  `ux_tb_review_user_place_active`, surfaced as a conflict error code, not a 500.
- **Moderation uses the capability that already exists**: `review.moderate` is seeded in
  `06-seeds.sql`, and `review_status` is already `ACTIVE | INACTIVE | MODERATED`. No new
  permission is invented.
- **History is written by the trigger, not the service.** `tb_review` is in the point-in-time
  set. The handler sets `app.current_user_id` for the transaction so `co_actor_user` is
  recorded, and never writes a history row itself — D-side rule from the history design.
- **A review is not an aggregate.** The place's average and count are computed by the worker
  into the same zone payload as the ranking, so no discovery request aggregates
  `tb_review`.

Deferred with a reason: place CRUD. Places still arrive by seed and ledger, and a places
write API is its own slice with its own permissions (`place.create`, `place.update`).

## D18 — A place has one category and many attributes; they are different catalogs

> "filtrar por categoria, ejemplo rumba, playa noche, terraza, petfriendly"

Those four things are not the same kind of data, and modelling them as one list is the
mistake migration 12 already paid for:

| Example | What it really is | Why it cannot be a category |
|---|---|---|
| `rumba` | category | It is what the place *is* |
| `terraza` | attribute | A place can be a restaurant *and* have a terrace |
| `petfriendly` | boolean attribute | Nobody looks for "a petfriendly" as a destination type |
| `playa noche` | attribute + time | Surface is an attribute; "noche" is a schedule question, not a taxonomy |

Settled:

- `core.tb_place_category` stays as it is: one category per place, governed catalog, already
  seeded with `hotel`, `arepera`, `parque`.
- New `core.tb_place_attribute` (`tx_code`, `tx_name`) plus `core.tb_place_attribute_link`
  with the composite primary key `(co_place, co_attribute)`. Many attributes per place.
- Filters on attributes are **AND** semantics: `attributes=terraza,petfriendly` returns places
  that have both, because that is what a person means by asking for both.
- `playa noche` becomes `attributes=frente_playa` plus `openAt=<ISO-8601 instant>`, answered
  from `core.tb_place_schedule` and the place's own `tx_timezone`. The schedule model already
  handles closing after midnight (`dt_close_time <= dt_open_time`), which is exactly the
  night case, so no new machinery is invented for it.
- Categories and attributes both need seeding (`rumba`, `terraza`, `frente_playa`,
  `petfriendly`, …) as governed vocabulary in a ledger file, reviewed like any catalog.

Rejected: a `js_tags` blob on the place. It cannot be filtered by the database with an index,
and it re-introduces free text wearing a JSON hat.

## D19 — Prices are USD in Phase 1; no rate, no currency parameter

> Settled with the human: "si por ahora sera moneda USD".

The model already carries the hard part: `tb_menu_item.nu_price decimal(10,2)` with a
mandatory `co_currency`, and `tb_currency` holding `USD`, `VES`, `COP` with `nu_decimals`.

Settled for Phase 1: the price filter is **USD**, full stop. `maxPrice` is an amount in
dollars; the filter joins `tb_currency` on `tx_code = 'USD'` and compares only items priced
there. There is no `currency` query parameter, because a parameter that accepts one value is
dead surface pretending to be flexibility.

The consequence, stated plainly because it is a product consequence: **a menu item priced in
bolívares is invisible to the filter.** If the catalogue is entered in USD, as decided, this
costs nothing; the day a place publishes in VES, its items simply do not appear in a
price-filtered search until multi-currency lands.

Adding multi-currency later is additive: an optional `currency` parameter defaulting to USD,
plus `tb_fx_rate` (pair, rate, effective interval) and a derived `nu_price_usd` if
cross-currency comparison is ever required. In VES, which rate is authoritative is a business
decision — official and parallel differ by an order of magnitude — so it stays a decision, not
a constant embedded in a query.

`tb_currency` remains the only authority for decimals; the API never formats money itself.

## D20 — A surprise invitation hides the venue, and the host chooses the reveal

> Settled with the human, overriding an earlier recommendation of mine: "yo invito a alguien,
> elijo si le muestra una pista o no ... ese usuario recibe la invitación, ve mi mensaje custom,
> la fecha y hora y la pista si lo marqué, pero si no marqué que sea así modo sorpresa, sí le
> llega todo normal con el nombre del local. La pista es por ejemplo 'lleva atuendo cómodo y
> cálido, porque en una cena de pasta en un rooftop'."

Two modes, chosen per invitation by the host:

| Mode | What the guest receives |
|---|---|
| `PLAIN` (**default**) | The plan as any member would read it, place name included |
| `SURPRISE` | The host's message, the scheduled date and time, the hint if one was written, and the coarse area — **no** place name, address, coordinates, category, menu or photos |

The earlier version of this decision argued that a guest cannot consent to a hidden
destination. That argument is withdrawn as the *rule* and kept as the *cost*, because the
human's model is a real product need and it is his call: the hint is what prepares the guest —
the rooftop example exists precisely so the guest can dress without knowing the venue.

What the design owes in exchange, cheaply:

- The hide is total for place identity, not partial: a redacted projection returned by the
  read path, never a full object with fields blanked at the edge.
- The **coarse area stays visible** (country and region, never address or coordinates). An
  invitation you cannot place even roughly — another city, another country — is not a
  surprise, it is a trap.
- A `SURPRISE` invitation requires a scheduled date and time. A surprise with no date has no
  reveal moment, and "someday, somewhere" is not an invitation.
- A `SURPRISE` invitation requires at least one of message or hint. Otherwise the guest
  receives an empty envelope.
- The reveal rule is the host's: `AT_SCHEDULED` (default — the venue unlocks at the plan's
  time, which is what makes the surprise last) or `ON_ACCEPT` (unlocks when the guest says
  yes, for a guest who needs to prepare logistically).
- Declining never reveals. There is no path that shows the venue to someone who said no.

## D21 — The mobile app is the consumer; the admin panel only administers

> Stated by the human on 2026-11-02: "la web melia-admin sera netamente administrativa y de
> gestion, la aplicacion movil aun no esta hecha, por eso tenemos chance de hacer la base de
> lo que sera el rumbo de la app".

This settles a question the change had left implicit. Discovery, reviews, follow, check-ins,
plans and personalization are **mobile surfaces**. `melia-admin` is a backoffice: it manages
users, roles, places and moderation. It renders data; it is not where a person discovers a
place or checks in.

Consequences that shape what comes next:

- The admin panel is **not** a test bed for consumer UX. A feature that only makes sense in
  the mobile app does not need an admin screen to be "complete", and an admin screen is not
  evidence that the mobile flow works.
- Because the mobile client does not exist yet, the **API contract is the specification of
  the app's direction**. Getting `GET /v1/places`, the review list and the check-in rules
  right now is cheaper than changing them after a client is built against them — which is
  why this phase invests in contract-first routes and honest error codes rather than in
  either UI.
- The admin routes that remain in scope are the ones a moderator or an operator needs:
  reviews listing and moderation, place ownership and management. The SPA session's scope
  note ("nada de esto habilita la pantalla admin de Places") is correct and expected.
- Anything consumer-facing that would need an admin analogue should be asked once, in the
  change, rather than assumed: the moderation list (`GET /v1/admin/reviews`) was exactly that
  gap and was found by the SPA session, not by this plan.

## Open decisions (non-blocking for P1)

- ~~**Q1 — Check-in validation depth.**~~ **Resolved 2026-11-02: option B, a coordinate
  validated at the edge.** The client sends its position with the check-in; the server
  validates it is within the place's radius using the same edge validation the discovery
  query already uses, and stores **one enum value** — never the coordinate and never the
  distance (D11 still holds). A check-in made outside the radius is rejected, not silently
  downgraded: telling someone "you are not there" is honest, and recording a check-in they
  did not make is not.
  - Rejected for now: the signed proximity token (real proof, but a slice of its own: issue
    endpoint, TTL, single use, binding, replay protection). It arrives only if abuse appears.
  - Rejected: free self-assertion, because it makes the "my friend was here" highlight —
    the whole point of P2 — trivially fake.
  - Why this is enough: it turns lying from trivial into work. It is not cryptographic proof
    and the UI must never say "verified"; the words are "near" or "at the place".
- **Q2 — Blocking before public launch.** P2 ships follow without block. A public social
  surface needs block before it is exposed broadly; it is cheap to add and expensive to
  retrofit once people expect the graph.
- **Q3 — Third visibility value.** `FOLLOWERS_ONLY` (a plan visible to followers but not
  public) was considered and deferred. Adding an enum value later is additive; adding it
  speculatively would force every authorization check to handle a case no UX needs yet.
- ~~**Q4 — Cross-currency price.**~~ **Resolved 2026-11-02:** Phase 1 is USD-only, with no
  `currency` parameter (D19). Reopens only if a place must publish in VES or COP.
- ~~**Q5 — When a surprise unlocks.**~~ **Resolved 2026-11-02:** `SURPRISE` hides the venue,
  the host picks `AT_SCHEDULED` (default) or `ON_ACCEPT`, and the coarse area stays visible
  (D20).
- ~~**Q7 — Does moderation let the author review that place again?**~~ **Resolved 2026-11-02**:
  the predicate gains `tx_status = 'ACTIVE'` (migration `20-review-active-unique.sql`), so a
  MODERATED review leaves the slot free and the author may post a new one while the moderated
  record stays readable. Taken over "moderation also sets `dt_deleted_at`" because that one
  would make the moderated review vanish for its own author and would force the SPA to expose
  two destructive-looking actions. Verified: the new index indexes fewer rows than the old one
  (it adds a conjunct), zero duplicate pairs existed under the new predicate before the change,
  and a second ACTIVE review still fails with 23505 on the same index name, so the 409 mapping
  in the reviews store is unchanged.
- **Q6 — Capacity on public plans.** A private plan needs a party size ("voy solo", "somos
  dos"); a public festival may need a capacity and a waitlist. Both are plausible and they
  are not the same column; P3a ships party size only, and capacity waits for a consumer.
