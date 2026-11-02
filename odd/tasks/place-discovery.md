# place-discovery — P1 commit 1: the discovery read path

Feature: `GET /v1/places` in list mode (radius) and map mode (viewport), with in-memory H3
clustering.
Branch: `feat/api-place-discovery` off `dev`. One work-unit commit. Do not push.
Authority: `openspec/changes/social-discovery/p1-discovery.md` §1–§4 is the operative
contract — read it, plus `spec.md` §S1, §S2, §S14, §S16, §S20 and `tasks.md` T1.1–T1.5. This
file only pins the slice's constraints. Where they disagree, `p1-discovery.md` wins.

`GET`-only slice: no reviews, no trending producer, no search. Those are commits 2–4.

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

## Log

- L1 — Human authorization, verbatim: "no pushemos todavia, hasta que se pruebe, la imagen la
  puedes reconstruir esta bien aun estamos en desarrollo del mvp ... continua con P1".
- L2 — P0 was verified on the live dev database before this slice started: ledger 16 rows,
  postgis 3.5.7, `co_location` stored-generated, both indexes, `tx_region` mirrored, drift
  check `ALL CHECKS PASSED`.
