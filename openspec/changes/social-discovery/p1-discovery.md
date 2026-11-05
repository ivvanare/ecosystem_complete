# P1 — `place-discovery`: contract and implementation detail

Annex to `design.md` (D1–D20) and `spec.md` (S1–S26). This file is the level of detail a
subagent can implement without asking again: the routes, the payloads, the SQL shape, the
cache keys, the error mapping and the test plan.

Phase 1 ships as four commits (see `tasks.md`): places read, place detail and reviews write,
search and filters, trending producer. Nothing here is implemented yet.

---

## 0. Conventions this annex obeys

Verified in the repository, not assumed:

- **Route registration** must match the pattern `internal/contract` scrapes:
  `mux.HandleFunc("GET /v1/places", ...)`. A route registered any other way is invisible to
  the contract test and therefore fails it.
- **Three-file rule per endpoint**: `api/openapi.yaml` (English), `api/API.md` (Spanish),
  and the handler in the same commit. `TestEveryRouteIsInTheOpenAPIContract`,
  `TestEveryRouteIsInTheConsumerGuide`, `TestEveryErrorCodeIsInTheCatalogue` and
  `TestEveryErrorCodeIsExercisedByATest` all enforce it.
- **Error envelope**: `{"error":{"code","message","details"?}}`. Clients parse `code`.
- **Validation failures use the existing vocabulary**, not a new one: `422`
  `validation_failed` with `details.fields: [{field, code}]`, where `code` is one of
  `required | invalid | too_short | too_long | unchanged | unknown | not_configured`.
- **New objects declare openness**: `TestObjectPropertiesDeclareWhetherTheyAreOpen`
  requires every schema object to state `additionalProperties`.
- **Pagination** reuses the existing `page` / `limit` parameters (defaults 1 and 20, maximum
  100) and the existing `Page` wrapper.

### One correction to S1/S2

S1 and S2 said an out-of-range coordinate is a `400`. That predates reading the error
catalogue: this service answers invalid input with `422 validation_failed` and a field list,
and `400` is reserved for `invalid_json` and `invalid_code`. Phase 1 uses the existing
vocabulary — a coordinate problem is `422` with `field: "near"`, `code: "invalid"` — and S1
is corrected rather than the catalogue widened for one endpoint.

---

## 1. `GET /v1/places` — one route, two modes

The locator parameter decides the mode. Exactly one locator is required; sending both or
neither is a validated error, never a silent default.

| Parameter | In | Type | Notes |
|---|---|---|---|
| `near` | query | `lat,long` | **List mode.** `-90..90` and `-180..180`, closed bounds |
| `radius` | query | integer metres | List mode only. Default `2000`, minimum `1`, maximum `50000` |
| `bbox` | query | `minLon,minLat,maxLon,maxLat` | **Map mode.** Order is longitude-first, like GeoJSON. Anti-meridian crossing is out of scope and rejected |
| `zoom` | query | integer `0..21` | Map mode only. Selects the cluster resolution (§4) |
| `country` | query | `^[A-Z]{2}$` | Filter, both modes |
| `region` | query | `^[A-Z]{2}-[A-Z0-9]{1,3}$` | Filter, both modes |
| `category` | query | uuid | Filter, both modes |
| `sort` | query | enum | List mode only: `distance` (default) \| `trending` \| `rating` |
| `page`, `limit` | query | integer | List mode only; ignored in map mode |
| `q` | query | string, 2–80 chars | Place-name search (S21). Composes with any locator |
| `dish` | query | string, 2–80 chars | Menu-item search; returns the **places** that carry a match (S21) |
| `attributes` | query | repeatable `^[a-z_]{2,32}$` | Attributes, **AND** semantics (S23) |
| `maxPrice` | query | decimal, ≥ 0 | Amount **in USD**. Matches a place with at least one active USD item under it. No `currency` parameter (S22, D19) |
| `openAt` | query | ISO-8601 instant | Open at that instant, in the place's own `tx_timezone` (S23) |

**Authentication:** none. Discovery is read-only and public (S14).

### Why one route and not two

Two routes (`/v1/places` and `/v1/places/clusters`) would duplicate every filter, every
validation rule and every index decision, and would let a client ask for a list with a
viewport or a cluster map with a radius. One route with one locator makes the invalid
combinations unrepresentable instead of merely discouraged.

