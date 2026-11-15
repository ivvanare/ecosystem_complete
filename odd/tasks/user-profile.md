# user-profile — the face behind a co_user

Feature: `GET /v1/users/{coUser}` — the public identity of another account: display name, the
day it appeared, and four counters.
Branch: `feat/api-user-profile` off `dev`. One work-unit commit.
Test runner: `/usr/local/go/bin/go test ./...` inside `api/` (never needs PostgreSQL).

Why: follows, reviews and check-ins all point at a `co_user`. Until now, tapping one of those
names led nowhere — a follow button with no destination. This is the second and last contract
gap before the mobile app is started.

Out of scope: an avatar (the model has no column for one), a listing of a person's reviews or
check-ins (a separate slice), and any viewer-relative flag.

---

## Specs

**S1 — Contract first.** `openapi.yaml` and `API.md` carry the route before the handler exists;
`internal/contract` must pass.

**S2 — Public.** No auth middleware, like the review read that already publishes `{id, name}` as
the author. A profile that demanded a token would leave every author link dead for an anonymous
visitor.

**S3 — The email never appears.** It is a credential, not a profile field. The response carries
the display name only, and `null` is a legitimate value: an account created through Google or
Apple may never have given one.

**S4 — One answer for every invisible account.** Deleted, non-`ACTIVE` and non-existent answer
`404 not_found`. A suspended account is not a profile with a status banner: to a stranger it
does not exist, and that is what keeps a moderation decision private. A malformed identifier is
also `404`, without reaching the store.

**S5 — Counters are live rows.** A moderated review does not count, a deleted one does not
count, and a profile whose owner did nothing answers four zeroes — not an error, not an
omission.

**S6 — One statement.** The account and its four counters in a single query with scalar
subqueries. A profile is read far more often than a person writes; five round trips per screen
would be paid forever.

**S7 — Nothing viewer-relative.** No `followedByMe`: the read is stateless, so a shared cached
answer cannot depend on who asked. The client resolves it from `/v1/follows`.

**S8 — Verification.** `gofmt`/`go vet`/`go build ./...`/`go test ./...` clean, contract green,
and the route exercised over HTTP against a rebuilt image: a real account with the counters
cross-checked against SQL, an `INACTIVE` account, a random uuid and a malformed one.

---

## Tasks

| ID | What | Specs | Route | Status |
|----|------|-------|-------|--------|
| T1 | Contract: `/v1/users/{coUser}` + `UserProfile` schema | S1, S3 | inline | done |
| T2 | Guide: `API.md` §13.4 with the three rules | S1 | inline | done |
| T3 | `internal/profile`: service + one-statement store | S5, S6 | inline | done |
| T4 | Handler + router + `main.go` wiring | S2, S4 | inline | done |
| T5 | Tests: service (4), handler (5) | S3, S4, S5, S7 | inline | done |
| T6 | Live verification + work-unit commit | S8 | inline | done |

---

## Log

**L1** — User, verbatim:

> si dale

Approving the second and last contract gap named in the previous turn: `GET /v1/users/{coUser}`.

**L2** — Evidence gathered before writing (2026-11-15):
`core.tb_follow(co_follower, co_followed)`, `core.tb_checkin(co_user, co_place)`,
`core.tb_review(co_user, tx_status, dt_deleted_at)` and `seg.tb_user(tx_name, tx_email,
tx_status, dt_created_at, dt_deleted_at)` are the only sources needed. The review read already
publishes `author: {id, name}` and documents it as "display-safe author fields; the email is
never exposed" — so this route adds counters and a join date, and releases no identity that was
not already public. `seg.tb_user` has **no avatar column**: the app will have to render initials,
which is a product decision, not an oversight to fix here.

**L3** — Verification, against the rebuilt image (2026-11-15):

- `gofmt`/`go vet` clean; `go test ./...` green — 31 packages, contract included.
- `GET /v1/users/588d734f-…` → 200 with `{"reviews":1,"checkins":1,"followers":1,"following":1}`.
  Cross-checked against SQL for the same id: `1|1|1|1`. The counters are the rows, not an
  estimate.
- The body contains no `@` at all — the email is absent, not hidden.
- An `INACTIVE` account (`72799c5b-…`) → `404`. A random uuid → `404`. `not-a-uuid` → `404`
  without reaching the store.
