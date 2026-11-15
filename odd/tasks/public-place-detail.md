# public-place-detail — the read the mobile app cannot live without

Feature: `GET /v1/places/{coPlace}` — the public place detail: photos, weekly hours with the
open-now flag, menus, attributes and the published rating.
Branch: `feat/api-place-detail` off `dev`. One work-unit commit. Do not push without asking.
Test runner: `/usr/local/go/bin/go test ./...` inside `api/` (never needs PostgreSQL).

Why this slice exists: the mobile app can list and map places today, because `/v1/places`
returns `PlaceSummary`. It cannot open one — there is no detail route and no public read of
photos, hours or menus, which exist only as backoffice `PUT`s. Contract-first is the rule of
this repository, so the contract is completed before the app is started.

Out of scope, deliberately: push notifications (no device-token table exists), a public user
profile (`GET /v1/users/{coUser}` lands in a following slice), and any personalized flag such
as "followed by me" — this read is public and stateless.

---

## Specs

**S1 — Contract first.** `openapi.yaml` (English) and `API.md` (Spanish, matching the file)
carry the route before the handler exists. `internal/contract` must pass:
`TestEveryRouteIsInTheOpenAPIContract`, `TestEveryRouteIsInTheConsumerGuide`, and the test that
requires every emitted error code to have an assertion. Register as
`mux.HandleFunc("GET /v1/places/{coPlace}", ...)` — the contract test scrapes that exact shape.

**S2 — Public, like discovery.** No auth middleware. `/v1/places` is public; a detail that
demanded a token would let the app list places it cannot open. No `Authorization` header is
required, read or echoed.

**S3 — One answer for everything invisible.** Deleted, non-`ACTIVE` and non-existent places all
answer `404 not_found`. Never `403`, never `410`: distinguishing them confirms existence.
A malformed path identifier is also `404`, not `422` — a bad uuid and a missing place are the
same fact to a client.

**S4 — The legal name and the owner never appear.** The read path exposes `tx_title` as `name`;
`tx_name` and `co_owner_user` stay in the backoffice, exactly as the discovery read already
does (`placeadmin` may see them, this route may not).

**S5 — Open-now is computed by the database, in the place's timezone.** Reuse the predicate
that already serves the `openAt` discovery filter: `EXTRACT(ISODOW FROM now() AT TIME ZONE
p.tx_timezone)` with both the same-day and the overnight branch (`close < open`). Do **not**
compute it in Go: the runtime image is `alpine` without `tzdata` and the binaries are static, so
`time.LoadLocation` fails at runtime — and a second copy of the rule would drift from the first.

**S6 — Absence is not zero.** `openNow` is `null` when the place has no schedule rows, and
`rating` is `null` when no aggregate was published. A place nobody rated is not a 0-star place,
and a place with no hours is not closed. `null` is the contract for "unknown".

**S7 — Four queries, not four per aggregate.** Place+category in one, photos in one, hours in
one, menus+items+currency in one, attribute links in one: five fixed statements regardless of
how much content the place has. No query inside a loop.

**S8 — Order is contractual.** Photos: `COVER` first, then `GALLERY` by `nu_order`. Hours: by
`nu_day_of_week` then open time, `dayOfWeek` in ISO numbering (1 = Monday, 7 = Sunday).
Menus: by creation, items by creation. Attributes: by `tx_code`.

**S9 — Empty is an array.** A place with no photos, no menus or no attributes answers `[]`, never
`null`, so a client renders without guarding every field.

**S10 — Verification.** `gofmt`/`go vet`/`go build ./...`/`go test ./...` clean, contract green,
and the route exercised over HTTP against a rebuilt image with a real place id: 200 with the
expected content, 404 for a random uuid, 404 for a retired place.

---

## Tasks

| ID | What | Specs | Route | Status |
|----|------|-------|-------|--------|
| T1 | Contract: `/v1/places/{coPlace}` + `PlaceDetail` schema; reused the existing `PlaceSchedule`/`PlaceMenu`, added `PlaceAttributeRef` | S1, S4 | inline | done |
| T2 | Guide: `api/API.md` §11.4 route, shape and the three rules a client must not reimplement | S1 | inline | done |
| T3 | `internal/places` detail service + store (5 fixed statements) | S5–S9 | inline | done |
| T4 | Handler + router + `main.go` wiring, reusing the discovery store and stats reader | S2, S3 | inline | done |
| T5 | Tests: service (7), handler (5), including nil-error wrapping | S3, S6, S9 | inline | done |
| T6 | Live verification + work-unit commit | S10 | inline | done |

---

## Log

**L1** — User, verbatim:

> ok vamos a tapar los agujeros y iniciemos con la etapa 1 luego que tengamos el contrato completo iniciamos arrancamos el app

**L2** — Evidence gathered before writing (2026-11-15):
`openapi.yaml` has no `GET /v1/places/{coPlace}`; `PlaceSummary` (openapi.yaml:3249) carries no
photos, hours or menus; `PUT` menu/schedules exist only under `/v1/admin`. The open-now rule
already exists at `internal/places/store.go:123-130` using `AT TIME ZONE p.tx_timezone`.
`Dockerfile` runtime stage is `alpine:3` with no `tzdata` and the binary is built `-static`, so
`time.LoadLocation` has no database to read — verified with `ls /usr/share/zoneinfo` inside the
running container: absent. Rating comes from `places.NewRedisStats` (worker-published), not from
aggregating `tb_review` at request time.

**L3** — Verification, against the rebuilt image (2026-11-15):

- `gofmt`/`go vet`/`go build ./...` clean; `go test ./...` green — 30 packages in this branch,
  contract included.
- `GET /v1/places/a0000001-…0001` → 200: category, country/region, `timezone: America/Caracas`,
  `openNow: false` (a Sunday, and the place only publishes Monday and Saturday),
  `schedules` with both rows, one menu with two items in USD, attributes, and
  `rating: {average: 3.2, count: 5}` — the same numbers the discovery list returns, so the two
  reads cannot disagree.
- Photos: `…0003` returns its `COVER` first.
- `11111111-2222-4333-8444-555555555555` → 404; `not-a-uuid` → 404 without reaching the store.
- The overnight branch, proved in SQL with a fixed instant: Saturday 2026-11-14 23:00 in
  `America/Caracas` is **open** for the 20:00–02:00 row.

**L4** — A real defect found by the live check and fixed before the commit: `wrapDetailError`
wrapped a `nil` error, so every readable place answered `500` with `%!w(<nil>)` in the log. The
fakes never exercise the SQL path, so the helper is now pinned by
`TestWrappingANilErrorStaysNil`. This is exactly why S10 asks for the route exercised over HTTP
and not only through the suite.