### List mode — response

```json
{
  "mode": "list",
  "page": 1, "limit": 20, "total": 42,
  "sortApplied": "trending",
  "degraded": false,
  "data": [
    {
      "coPlace": "…", "name": "…",
      "category": { "id": "…", "name": "…" },
      "country": "VE", "region": "VE-A",
      "latitude": 10.480594, "longitude": -66.903606,
      "address": "…",
      "distanceMeters": 940,
      "rating": { "average": 4.6, "count": 128 },
      "trendingRank": 3
    }
  ]
}
```

- `sortApplied` echoes the order that actually ran. `degraded: true` means the requested
  `trending` had no zone key and fell back to `rating` (S4). A client can therefore tell a
  cold cache from a genuinely unpopular place without an error.
- `distanceMeters` is present only in list mode; `null` when absent, never fabricated.
- `rating` is `null` when the worker has published no aggregate for that place. Absence is
  not an error (S12 spirit, applied to places).
- `name` maps to `tb_place.tx_title` (the commercial name) and **never** `tx_name` (the legal
  name), which stays out of the public read path.
- `trendingRank` appears only when `sort=trending` and a zone key existed.

### Map mode — response

```json
{
  "mode": "map",
  "zoom": 12,
  "resolution": 7,
  "total": 311,
  "truncated": false,
  "clusters": [
    {
      "cell": "872830828ffffff",
      "resolution": 7,
      "count": 37,
      "center": { "latitude": 10.48, "longitude": -66.90 },
      "averageRating": 4.3
    }
  ]
}
```

- `total` is the number of places behind the clusters, not the number of clusters.
- `averageRating` is `null` when unknown, never `0`.
- A cluster is a **zone**. It carries no name, no district, no administrative claim (D14, S20).

---

## 2. SQL shape

Both queries filter `dt_deleted_at IS NULL AND tx_status = 'ACTIVE'` first, then the spatial
predicate. The spatial predicate must be expressed **on `co_location`** — the derived column —
or the GiST index is not used and the query degrades silently to a scan.

### List mode, `sort=distance` (index-ordered)

```sql
SELECT p.co_place, p.tx_title, p.tx_country, p.tx_region,
       p.nu_latitude, p.nu_longitude, p.tx_address,
       ST_Distance(p.co_location, $1::geography) AS distance_m
FROM core.tb_place p
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND ($2::char(2) IS NULL OR p.tx_country = $2)
  AND ($3::varchar IS NULL OR p.tx_region = $3)
  AND ($4::uuid IS NULL OR p.co_place_category = $4)
  AND ST_DWithin(p.co_location, $1::geography, $5)
ORDER BY p.co_location <-> $1::geography
LIMIT $6 OFFSET $7;
```

`<->` is the GiST KNN operator: the index walks the places in true distance order, so no sort
of the whole candidate set happens. `ST_DWithin` bounds the scan. This is the only reason
`sort=distance` is cheap.

### List mode, `sort=rating` / `sort=trending`

Same predicate; `ORDER BY` reads the aggregate published by the worker, not `tb_review`:

- `rating`: `ORDER BY rating_avg DESC NULLS LAST, p.tx_title ASC`
- `trending`: no `ORDER BY` in SQL. The zone zset already holds the order; the query fetches
  the page of ids from Redis and then reads those rows `WHERE p.co_place = ANY($ids)`,
  preserving the Redis order in Go. A zone miss falls back to `rating` and sets
  `degraded: true`.

### Map mode

```sql
SELECT p.co_place, p.nu_latitude, p.nu_longitude, s.rating_avg
FROM core.tb_place p
LEFT JOIN …  -- aggregate source, see §5
WHERE p.dt_deleted_at IS NULL
  AND p.tx_status = 'ACTIVE'
  AND p.co_location && ST_MakeEnvelope($1, $2, $3, $4, 4326)::geography
LIMIT $5;
```

`&&` is the geography bounding-box operator and is index-supported. `ST_MakeEnvelope` takes
`(minLon, minLat, maxLon, maxLat)` — longitude first.

### Result caps and the honest-truncation rule

- Map mode caps at **5000 rows**. A bbox that would exceed it is rejected before querying
  (`422`, field `bbox`, code `too_long`) with a message telling the client to zoom in.
