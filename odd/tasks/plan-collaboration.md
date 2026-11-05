# plan-collaboration — P3a: plans, membership and the public/private door

Feature: plans a person can plan with friends. Visibility is an attribute of the plan, membership
is a relation, and joining a public plan grants the least privilege (D7/D8).
Branch: `feat/api-plan-collaboration` off `feat/api-place-ownership`.
Authority: `openspec/changes/social-discovery/spec.md` §S7, §S8, §S15, §S25; `design.md` §D7,
§D8, §D21. This document pins the slice.

Consumer: the mobile app (D21). `melia-admin` gets no screen from this slice.

## Facts established before writing (do not re-derive)

- `core.tb_route` exists with `co_user` (creator), `tx_name`, `tx_description`,
  `tx_status activatable_status`, timestamps, `dt_deleted_at`.
- `core.tb_route_place` already exists with the composite primary key `(co_route, co_place)` and
  `nu_order` — the ordered visit list is modelled, nothing to add.
- **`tb_route` is NOT in the point-in-time set.** Only four history tables exist (`place`,
  `menu_item`, `place_schedule`, `review`). S15 assumed otherwise; adding columns to `tb_route`
  therefore needs no history payload and no trigger replacement. Recorded as a correction.
- The live database has zero routes, so a backfill of owners is trivial but must still be written
  for any environment that has rows.

---

## Commit A — schema, plans and membership

**S1 — Enums are closed sets.** `public.plan_visibility` (`PRIVATE`, `PUBLIC`) and
`public.plan_role` (`OWNER`, `EDITOR`, `VIEWER`). Roles participate in authorization; a typo in
free text would be a silent privilege bug.

**S2 — The plan gains three attributes.** `core.tb_route`:
- `tx_visibility public.plan_visibility NOT NULL DEFAULT 'PRIVATE'` — the restricted value is the
  default, so a plan can never become public by omission;
- `dt_scheduled_at timestamp NULL` — a plan may not have a date yet, and "not defined" is a real
  state, never "today";
- `nu_party_size smallint NULL` with `CHECK (nu_party_size BETWEEN 1 AND 50)` — numeric, because
  "solo", "en pareja" and a 50-person festival are 1, 2 and 50; an enum would need a migration the
  first time a group is four.

**S3 — Membership is a relation with a role.** `core.tb_route_member (co_route, co_user, tx_role,
dt_created_at, dt_updated_at)` with the composite primary key `(co_route, co_user)`, no soft
delete, FK to `tb_route` and to `seg.tb_user`, index on `co_user` ("which plans am I in").
`tb_route.co_user` stays as the creator reference; ownership for authorization reads the member
row.

**S4 — The creator is OWNER in the same transaction.** A plan with no owner is invalid. If the
member insert fails, the plan insert must roll back with it.

**S5 — Self-join grants VIEWER.** `POST /v1/plans/{coRoute}/members` joins the caller. Allowed only
when `tx_visibility = 'PUBLIC'` and the plan is `ACTIVE`; otherwise `404` (a private plan must not
confirm it exists). The granted role is `VIEWER`, always: an open door that hands out `EDITOR` is
a plan anyone can rearrange and empty, and a festival is the worst case (D8).

**S6 — Reads are scoped, and a non-member sees only public plans.** `GET /v1/plans/{coRoute}` is
visible to members and, for `PUBLIC` plans, to any authenticated caller. Anything else is `404`,
never `403`.

**S7 — Mutations need EDITOR or OWNER.** `PATCH /v1/plans/{coRoute}` accepts name, description,
scheduled time and party size; `tx_visibility` is **OWNER only** (changing PRIVATE → PUBLIC touches
everyone already invited). A member with `VIEWER` gets `404` for the mutation, consistent with S6:
they can see the plan, and they still cannot change it — the distinction is 403 for "you may see
this but not do it" only when the row is already visible, so: visible + insufficient role → `403`;
invisible → `404`.

**S8 — Leaving and removal.** `DELETE /v1/plans/{coRoute}/members/{coUser}`: a caller may remove
themselves; an `OWNER` may remove anyone except themselves (a plan without an owner is invalid).
Removing a non-member is idempotent `204`.

**S9 — Suspension is moderation and immediate (S8 of the spec).** `tx_status` uses the existing
`activatable_status`. A plan that is `INACTIVE` answers `404` to non-owners and keeps read access
for its members so nobody loses their itinerary.

## Routes in commit A

| Method | Path | Who |
|---|---|---|
| `POST` | `/v1/plans` | bearer; creator becomes `OWNER` |
| `GET` | `/v1/plans/{coRoute}` | member, or anyone for a PUBLIC plan |
| `PATCH` | `/v1/plans/{coRoute}` | `EDITOR`/`OWNER`; visibility `OWNER` only |
| `POST` | `/v1/plans/{coRoute}/members` | bearer; self-join, PUBLIC only → `VIEWER` |
| `DELETE` | `/v1/plans/{coRoute}/members/{coUser}` | self, or `OWNER` |
| `GET` | `/v1/plans` | bearer; the plans the caller belongs to, paginated |

**S10 — Contract first, no new error codes.** Everything reuses `validation_failed`, `not_found`,
`permission_denied`, `unauthorized`, `internal_error`.

**S11 — Tests without services.** Creator is owner, self-join grants VIEWER (not EDITOR),
self-join on a PRIVATE plan is 404, a non-member reading a PRIVATE plan is 404, a VIEWER mutating
is 403, a non-owner changing visibility is 403, the owner cannot remove themselves, and a
VIEWER-readable plan is still mutable by an EDITOR.

## Commit B (next, after A is verified)

Places in a plan (`tb_route_place` already exists) and votes (`core.tb_route_place_vote` with
`nu_vote ∈ {-1,1}` and the composite primary key), with the ordered read: net votes descending,
ties by `nu_order`, then creation time. A vote from a non-member is rejected, not dropped. An
`OWNER` override of the order is recorded rather than silently fighting the votes.

---

## Log

- L1 — Human authorization, verbatim: "dale <3".
- L2 — Two facts corrected against the model before writing: `tb_route` is not in the
  point-in-time set (four history tables exist, none for routes), so S15's mirroring instruction
  does not apply; and `tb_route_place` with `nu_order` already exists, so the ordered itinerary
  needs no new table.
