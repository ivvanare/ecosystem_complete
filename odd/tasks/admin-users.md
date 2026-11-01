# Feature: admin-users

Objective: make the users screen able to tell panel users apart from app users and to act on them — scope filter, edit of name/status, invitation by code, and a password-reset trigger — without introducing a second way to say "admin".
Branches: `feat/admin-users` (melia) from `feat/audit-filters`
Test runners: `go test ./internal/httpapi/ ./internal/identity/ ./internal/contract/` inside `api/`; `npx ng test admin --watch=false` and `npx ng build` inside `gui/admin`

## Specs

- **S1** (verbatim, user, L1): "en usuarios faltaria la creacion, edicion cambio de clave para los usuarios administrativos, queda pendiente como diferenciar los usuarios del admin y de la app cliente que serian los usuarios finales del app, los que visitarna lures, no los que lo administraran"
- **S2** (verbatim answer, user, L2): "Modelo canonico (recomendado)" — one user entity; the difference is a capability, never a stored type.
  - The rule it preserves, verbatim from `database/diagram_base.txt:54-58`: "tb_user es LA tabla base de usuarios: los de la app y los del backoffice son la misma entidad […] Un usuario entra al backoffice porque un rol activo le otorga `admin.access`, no porque su registro sea 'de tipo admin'."
- **S3** (derived): `GET /v1/admin/users` gains `scope=admin|app|none`, resolved from the roles' permissions (`admin.access`), never from a column.
- **S4** (derived): `PATCH /v1/admin/users/{id}` edits `name` and `status` only. The email is never edited by an administrator: `diagram_base.txt:74` proves the address before the row exists, so an admin-side change would skip verification.
- **S5** (derived): an administrator never sets another account's password. Creation is an **invitation** that sends the signup code to the person, and the reset path sends a reset code; both keep email ownership with the user.
- **S6** (constraint, existing suites): every new route lands in `api/openapi.yaml` and `api/API.md` in the same commit, and the contract suite must stay green.

## Tasks

| ID | Spec | Status | Evidence |
|----|------|--------|----------|
| T1 | S3, S6 | in progress | API: `scope` filter on the users list |
| T2 | S4, S6 | pending | API: `PATCH /v1/admin/users/{id}` for name and status, audited |
| T3 | S5, S6 | pending | API: invitation that sends the signup code |
| T4 | S5 | pending | API: reset-code trigger for one user |
| T5 | S3, S4 | pending | SPA: scope control in the toolbar + edit dialog |
| T6 | S5 | pending | SPA: invite action and reset trigger |
| T7 | all | pending | suites and builds green |

## Log

- **L1** (verbatim, user, 2026-11-01): "en usuarios faltaria la creacion, edicion cambio de clave para los usuarios administrativos, queda pendiente como diferenciar los usuarios del admin y de la app cliente que serian los usuarios finales del app, los que visitarna lures, no los que lo administraran"
- **L2** (verbatim answer, user, 2026-11-01): "Modelo canonico (recomendado)" — one user entity, capability instead of type; rejected alternatives recorded: `tx_type` on `tb_user` and a separate admin table.
- **L3** (finding, parent, 2026-11-01): the list has no way to narrow, which is why it "does not distinguish" today — `GET /v1/admin/users` accepts `page`, `limit`, `q` and `status` only, and its SQL has no join to roles. The panel's edit/disable/password gaps are separate: no `PATCH` for the user record, no admin-side invitation, and no reset trigger; passwords are self-service by design (`/v1/auth/password/change` and the `forgot`/`reset` code pair).

- **L4** (incident, 2026-11-01): the `api` session that was going to take the backend died without leaving anything — `dev` clean, no commits, no branches. I verified that before taking the work, and moved it to its own branch `feat/api-admin-users` so the two sides stay separate per `AGENTS.md`. Its last message also turned out to be a misdelivery for the `/database` session, so there was never a real dependency on new domain models.
- **L5** (delivered, 2026-11-01): the whole feature landed. API: `scope=admin|app|none` resolved through `user_role → role_permission → admin.access` (no user type column, as the model demands), `PATCH /v1/admin/users/{id}` for name and status with an `admin.user.updated` audit event, `POST /v1/admin/users/invitations` (queues a message, creates nothing, always 202) and `POST /v1/admin/users/{id}/password-reset` (the code goes to the owner). SPA: scope control in the toolbar, Edit dialog (name/status, never the email), Invite dialog projected into the adapter's header slot, and a per-row Send reset with its own state. Verified: `go vet` plus 14 packages green with 10 new handler tests; curl against the rebuilt container (scope 1/23/23, invalid scope 422, PATCH 200 and audited, invitation 202 with the mail in Mailpit, reset 202 with its mail, unknown id 404); SPA **19 files / 92 tests** and adapter **35 tests**.
- **L6** (open, 2026-11-01): two dispositions pending, both the human's. The invitation email is written in English while the existing password emails are in Spanish, so the mailbox mixes languages. And nothing is pushed: six branches in `melia` (`feat/admin-dashboard-shell` → `feat/admin-crud-screens` → `feat/dark-theme` → `feat/audit-filters` → `feat/admin-users`, plus `feat/api-admin-users` off `dev`) and one in `crud-maker`.

- **L7** (registration, 2026-11-01): the invitation email promised a flow that did not exist — it pointed at the panel root, which offers no signup, and no code was ever sent. The human reopened the earlier "no registro" decision, so `/registro` now exists: two steps (name/email/password, then the six-digit code), backed by `AuthService.startSignup` / `verifySignup`. The invitation text moved to `<origin>/registro` and explains the code (`77bbf05`), with a Go test pinning the link and the wording. Verified live against the rebuilt container: the mail arrives with the right link, `POST /auth/signup` answers 202, the code read from Mailpit verifies 201 and opens a session, and the fresh account gets **403** on `/v1/admin/users` — registering grants nothing, exactly as the model says. Test data left behind: `pedrito@melia.test`, no role.