- If the cap is ever hit anyway, the response sets `truncated: true`. Cluster counts then
  describe the sampled set, and the client must not present them as totals.
- Rejecting an oversized bbox rather than truncating silently is what keeps cluster counts
  truthful; a wrong count on a map is indistinguishable from a bug.

---

## 3. Validation matrix

| Case | HTTP | `code` | `details.fields` |
|---|---|---|---|
| neither `near` nor `bbox` | 422 | `validation_failed` | `[{locator, required}]` |
| both locators sent | 422 | `validation_failed` | `[{bbox, invalid}]` |
| `near` not `lat,long` | 422 | `validation_failed` | `[{near, invalid}]` |
| latitude out of `-90..90` | 422 | `validation_failed` | `[{near, invalid}]` |
| longitude out of `-180..180` | 422 | `validation_failed` | `[{near, invalid}]` |
| `radius` not an integer | 422 | `validation_failed` | `[{radius, invalid}]` |
| `radius` > 50000 or < 1 | 422 | `validation_failed` | `[{radius, too_long}]` / `[{radius, invalid}]` |
| `bbox` with 3 or 5 parts | 422 | `validation_failed` | `[{bbox, invalid}]` |
| `bbox` with `minLat >= maxLat` or `minLon >= maxLon` | 422 | `validation_failed` | `[{bbox, invalid}]` |
| `bbox` crossing the anti-meridian | 422 | `validation_failed` | `[{bbox, invalid}]` |
| bbox over the area cap | 422 | `validation_failed` | `[{bbox, too_long}]` |
| `zoom` outside `0..21` | 422 | `validation_failed` | `[{zoom, invalid}]` |
| `country` not two uppercase letters | 422 | `validation_failed` | `[{country, invalid}]` |
| `region` not ISO 3166-2 shape | 422 | `validation_failed` | `[{region, invalid}]` |
| `sort` not in the enum | 422 | `validation_failed` | `[{sort, invalid}]` |
| radius search with no results | 200 | — | empty `data`, `total: 0` — **not an error** (S2) |
| `maxPrice` negative or non-numeric | 422 | `validation_failed` | `[{maxPrice, invalid}]` |
| `attributes` with an unknown code | 422 | `validation_failed` | `[{attributes, unknown}]` |
| `q` or `dish` shorter than 2 chars | 422 | `validation_failed` | `[{q, too_short}]` |
| `q` or `dish` longer than 80 chars | 422 | `validation_failed` | `[{q, too_long}]` |
| `openAt` not an ISO-8601 instant | 422 | `validation_failed` | `[{openAt, invalid}]` |
| search with no matches | 200 | — | empty page — **not a 404** |

Every rule here is enforced **before** any database call, and a test asserts that: the
validation tests run against a nil store and still pass.

---

## 4. H3: one resolution stored, parents derived

No cell is stored (D13). The worker writes zone keys at **resolution 7** (~5.16 km² per
cell, edge ≈ 1.22 km), which is the granularity "trending in this area" needs. The map
derives coarser cells per zoom by `cellToParent`:

| `zoom` | cluster resolution | approx cell area |
|---|---|---|
| 0–4 | 3 | ~12 000 km² |
| 5–7 | 5 | ~250 km² |
| 8–10 | 6 | ~36 km² |
| 11–13 | 7 | ~5.2 km² |
| 14–16 | 8 | ~0.74 km² |
| 17–21 | 9 | ~0.11 km² |

Consequences worth stating once:

- Zoom never changes what is stored; it changes an arithmetic argument. That is what keeps
  this from becoming one column per zoom level.
- Clustering happens over the rows the query already returned, so cluster `count` is exact
  within the returned set and honest only up to the cap (§2).
- The cell is serialized as the **H3 hex string**, not a 64-bit integer: JSON numbers lose
  precision above 2^53 and the index uses the full 64-bit range.

---

## 5. Redis: keys the worker owns, keys the API reads

| Key | Type | Written by | TTL | Contents |
|---|---|---|---|---|
| `melia:trending:z:{h3Cell}` | ZSET | worker | 1 h | member `co_place`, score = trending score |
| `melia:place:stats:{coPlace}` | HASH | worker | 24 h | `ratingAvg`, `reviewCount`, `computedAt` |

Rules that make the cache safe:

- **No key contains the viewer, the request coordinate, or anything derived from them.**
  A cell-keyed payload is viewer-neutral by construction (D15, S18).
