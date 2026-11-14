# realtime-votes — live vote propagation over Soketi

Feature: make a plan's vote change reachable by the other members as it happens, without
leaking a private plan's activity and without ever turning a realtime failure into a failed
vote.
Branch: `feat/api-realtime-votes` off `dev`.
Authority: `openspec/changes/social-discovery/spec.md` §S9 ("vote changes propagate live over
Soketi; a client that misses the event re-reads"), §S7 (plans are private by default),
§D21 (the mobile app is the consumer). This document pins the slice.

---

## Specs

**S1 — Realtime is a hint, never the state.** The event carries no ranking, no votes and no
counts: `vote.changed` with `{coRoute, coPlace}`. A client that misses it re-reads the plan,
which is already the contract's source of truth (S9 of the change).

**S2 — The channel is private, and subscribing is authorized by this API.** The channel is
`private-plan-<coRoute>`. A client subscribes only with a socket id and a signed auth response
issued here, after the API has decided the caller may watch that plan. A public channel would
let anyone holding a route id watch a private plan's activity.

**S3 — The subscription decision reuses the plan's visibility rules.** A member may subscribe.
An anonymous caller may not: the endpoint requires a bearer. For a `PUBLIC` plan any
authenticated user may. For a `PRIVATE` plan a non-member gets `404 not_found`, the same answer
every other private-plan read gives: the plan must not confirm that it exists.

**S4 — Realtime is optional and degrades; it never fails a write.** With no `SOKETI_URL` the
service runs with a publisher that does nothing and the vote route is unchanged. With the URL
configured, a publish failure is logged and the vote still answers `204`: the vote is the fact,
the event is a courtesy. A missing credential with the URL set fails startup by name — a
silently half-configured publisher is worse than none.

**S5 — The signing is the Pusher HTTP protocol, and it is provable without Soketi.** Events are
`POST {base}/apps/{app}/events` with `auth_key`, `auth_timestamp`, `auth_version=1.0`,
`body_md5` and `auth_signature = HMAC-SHA256(secret, "POST\n/apps/{app}/events\n" + sortedQuery)`.
The subscription response is `{"auth": "<key>:<signature>"}` with
`signature = HMAC-SHA256(secret, socket_id + ":" + channel)`. Both are computed in-process and
asserted against fixed vectors, so the suite needs no broker.

**S6 — Contract first.** `POST /v1/realtime/auth` and the four environment variables land in
`openapi.yaml`, `API.md`, `README.md` and `.env.example` before the handler is trusted: the
contract test fails otherwise. New error codes: none — `validation_failed`, `unauthorized`,
`not_found`, `permission_denied`, `internal_error` already cover it.

**S7 — Tests without services.** Signature vectors, a disabled publisher that is a no-op, a
publish failure that does not fail the vote, the four authorization outcomes of the auth
endpoint, and a malformed channel.

## Routes

| Method | Path | Auth | Notes |
|---|---|---|---|
| `POST` | `/v1/realtime/auth` | bearer | `{socketId, channel}` → `{auth}`; `422` on a channel that is not `private-plan-<uuid>` |
| `POST` | `/v1/plans/{coRoute}/places/{coPlace}/votes` | bearer | unchanged from the caller's side; now publishes after a successful write |

## Tasks

- T1 — `internal/realtime`: config, the Pusher client (publish + subscription signature), the
  disabled publisher, and their vectors. Test-first. **done** — the signature is pinned against
  an independently computed vector, not against itself.
- T2 — `internal/plans`: a reporter seam on the service, called after a successful vote,
  best-effort; a membership predicate the auth endpoint can ask. **done** — `VoteReporter` and
  `CanWatch`; a failing vote reports nothing.
- T3 — `internal/httpapi`: the auth handler, wiring in `cmd/api/main.go`, and the channel parser.
  **done** — the route is registered only when the broker is configured.
- T4 — Config: `SOKETI_URL`, `SOKETI_APP_ID`, `SOKETI_KEY`, `SOKETI_SECRET`, with the failure
  rule of S4. **done** — half a configuration aborts startup naming the variable.
- T5 — Contract: `openapi.yaml`, `API.md`, `README.md`, `.env.example`. **done**.
- T6 — Verify: suite and `internal/contract` green, plus a real publish against the running
  Soketi container and a subscription round-trip with a real client. **done** — see L2.

---

## Log

- L1 — Human authorization, verbatim: "vamos por las tablas y luego la propagacion de votos",
  then "toma el correcto" when asked to choose between a public channel with a minimal payload
  and a private channel with a subscription-auth endpoint. The private channel is the decision.

- L2 — T1–T6 done (2026-11-14). The risky half was never the HMAC arithmetic, so it was not
  tested against itself: the signature vectors were computed outside the package (independent
  Python) and the Go test pins those constants. Then the two things arithmetic cannot prove were
  proved against the real broker in the compose stack — our own client published to
  `private-plan-live-test` and Soketi accepted the signed request, and a real WebSocket client
  subscribed to that private channel with the same documented scheme and received the
  `vote.changed` event our client published. `docker-compose.yml` gained the four `SOKETI_*`
  values for the `api` service (the broker's default app credentials), so dev exercises the path
  instead of carrying an untested branch. The publish failure path is a log line and a `204`:
  the vote is stored before anything is published, and a member who cannot be notified has not
  voted less.
  Not exercised, and worth saying instead of implying: the WebSocket subscription was driven by
  a small Node script, not by the Go API's own client — Go has no WebSocket dependency here and
  adding one for a test would be the tail wagging the dog. The signature the script used is the
  one the Go client produces for the same inputs, pinned in the unit test.

- L3 — Independent verification (2026-11-14) found one blocker and four advisories; all of them
  are fixed in this branch, and the reasoning matters more than the diff.
  **B1 — the route was never absent.** `cmd/api` always passed a publisher, and it passed
  `realtime.Disabled()`, a non-nil interface. The router guard `opts.Realtime != nil` was
  therefore always true: with no broker configured `POST /v1/realtime/auth` existed and answered
  `500`, while `openapi.yaml`, `API.md`, `README.md`, `.env.example`, `docker-compose.yml` and the
  router's own comment all promised the route is not registered. Fix: absence is a nil publisher,
  built by `newRealtimeClient`, which returns nil when `SOKETI_URL` is empty. `Disabled()` and
  `ErrDisabled` are gone — a no-op implementation of an interface used as a presence flag is the
  defect itself. Two tests hold it: `cmd/api` asserts nil for an unconfigured broker and a client
  for a configured one, and `httpapi` asserts the route answers `404` when no publisher is wired.
  **A1 — a suspended PUBLIC plan stayed watchable.** `CanWatch` approximated the read door with
  role and visibility, and `Role` ignores `tx_status` while `Get` hides a suspended plan from
  non-members: the subscription answered `200` where the read answered `404`. Fix: the store owns
  a `Watchable` predicate that copies `Get`'s exactly, and the service delegates. Proven on the
  live database inside a rolled-back transaction: private-active stranger=false/member=true,
  public-active true/true, public-suspended false/true — the same answer `Get` gives.
  **A2 — a hanging broker held the vote open for the client timeout.** Publishing now happens off
  the request, with a context detached from it and bounded by the client's timeout, so the answer
  never waits and a slow broker cannot invite a retry that votes twice.
  **A3 — a broker behind a path prefix broke signing.** The signed path is now the broker's own
  path plus `/apps/<id>/events`, so the signature names the path the request uses.
  **A4 — a Spanish comment in an English-commented file.** Fixed.
  Left as is: **A5** (handlers accept unknown JSON fields while `openapi.yaml` declares
  `additionalProperties: false`) is pre-existing and project-wide, so it is not this slice's debt.
