# device-notifications — where a push can go, and the seam that sends it

Feature: device registration (`seg.tb_device_token`, `POST /v1/devices`,
`DELETE /v1/devices/{coDevice}`) and the push dispatcher behind the existing notification kinds.
Branch: `feat/api-device-tokens` (P1) and `feat/api-push-dispatch` (P2) off `dev`.
Test runner: `/usr/local/go/bin/go test ./...` inside `api/` (never needs PostgreSQL).

Why: push was the one row of the app-readiness table that stayed open. There was no table, no
route and no sender — only `core.tb_notification_optout` and one email producer.

Scope, agreed with the user: build the contract and the seam **against the interface**, verified
with a fake sender, and plug the real provider when credentials exist. Building an FCM client
nobody can exercise would be an untested guess, and an untested sender is worse than none.

Out of scope until credentials exist: the FCM/APNs `Sender` implementation, and iOS delivery
(which needs an APNs key uploaded to Firebase — an administrative cost, not a technical one).

---

## Specs

**S1 — The token is a device, not a session.** It survives logins, rotates without being asked,
and registering a token another account already holds **reassigns** it. A phone that changes
hands must be reachable by whoever is holding it now; leaving it bound to the previous account
would deliver their notifications to a stranger.

**S2 — The token is stored in the clear.** A refresh token is *verified*, so hashing loses
nothing. A push token is *used*: the sender hands it to the provider verbatim, and hashing it
would make it useless for the only thing it is for.

**S3 — Partial unique.** The table soft-deletes (uninstall, logout with revocation, provider
rejection), so the unique on the token is partial: a plain one would burn the value forever and
a retired token could never be registered again.

**S4 — Registration is idempotent and authenticated.** Same token twice is one row, updated.
No bearer is `401`: a push has an owner, and an unowned token is a message with nobody to
address. `platform` is a closed set (`ANDROID`, `IOS`); anything else is `422`.

**S5 — Removal is scoped in the WHERE clause.** Checking ownership in a prior `SELECT` and
deleting after is a race, and it also confirms that an identifier exists. Absent, already
removed and someone else's all answer `404`.

**S6 — The dispatcher never fails its caller.** `push.Send` returns nothing: the action that
triggered the notification already happened, and undoing it because a courtesy did not arrive
would be worse than the courtesy. Preferences, devices and provider failures are log lines.

**S7 — Opt-out is checked first.** A kind the person disabled is not sent, and nothing is read
afterwards.

**S8 — No provider is a valid configuration.** A nil sender makes `Send` a no-op without logging
an error on every event. The token is stored and unused, which is harmless; a route that accepts
and fails is not.

**S9 — The ledger proves itself from zero.** The new file applies on an empty database and the
drift check passes there, including the rule that every column carries a `COMMENT`.

**S10 — Contract first.** `openapi.yaml`, `API.md` and the contract tests carry both routes.

---

## Tasks

| ID | What | Specs | Route | Status |
|----|------|-------|-------|--------|
| T1 | `33-device-token.sql`: table, partial unique, indexes, comments | S1–S3, S9 | inline | done |
| T2 | `verify_base_model.sql`: table in the inventory | S9 | inline | done |
| T3 | `internal/device`: validation, upsert by token, owner-scoped removal | S1, S4, S5 | inline | done |
| T4 | Routes + contract + guide | S10 | inline | done |
| T5 | `internal/push`: `Sender`, dispatcher, best-effort semantics | S6–S8 | inline | done |
| T6 | Wire the dispatcher into `notifications.InvitationReceived` | S6, S7 | inline | done |
| T7 | Tests and live verification | S4, S5, S9 | inline | done |

---

## Log

**L1** — User, verbatim:

> si dale con la recomendacion

Approving the interface-first path for push: contract and table now, provider later.

**L2** — Evidence gathered before writing (2026-11-15):
`core.tb_notification_optout` exists (30-notifications.sql) and documents that "delivery is
email … push waits for the mobile client". `notifications.Service.InvitationReceived` is the only
producer, and it already skips a disabled kind and never fails the invitation. There is no device
table, no `fcm`/`apns` reference anywhere in `api/internal`.

**L3** — A real omission caught by the drift check:
the first version of `33-device-token.sql` left `co_device`, `dt_created_at` and `dt_updated_at`
without a `COMMENT`, and `verify_base_model.sql` failed with `S24 FAIL: columns without COMMENT`.
Fixed before the file was ever applied to the live database. This is the check earning its keep:
the table looked complete and was not.

**L4** — Verification (2026-11-15):

- The whole ledger applied from zero into a throwaway database — **32 files**, none failing —
  and `verify_base_model.sql` passes there with `-v expected_db=…`, no `FAIL`, no `ERROR`.
  Note: the files carry `\connect melia`, which psql honours and the service's loader strips, so
  a manual run against a scratch database must filter those lines or it will write into `melia`.
- The live database received the file through the service: `schema file applied
  file=33-device-token.sql`.
- Over HTTP, with a real bearer: register `201` (token trimmed, platform uppercased), register
  again → **same `coDevice`** and one live row, delete `204`, delete again `404`, no bearer
  `401`, `platform: WINDOWS` → `422 {field: platform, code: invalid}`.
- Reassignment proved in a rolled-back transaction: the same token inserted for a second user
  leaves **one** row and the owner is the second account.
- `go test ./...` green — 33 packages, contract included.