- The request coordinate is never a key material: it is rounded to a cell, and only the cell
  is used. Keying on raw coordinates would build a trace of where users asked from — exactly
  what D1 forbids.
- A missing key is a degraded answer, never an error: `sort=trending` falls back to `rating`,
  `rating` falls back to `name`, and `rating` fields come back `null`.
- The worker never writes a partial zone: it computes a zone's ranking in memory and writes
  it in one pipeline, so a reader never sees half a ranking.

### Trending score (deterministic, no ML)

The weights are a code constant with a test, not a product decision to relitigate here. The
shape is review recency plus volume-weighted rating; the properties the test pins are what
matter:

- recency dominates volume: 10 reviews this week outrank 100 reviews from last year;
- a place with no reviews scores by rating alone and never outranks a genuinely active one;
- the ordering is stable for identical inputs (deterministic tie-break by `co_place`), so a
  rerun of the same batch produces the same zset and no client sees the list reshuffle for
  no reason.

---

## 6. Reviews API (commit 2 of Phase 1)

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/v1/places/{coPlace}/reviews` | none | Paginated. Hides `INACTIVE`/`MODERATED`/deleted rows from everyone but the author and holders of `review.moderate` |
| `POST` | `/v1/places/{coPlace}/reviews` | bearer | Body `{rating: 1..5, comment?}`. `409 review_already_exists` on the partial unique index |
| `PATCH` | `/v1/places/{coPlace}/reviews/{coReview}` | bearer, author | `403 permission_denied` for anyone else |
| `DELETE` | `/v1/places/{coPlace}/reviews/{coReview}` | bearer, author only | Logical delete (`dt_deleted_at`), so the author may review again later. A moderator retires content through the admin route (S17) |
| `PATCH` | `/v1/admin/reviews/{coReview}` | bearer, `review.moderate` | `{status: ACTIVE\|INACTIVE\|MODERATED}` |
| `GET` | `/v1/admin/reviews` | bearer, `review.moderate` | **Pending.** Lists reviews to moderate, paginated, filters `status` (`ACTIVE`/`INACTIVE`/`MODERATED`), `placeId`, `authorId`, newest first. Without it the moderation screen has no entry point: the admin route needs a `coReview` nobody can discover |

> **Backoffice gap, tracked here because it blocks the SPA session.** Two admin surfaces are
> outstanding and both are read-plus-write routes an operator needs, not consumer features
> (D21): `GET /v1/admin/reviews` with the filters above, and the places backoffice
> (`GET`/`POST`/`PATCH /v1/admin/places` with the ownership `Viewer`). The second belongs to the
> `place-ownership` slice in the admin repository's own plan; it is listed here so the API side
> does not quietly assume someone else is building it. The SPA session confirmed that
> everything else in the panel is migrated to its base components, so these two routes are the
> only remaining blockers for admin work.

Details that are easy to get wrong and are therefore pinned:

- `POST` and `PATCH` set `app.current_user_id` via `set_config(..., true)` at the start of the
  transaction, so the history trigger records `co_actor_user`. The service never inserts into
  `core.tb_review_history` itself.
- A `409` is the partial unique index, not a pre-check race: the handler maps the constraint
  violation (`23505` on `ux_tb_review_user_place_active`) to `review_already_exists`.
- The only new error code in Phase 1 is `review_already_exists`. Everything else reuses the
  catalogue: `not_found`, `permission_denied`, `validation_failed`, `unauthorized`.
- `rating` is `int` and bounded by the table's CHECK; `comment` is `text` with a request-level
  maximum of 2000 characters enforced at the edge.

---

## 7. Error codes: the delta

| Code | HTTP | Where |
|---|---|---|
| `review_already_exists` | 409 | `POST /v1/places/{coPlace}/reviews` |

That is the whole delta. `validation_failed`, `not_found`, `permission_denied`,
`unauthorized`, `internal_error` already exist (API.md §10) and are reused. Adding fewer
codes is the point of §0.

---

## 8. Test plan (RED before GREEN)

Each item is one test that fails first. The store is faked where the rule is validation; the
SQL shape is asserted against a fake that records the query, because these tests must not
need PostgreSQL (`AGENTS.md`).

### Places, list mode

1. `near` missing and `bbox` missing → `422 validation_failed`, `details.fields[0].field == "locator"`.
2. `near=91,0` → `422`, field `near`, code `invalid`; **the store was never called**.
3. `radius=0` and `radius=50001` → `422`, field `radius`.
4. Valid `near` with no results → `200`, `total: 0`, empty `data`.
5. `distanceMeters` is non-null and ascending for `sort=distance` (default).
6. `sort=trending` with a populated zone → order matches the zset; `degraded: false`.
7. `sort=trending` with no zone key → falls back to rating order, `degraded: true`, **200**.
8. `rating` is `null` (not `0`) when the worker published no aggregate.
9. Response never contains `tx_name` (legal name) — asserted on the serialized body.

### Places, map mode

10. `bbox` with 3 parts, and `bbox` inverted, → `422`, field `bbox`.
11. `bbox` over the area cap → `422`, field `bbox`, code `too_long`; store not called.
12. `zoom=12` → clusters at resolution 7; `zoom=15` → resolution 8. Same data, different
    grouping, asserted as a pure function over a fixed row set.
13. Cluster `count` sums to `total` when `truncated: false`.
14. A cluster carries no `name`/`district` field — asserted on the schema, not by convention.

### Reviews

15. `POST` a second active review for the same user and place → `409 review_already_exists`.
16. `POST` after a logical delete succeeds again (the index is partial).
17. `PATCH` by a different user → `403 permission_denied`.
18. `GET` from an anonymous caller hides `MODERATED` rows; the author still sees their own.
19. `PATCH /v1/admin/reviews/{id}` without `review.moderate` → `403`.
20. The transaction sets `app.current_user_id` before the insert (asserted on the fake's
    recorded statements).

### Cache

21. No cache key contains the request coordinate — asserted over keys the read path produced
    for a fixed request.
22. The trending payload contains no viewer-relative field (S18), asserted structurally.

### Contract

23. `go test ./internal/contract/...` green: routes in both files, `review_already_exists`
    in the catalogue, and the new scheduler interval documented in `README.md` and
    `.env.example`.

---

## 9. Files this phase touches

| File | Why |
|---|---|
| `database/init/15..17-*.sql` | Phase 0's ledger files (already cut in `tasks.md`) |
| `api/openapi.yaml` | Both contracts, English |
| `api/API.md` | Both flows, the error code, the field codes — **Spanish**, matching the file |
| `api/internal/httpapi/places.go` (+ test) | Handler, validation, response mapping |
| `api/internal/httpapi/reviews.go` (+ test) | Handler, author checks, conflict mapping |
| `api/internal/httpapi/router.go` | Two registrations in the scraped `HandleFunc` style |
| `api/internal/places/` | Query building, haversine/H3 grouping, sorting |
| `api/internal/reviews/` | Store access, status rules |
| `api/internal/geo/` | Coordinate and bbox validation, H3 resolution mapping |
| `database/init/18-place-attributes.sql` | Attribute catalog and link table (Phase 1, commit 3) |
| `database/init/19-place-search.sql` | `pg_trgm`, `unaccent`, generated `tsvector` columns, GIN indexes |
| `api/internal/search/` | Query composition: text, attributes, price, `openAt` |
| `api/cmd/worker/main.go` | Job registration for the trending batch |
| `api/internal/trending/` | Score, zone aggregation, Redis publication |
| `api/README.md`, `api/.env.example` | The scheduler interval variable |

---

## 10. Non-goals for P1, with the reason

- **Place CRUD.** Places still arrive by ledger and seed. `tx_region` assignment waits for it
  (§11).
- **Text search, opening hours, "open now".** Each is a filter someone will ask for; none is
  in scope, and `openNow` needs schedules joined per row.
- **Polygon containment and neighbourhood names.** No data source (D14).
- **A stored H3 cell, country-scale views.** Additive later with a backfill (D13).
- **Any per-user personalization.** No profile exists until P4; absence must never be an
  error.

---

## 12. Search, filters and the detail route

### `GET /v1/places/{coPlace}` — detail

```json
{
  "coPlace": "…", "name": "…",
  "category": { "id": "…", "code": "arepera", "name": "Arepera" },
  "attributes": [ { "code": "terraza", "name": "Terraza" } ],
  "country": "VE", "region": "VE-A",
  "latitude": 10.480594, "longitude": -66.903606,
  "address": "…",
  "rating": { "average": 4.6, "count": 128 },
  "openNow": true,
  "schedule": [ { "dayOfWeek": 1, "open": "20:00", "close": "02:00" } ],
  "menu": [ { "name": "Cena", "items": [ { "name": "Arepa", "price": { "amount": 4.5, "currency": "USD" } } ] } ]
}
```

- `openNow` and `schedule` are computed in the place's own `tx_timezone` from
  `core.tb_place_schedule`; a schedule that closes after midnight belongs to the day it
  opened, so the overnight case is resolved, not guessed.
- `404` covers soft-deleted, `INACTIVE` and non-existent alike, so a caller cannot enumerate
  retired places through error differences.
- `tx_name` (legal name) and `co_owner_user` are never serialized here (S24).

### Search SQL shape

Both searches use generated `tsvector` columns with GIN indexes; `unaccent` is applied to the
query, and the same expression is used to build the stored vector so the two match.

```sql
-- q: place name
WHERE p.search_name @@ plainto_tsquery('simple', unaccent($1))

