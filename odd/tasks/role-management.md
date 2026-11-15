# role-management — authoring roles and their permissions from the API

Feature: the write half of RBAC. Until now the contract could read roles and assign them to a
user, and nothing could create a role or change which permissions it grants.
Branch: `feat/api-role-management` off `dev`.
Authority: `database/diagram_base.txt` §7–§11; `openspec/archive/security-module/spec.md`;
the seeds in `database/init/06-seeds.sql`, which already declare the intent
(`SUPERADMIN` "incluida la gestión de roles", `ADMIN` "no cambia roles ni permisos").

---

## Specs

**S1 — Contract first, and `role.manage` finally guards something.** Five routes in
`openapi.yaml` and `API.md` before the handlers: `POST /v1/admin/roles`,
`GET /v1/admin/roles/{coRole}`, `PATCH /v1/admin/roles/{coRole}`,
`DELETE /v1/admin/roles/{coRole}`, `PUT /v1/admin/roles/{coRole}/permissions`. Every one requires
`role.manage`, the permission the seeds created for exactly this and that today gates no route.
The two reads that already exist (`GET /v1/admin/roles`, `GET /v1/admin/permissions`) keep
`admin.access`, because the panel needs the catalogue to assign roles and that is `user.manage`
work, not role authoring.

**S2 — A role is a name, an optional description and a set of permission codes.** The name is
trimmed, non-empty, at most 60 characters, and unique among live roles: a duplicate is `422` with
field `name`, code `duplicate`. A permission code the catalogue does not carry is `422` with
field `permissions`, code `unknown` — never silently dropped.

**S3 — Editing is partial and obeys the same shapes.** `name`, `description` and `status`
(`ACTIVE`|`INACTIVE`) may each be omitted; an omitted field is left alone. A duplicate name is the
same `422` as on create.

**S4 — Permissions are replaced as a whole set.** `PUT .../permissions` takes
`{codes: [...]}` — **the key is required**: a body without it is `422 field: codes, code: required`,
because a missing key must never mean "strip the role". The composition is replaced in one
transaction and only the **difference** is written, so a permission that stays produces no history
row: recording a `DELETE` plus an `INSERT` for something nobody touched would make the audit say
the opposite of what happened. The response is `204`.

**S5 — A role with users is not deleted, it is reported.** `DELETE` retires the role logically
(which already revokes its capabilities on the next request, because `Access` filters deleted
roles). If any **live** user still holds it, the answer is `409 role_in_use` with the count in
`details`, so a human decides before anyone loses access. The count and the retire share one
transaction, and the role row is locked `FOR UPDATE` — which also serializes against a concurrent
assignment, so nobody can slip in between. A soft-deleted user's assignment is inert and is not
counted; the row stays for the audit. Physical deletion is never offered.

**S6 — The system role is protected, because the alternative is a lockout.** `SUPERADMIN` cannot
be renamed, cannot be deleted, and its permission set must always contain `role.manage`. Any
attempt is `409 role_protected`. There is no path through this API that removes the last way back
into role management.

**S7 — Every write is attributable, and granting capability needs having it.** All writes run with
the actor set (`set_config('app.current_user_id')`), so `seg.tb_role_permission_history` records who
changed the composition and when. The role row itself has no history table and none is invented
here: the point-in-time set is fixed by ledger 07.
Assigning a role that carries `role.manage` requires holding it (`403` otherwise): without that
guard the seeded `ADMIN` — deliberately given `admin.access` and not `role.manage` — grants itself
`SUPERADMIN` in one request and the separation is decorative.

**S8 — Tests without services.** The validation rules are pure functions; the contract test holds
the routes and codes to the documentation; the SQL is proven on the live database inside a
transaction that rolls back, then discarded.

## Routes

| Method | Path | Guard | Notes |
|---|---|---|---|
| `POST` | `/v1/admin/roles` | `role.manage` | `{name, description?, permissions?}` → `201` |
| `GET` | `/v1/admin/roles/{coRole}` | `role.manage` | the role with its permission codes |
| `PATCH` | `/v1/admin/roles/{coRole}` | `role.manage` | `{name?, description?, status?}` → `200` |
| `DELETE` | `/v1/admin/roles/{coRole}` | `role.manage` | `204`, or `409 role_in_use` / `role_protected` |
| `PUT` | `/v1/admin/roles/{coRole}/permissions` | `role.manage` | `{codes: [...]}` → `204` |

New error codes: `role_in_use` (409) and `role_protected` (409). A duplicate name and an unknown
permission are field-level validation, so the catalogue grows one field code: `duplicate`.

## Tasks

- T1 — `internal/identity`: types, sentinels, the service rules of S2–S6.
- T2 — `internal/identity/admin_postgres.go`: create, get, update, retire and replace-permissions
  inside `withActor`, with the unique-name violation mapped to the field error.
- T3 — `internal/httpapi`: the five handlers and their wiring behind `role.manage`.
- T4 — Contract: `openapi.yaml`, `API.md`, the two new codes and the field code.
- T5 — Tests: validation rules, protection rules, and the HTTP mapping of each outcome.
- T6 — Verify: suite and `internal/contract` green, and the SQL proven on the live database in a
  rolled-back transaction (create, replace, retire, the in-use refusal, the SUPERADMIN guards).

---

## Log

- L1 — Human finding, verbatim: "Revisé el contrato y el backend en melia: hoy no existen endpoints
  para crear, editar o borrar roles, ni para modificar qué permisos componen un rol." Confirmed
  against `router.go`: only `GET /v1/admin/roles`, `GET /v1/admin/permissions` and
  `PATCH /v1/admin/users/{userId}/roles` exist, all under `admin.access`; `role.manage` is seeded
  and gates nothing.
- L2 — Decision asked and answered: deleting a role that users still hold is refused with the
  count, not applied silently. Recorded as S5.

- L3 — Implemented (2026-11-14). Contract first: the two new codes (`role_in_use`,
  `role_protected`) and the field code (`duplicate`) landed in `API.md` before the handlers, which
  is also what made the contract test demand the routes and then their tests.
  `identity.RoleStore` is a separate seam from `AdminStore`, and `httpapi.Options.RoleAdmin` a
  separate interface from `AdminReader`: authoring a role is a different capability from assigning
  one, and the wiring says so instead of hiding it behind `admin.access`.
  The guards are in the service, not the transport: renaming `SUPERADMIN`, deactivating it,
  retiring it, or replacing its set without `role.manage` all answer `409 role_protected` before
  any write. Proven by mutation: with the guard conditions neutralised, the lockout test fails on
  the first door (`rename: err = <nil>`).
  The store SQL was proven on the live database inside a transaction that rolled back: create plus
  composition produce two open intervals carrying the actor; replacing the set closes the removed
  permission's interval and opens a new one; the in-use count query answers; retiring the role
  leaves zero live rows and zero grants, which is the predicate `Access` uses. The transaction was
  rolled back and the three seeded roles are untouched.
  One limit stated rather than hidden: a role's own row (name, description, status) has no history
  table — the point-in-time set is fixed by ledger 07 — so "who renamed a role" lives in the
  request log, not in the database. `L2` of the change records why inventing a seventh history
  table was not this slice's decision to make.
