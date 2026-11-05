# social-graph — P2: follow, check-in and the viewer-relative highlight

Feature: the social layer over discovery — asymmetric follow, check-ins with edge-validated
proximity, and "my friend was here" resolved per viewer.
Branch: `feat/api-social-graph` off `feat/api-place-discovery` (it needs P0–P1's schema and the
reviews read path it enriches).
Authority: `openspec/changes/social-discovery/spec.md` §S5, §S6, §S10, §S11, §S17, §S18;
`design.md` §D6, §D10, §D21 and the resolved Q1. This document pins the slice; where they
differ, the spec wins.

Consumer: the future mobile app (D21). `melia-admin` is a backoffice and gets no consumer screen
from this slice.

---

## Specs

**S1 — Follow is asymmetric and lives in `core`.** `core.tb_follow (co_follower, co_followed,
dt_created_at)` with a **composite primary key**, no soft delete (a logically deleted row would
block ever following that person again — the same reason `tb_route_place` has none), and
`CHECK (co_follower <> co_followed)` so self-follow is impossible even from SQL. Index the
reverse direction (`co_followed`) because "who follows me" is a first-class read.

**S2 — Friendship is never stored.** "Friend" is a mutual follow, computed at read
(`EXISTS(a→b) AND EXISTS(b→a)`). No `is_friend` flag on any row: the graph changes daily and a
denormalized flag rots within a day.

**S3 — Check-in is append-only with an edge-validated proximity claim.** `core.tb_checkin
(co_checkin, co_user, co_place, tx_verification, js_metadata, dt_created_at)`:
- append only: no update path, no soft delete, no `dt_updated_at`;
- `tx_verification` from `public.checkin_verification` (`SELF_REPORTED` | `PROXIMITY`), the
  resolved Q1 option B — the coordinate is validated against the place radius and **discarded**;
  only the enum is stored (D11: no coordinate, no distance);
- one check-in per user, place and day, expressed in the schema: a generated `dt_day date
  GENERATED ALWAYS AS (dt_created_at::date) STORED` plus a unique index over
  `(co_user, co_place, dt_day)`. The rule lives in the database, not only in the service.

**S4 — A check-in outside the radius is rejected, not downgraded.** The request carries
`near=lat,long`; if it validates but falls outside the place's radius, the answer is `422` with
a **new** error code `not_near_place` (field `near`). Recording a check-in the person did not
make would poison the very signal P2 exists to provide.

**S5 — The highlight is viewer-relative and bounded.** The review list (`GET
/v1/places/{coPlace}/reviews`, already shipped with optional authentication) gains one field per
row: `viaFollowedAuthor: bool`, true when the viewer follows the review's author. It is computed
in the read path by joining the viewer's follow set, never stored, and never cached under a key
that lacks the viewer (S18/D15). An anonymous read gets `false` everywhere.

**S6 — The cached payload stays viewer-neutral.** If any caching is introduced for this read
path, the cache key must include the viewer or the answer must be assembled after the cache.
The frontend contract does not change either way.

**S7 — Contract first, one new error code.** Follow and check-in routes go into `openapi.yaml`
(English) and `API.md` (Spanish) before the handlers, with the closed field vocabulary. The only
new code is `not_near_place`; everything else reuses `validation_failed`, `not_found`,
`unauthorized`, `permission_denied`, `internal_error`.

**S8 — History coverage follows the existing rule.** Neither `tb_follow` nor `tb_checkin` joins
the point-in-time set: both are append-only facts, and history answers "what was this entity at
instant T" for entities that change in place (S15). Say so in the ledger file, so the next
person does not "fix" the omission.

**S9 — Tests without services.** Self-follow rejection (including the SQL-level CHECK),
follow/unfollow idempotence, mutual-follow computation, one check-in per day per place, radius
rejection with `not_near_place`, and the highlight being viewer-relative (same reviews, two
viewers, different flags). `go test ./...` needs no Postgres, Redis or RabbitMQ.

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| `POST` | `/v1/follows` | bearer | `{coUser}`. `409`? no — following twice is idempotent, `204`. Self-follow is `422` with field `followed`, code `invalid` |
| `DELETE` | `/v1/follows/{coUser}` | bearer | `204`, idempotent when the follow does not exist |
| `GET` | `/v1/follows` | bearer | `?direction=following\|followers&page&limit`, paginated, display-safe fields only |
| `POST` | `/v1/places/{coPlace}/checkins` | bearer | `{near: "lat,long"}`; `201` with `{coCheckin, verification, createdAt}`; `422 not_near_place` outside the radius |
| `GET` | `/v1/places/{coPlace}/reviews` | optional | gains `viaFollowedAuthor` per row (S5) |

## Repo rules that bite here

- `internal/` stays a closed surface; new packages `internal/follow` (follow — **not** `internal/social`, which already exists for OIDC
  social login) and `internal/checkin` keep their stores behind narrow interfaces.
- Never write history rows from the service; these tables are not in the history set at all.
- The radius check reuses the same edge validation as discovery (`internal/geo`); do not write a
  second coordinate parser.
- A place that is soft-deleted or `INACTIVE` is `404` on every place-scoped route.

## Tasks

- T1 — Branch and ledger `21-social-graph.sql`: `tb_follow`, `tb_checkin`, the
  `checkin_verification` enum, indexes (including the reverse follow direction and the
  per-day uniqueness), and comments explaining why neither table joins the history set.
- T2 — `database/diagram_base.txt` and `database/verify_base_model.sql` updated; drift check
  must pass on a scratch database and on the live one.
- T3 — `openapi.yaml` + `API.md`: the four routes, the new error code in the catalogue, the
  flow in Spanish.
- T4 — `internal/follow`: store and service for follow/unfollow/list, mutual-follow helper.
- T5 — `internal/checkin`: store and service, radius validation through `internal/geo`, the
  `not_near_place` outcome, day-bucket uniqueness.
- T6 — `internal/httpapi`: handlers, wiring in `cmd/api/main.go` behind the existing
  `TokenParser`; the review list gains `viaFollowedAuthor`.
- T7 — tests: the S9 list, RED first.
- T8 — verify: suite and `internal/contract` green, drift check green, SQL proven on the live
  database inside a transaction that rolls back, and an HTTP smoke test of the five routes.

---

## Log

- L1 — Human authorization, verbatim: "excelente tomo la opcion B, la web melia-admin sera
  netamente administrativa y de gestion la aplicacion movil aun no esta hecha por eso tenemos
  chance de hacer la bases de lo que sera el rumbo de la app".
- L2 — Q1 is resolved by that same message: option B. See `design.md` Q1 and L17 in the change's
  log; the coordinate is validated and discarded, and only the verification enum is stored.
- L3 — The consumer clarification is recorded as D21: the mobile app is the consumer, the admin
  panel administers, and the API contract therefore carries the app's direction.