-- dish: menu items, returning places
WHERE EXISTS (
  SELECT 1
  FROM core.tb_menu m
  JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
  WHERE m.co_place = p.co_place
    AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
    AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
    AND mi.search_text @@ plainto_tsquery('simple', unaccent($1))
)
```

`'simple'` rather than `'spanish'` on purpose: the analyzer is configured to avoid stemming
place and dish names into each other, and accent folding already happens through `unaccent`.

### Attribute filter (AND semantics)

```sql
AND (
  SELECT count(*) FROM core.tb_place_attribute_link l
  JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
  WHERE l.co_place = p.co_place
    AND a.tx_code = ANY($1)
    AND a.dt_deleted_at IS NULL
) = cardinality($1)
```

Counting the intersection against the requested array is what turns a list of attributes into
"has all of them". A `JOIN … IN` would silently degrade to "has any of them", which is a
different product.

### `openAt`

```sql
AND EXISTS (
  SELECT 1 FROM core.tb_place_schedule sc
  WHERE sc.co_place = p.co_place
    AND sc.nu_day_of_week = EXTRACT(ISODOW FROM ($1::timestamptz AT TIME ZONE p.tx_timezone))
    AND (
      (sc.dt_close_time > sc.dt_open_time
        AND ($1::timestamptz AT TIME ZONE p.tx_timezone)::time BETWEEN sc.dt_open_time AND sc.dt_close_time)
      OR
      (sc.dt_close_time <= sc.dt_open_time
        AND (($1::timestamptz AT TIME ZONE p.tx_timezone)::time >= sc.dt_open_time
          OR ($1::timestamptz AT TIME ZONE p.tx_timezone)::time <= sc.dt_close_time))
    )
)
```

The second branch is the overnight case; without it, a place that opens at 20:00 and closes at
02:00 would be reported closed at 23:00.

### Price filter

```sql
AND ($1::numeric IS NULL OR EXISTS (
  SELECT 1
  FROM core.tb_menu m
  JOIN core.tb_menu_item mi ON mi.co_menu = m.co_menu
  JOIN core.tb_currency c ON c.co_currency = mi.co_currency
  WHERE m.co_place = p.co_place
    AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
    AND mi.dt_deleted_at IS NULL AND mi.tx_status = 'ACTIVE'
    AND c.tx_code = 'USD'
    AND mi.nu_price <= $1
))
```

No rate, no normalisation, no currency parameter (D19): an item priced in another currency is
invisible to this filter, and that is documented behaviour rather than a defect.

---

## 11. Open items this annex does not close

- **Region assignment by reverse geocoding** (S3): the column and CHECK ship in Phase 0, but
  nothing creates places yet, so the assignment hook has no caller. It ships with place CRUD.
- **Q1** (check-in validation depth) blocks P2, not P1.
- **Q2** (blocking) before the social graph is exposed publicly.
- ~~**The truncated product requirement**~~ — resolved 2026-11-02: "saber que mi [invitado
  aceptó]", the host's RSVP view. It lands in P3b as a read endpoint with no migration (S19,
  D16, L12).
- **The map engine on the client.** `melia-landing` has no map library today; whether the SPA
  uses MapLibre, Mapbox or a native SDK changes nothing on the server side of this contract,
  but it is an open product decision recorded here so it is not mistaken for settled.
