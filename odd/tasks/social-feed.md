# social-feed — what the people you follow have been doing

Feature: `GET /v1/feed?cursor&limit` — reviews, check-ins and **public** plans of the accounts
the caller follows, newest first, cursor-paginated.
Branch: `feat/api-social-feed` off `dev`. One work-unit commit.
Test runner: `/usr/local/go/bin/go test ./...` inside `api/` (never needs PostgreSQL).

Why: the social graph had no surface. Following someone changed one flag on a review and
delivered nothing else — the three lists the app could show were "who I follow", "who follows
me" and a profile. A feed is what makes following mean something.

Out of scope: "X started following Y" events (option C). They make a thin feed look busy at the
cost of exposing who follows whom, and that is not the reader's business.

---

## Specs

**S1 — Three sources, one shape.** Reviews, check-ins and plans of followed accounts, merged by
`dt_created_at` descending. Each row carries `kind`, `at`, the author (`actor`), and the payload
of its kind.

**S2 — Authenticated, and the block applies here.** Unlike the public profile, this read knows
who is asking, so a block filters in **both** directions. This is the first read where that is
true and it must not be forgotten.

**S3 — Only what the reader could open anyway.** A review must be live and `ACTIVE` and its
place visible; a check-in must point at a visible place; a plan must be `PUBLIC` and not deleted.
The feed is a shortcut to things already public, never a leak of something the reader could not
reach by tapping.

**S4 — Cursor, not offset.** A feed paginated by `page` skips or repeats rows as soon as
something new arrives while the reader scrolls. The cursor is `(created_at, id)` — opaque to the
client, base64url — and the next page is strictly older. `nextCursor` is `null` on the last page.

**S5 — A malformed cursor is `422`, not a silent first page.** Silently restarting at the top
looks like a loop to the reader and hides a client bug.

**S6 — One query.** The three sources are a `UNION ALL` with a `LIMIT`, not three queries merged
in Go: merging in memory would need a full page from each source to be correct, which is three
times the work for one answer.

**S7 — Empty is an array.** Following nobody, or nobody having done anything, answers
`{"data":[],"nextCursor":null}` — a real state, not an error.

**S8 — Verification.** `gofmt`/`go vet`/`go test` clean, contract green, and the route exercised
over HTTP with a real follow relationship, a block, and a cursor walk that returns every row
exactly once.

---

## Tasks

| ID | What | Specs | Route | Status |
|----|------|-------|-------|--------|
| T1 | `internal/feed`: cursor, service, one-statement store | S1, S4, S6 | inline | done |
| T2 | Handler + router wiring | S2, S3, S5, S7 | inline | done |
| T3 | Contract: `openapi.yaml` union + `API.md` | S8 | inline | done |
| T4 | Tests: cursor round-trip, service, handler | S4, S5, S7 | inline | done |
| T5 | Live verification | S8 | inline | done |

---

## Log

**L1** — User, verbatim:

> dale con la B

Choosing option B for the feed's content: reviews, check-ins and public plans. Follow events were
offered as option C and declined.

**L2** — Evidence before writing (2026-11-15):
`core.tb_follow(co_follower, co_followed)` is the edge; `core.tb_route` carries `co_user` as the
owner and `tx_visibility` (`PRIVATE`|`PUBLIC`); `core.tb_checkin` has no soft delete; every plan
in the development database is currently `PRIVATE`, so a plan row only appears once one is
created as public.

**L3** — Verification against a rebuilt image (2026-11-15), with real rows:

- Followed the author of an existing review, and the feed returned her **check-in and review**,
  newest first, each with the place title.
- Created a `PUBLIC` plan and gave it that author as owner: the feed then returned **three**
  kinds in one page — `plan`, `checkin`, `review` — which is the union working, not a merged
  list built in Go.
- **Cursor walk with `limit=1`**: 3 pages, 3 rows, **3 unique** — no gaps, no repeats, and the
  order strictly older.
- **Block**: `POST /v1/blocks` on that author emptied the feed (`{"data":[],"nextCursor":null}`),
  and unblocking restored the three rows. This is the read where the block actually applies.
- **Malformed cursor**: `422 {"field":"cursor","code":"invalid"}`.
- Cleaned up afterwards: unfollowed, soft-deleted the throwaway plan and its member rows. The
  feed went back to empty and the catalogue to its three live plans.
