# place-ownership — the places backoffice

Feature: the admin surface for places — create, read, update and retire a place, with an
ownership `Viewer` that decides who may see and change what.
Branch: `feat/api-place-ownership` off `feat/api-social-graph`.
Authority: `openspec/changes/social-discovery/design.md` §D21 (the admin panel administers) and
the decisions recorded here; `database/diagram_base.txt` for `core.tb_place`.

Why this slice exists: the SPA session reported that the Places screen is the last thing the
panel is missing, and the model already had `co_owner_user` (NULL for backoffice-curated rows)
without any route that honoured it.

---

## Specs

**S1 — The Viewer has two flags, not one.** `Viewer{UserID string, ManageAll bool}`.
`ManageAll` is true when the caller holds `admin.access`. It is resolved once, at the transport,
from the existing `Authorizer` — never from the request body and never from a claim in the
token.

**S2 — Reading is scoped by ownership.** `GET /v1/admin/places` requires `place.update`.
- `ManageAll` → every place, including soft-deleted ones when `status=DELETED` is asked for.
- Otherwise → only rows with `co_owner_user = <caller>`.
- Curated places (`co_owner_user IS NULL`) are therefore invisible to a non-admin, which is the
  point: they belong to the backoffice, not to an editor.
- Filters: `q` (name), `status` (`ACTIVE`|`INACTIVE`|`DELETED`), `categoryId`, `country`,
  `region`, paginated, newest first.

**S3 — Creating is backoffice-only.** `POST /v1/admin/places` requires `admin.access`, and the
body may set `coOwnerUser`. A user with `place.create` but without `admin.access` gets `403`:
the catalogue is curated, and a self-service claim flow is a separate decision nobody took.

**S4 — Updating follows the same scope as reading.** `PATCH /v1/admin/places/{coPlace}`
requires `place.update`; the row must be visible to the caller under S2 or the answer is `404`
— not `403` — because a caller who may not see a row must not learn that it exists.

**S5 — Retiring is logical.** `DELETE /v1/admin/places/{coPlace}` requires `place.delete` and
sets `dt_deleted_at`; it never removes the row. A retired place is `404` on every public read
path (discovery, reviews, check-ins), which is already the behaviour those paths implement.

**S6 — Writes announce the actor.** `tb_place` is in the point-in-time set, so every write runs
`set_config('app.current_user_id', <caller>, true)` as its first statement and lets the existing
trigger record `co_actor_user`. The service never writes a history row.

**S7 — Validation reuses the model's shapes.** `tx_country` `^[A-Z]{2}$`, `tx_region`
`^[A-Z]{2}-[A-Z0-9]{1,3}$` when present, coordinates within ±90/±180, `co_place_category` must
exist and be live, `tx_timezone` non-empty and shaped like an IANA name. Failures are `422
validation_failed` with the closed field vocabulary; an unknown category is field
`coPlaceCategory` code `unknown`.

**S8 — Contract first, no new error codes.** Routes go into `openapi.yaml` (English) and
`API.md` (Spanish) before the handlers. Everything reuses `validation_failed`, `not_found`,
`permission_denied`, `unauthorized`, `internal_error`.

## Routes

| Method | Path | Gate | Notes |
|---|---|---|---|
| `GET` | `/v1/admin/places` | `place.update` | Scoped by S2, paginated |
| `POST` | `/v1/admin/places` | `admin.access` | `coOwnerUser` optional |
| `PATCH` | `/v1/admin/places/{coPlace}` | `place.update` | Scope of S4; partial body |
| `DELETE` | `/v1/admin/places/{coPlace}` | `place.delete` | Logical (S5) |

## Tasks

- T1 — `internal/placeadmin`: types, store (pgx) and service with the ownership rules.
- T2 — `internal/httpapi`: four handlers, the `Viewer` resolved from `Authorizer`, wiring in
  `cmd/api`.
- T3 — tests: owner sees only own, admin sees all, curated invisible to an editor, `404` instead
  of `403` on an invisible row, create gated by `admin.access`, logic delete hides the place
  from discovery, and validation of the shapes.
- T4 — `openapi.yaml` + `API.md`.
- T5 — verify: suite green without services, contract green, and an HTTP run with an admin token
  and an editor token showing the two different scopes.

---

## Log

- L1 — Human decision, verbatim: "primero el ownership de lugares para no dejar a la SPA
  esperando y luego p3a".
- L2 — Human decisions on the two open questions: the Viewer is "el dueño ve solo lo suyo"
  (S2), and "solo admin crea y asigna dueño" (S3). Curated places with a NULL owner stay with
  the backoffice, which is what the schema comment always said.
