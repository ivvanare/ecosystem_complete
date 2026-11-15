# user-avatar — the one picture a person publishes about themselves

Feature: `seg.tb_user.tx_avatar_url`, `PUT /v1/me/avatar`, `DELETE /v1/me/avatar`, and `avatarUrl`
on every surface that shows a person.
Branch: `feat/api-user-avatar` off `dev`. One work-unit commit.
Test runner: `/usr/local/go/bin/go test ./...` inside `api/` (never needs PostgreSQL).

Why: the app has to render a face, and the model had no column for one. The alternative was
initials forever, which is not a product decision anyone made — it was an absence.

Out of scope: moderation of avatars. An image upload with no review path is a risk that belongs
to a slice of its own, and today the only account that can upload is the account itself.

---

## Specs

**S1 — A URL, never bytes.** The column holds where the picture lives; the storage seam holds the
file. The same rule place photos follow, and the reason swapping in S3 later is one implementation
and not a migration.

**S2 — Purely additive DDL.** Nullable, no default: no existing row is rewritten, and every
current account reads `NULL`.

**S3 — `PUT`, not `POST`.** Exactly one avatar per account: the same request twice leaves the same
state, and there is no identifier for the client to collect.

**S4 — Sniff the bytes.** The declared content type is not trusted alone; both must agree, so a
renamed executable is not stored as a picture. The name on disk is ours, never the client's
filename. JPEG/PNG/WebP, 5 MiB.

**S5 — Replacing deletes the old object.** An avatar is the one upload a user repeats; orphans
accumulate fast. Deletion is best effort and idempotent.

**S6 — Only the caller's own.** There is no route to change someone else's picture, not even for
an administrator.

**S7 — `null` is a real state.** An account with no avatar returns `null`, never `""`: the client
draws initials, not a broken image.

**S8 — Everywhere a person is shown.** `/v1/me`, the public profile, the author of every review,
and the actor of every feed row. An avatar that appears on one screen is a half feature.

**S9 — Verification.** `gofmt`/`go vet`/`go test` clean, contract green, the ledger applied from a
clean database, and the route exercised over HTTP with a real PNG: upload, serve, replace
(previous object gone), reject a non-image, delete twice.

---

## Tasks

| ID | What | Specs | Route | Status |
|----|------|-------|-------|--------|
| T1 | `34-user-avatar.sql` | S1, S2 | inline | done |
| T2 | `internal/avatar`: set/clear returning the replaced URL | S3, S5 | inline | done |
| T3 | `PUT`/`DELETE /v1/me/avatar` with sniffing and size bound | S3, S4, S6 | inline | done |
| T4 | `avatarUrl` on Me, UserProfile, Review.author, FeedActor | S7, S8 | inline | done |
| T5 | Contract + tests + live verification | S9 | inline | done |

---

## Log

**L1** — User, verbatim:

> sigamos con el listado de resenas el Sin feed de descubrimiento social y luego el avatar

Approving the third item after the by-author review list and the social feed.

**L2** — Verification (2026-11-15), against a rebuilt image:

- The ledger applied through the service: `schema file applied file=34-user-avatar.sql`.
- Uploaded a real 71-byte PNG → `200 {"avatarUrl":"/media/avatars/<uuid>/…png"}`, and
  `GET` on that URL answered `200 image/png`. The picture is actually reachable, not just stored.
- `/v1/me` and the public profile both carried the same URL.
- Replaced it → the new URL answered `200` and the **previous one answered `404`**: the object was
  deleted, not orphaned.
- A `.txt` with a shell body → `422`, and nothing was stored.
- `DELETE` → `204`, and again → `204` (idempotent); `/v1/me` then reported `null`.
- The two review reads and the feed were re-run after gaining the column: 5 place reviews, 1
  by-author review and 2 feed rows all returned `author.avatarUrl`, with no error in the log.
- `go test ./...` green — **35 packages**, contract included.
