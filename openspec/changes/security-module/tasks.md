# Tasks: Security Module

Specs: `openspec/changes/security-api-contract/spec.md` (C1–C14) · Contract: `api/openapi.yaml`

## Tasks

| ID | Slice | Contract | Route | Commit |
|----|-------|----------|-------|--------|
| T1 | CORS middleware: credentials on, exact origin echo, auth-free preflight, `Vary: Origin` | C13 | inline | pending |
| T2 | Seed catalogue: 10 permissions, 3 roles, role→permission links, idempotent on the model's partial uniques | C9 | inline | pending |
| T3 | Signup + verify (`tb_signup_request`), first RabbitMQ publisher + mail worker | C5 | — | next |
| T4 | Login with lockout + access JWT | C6, C3 | — | next |
| T5 | Refresh rotation with family revocation | C3 | — | next |
| T6 | Password reset/change revoking all families | C4 | — | next |
| T7 | RBAC middleware + `/v1/me` | C9 | — | next |
| T8 | Email change | C7 | — | next |
| T9 | Audit writes on every auth event | C10 | — | next |
| T10 | Social login with go-oidc | C8 | — | next |

## Log

- L1 — Slices 0 and 1 implemented: CORS and seed data. CORS is off when
  `CORS_ALLOWED_ORIGINS` is empty (a server must not invent an allowed origin), echoes the
  exact origin, enables credentials for the panel's httpOnly refresh cookie, and answers
  preflight without touching auth.
- L2 — Seeds follow the model: `ON CONFLICT` targets the partial unique indexes
  (`tx_code`/`tx_name` `WHERE dt_deleted_at IS NULL`) and the bridge primary keys, so
  re-running the file is a no-op. Roles are grants, not user types: `admin.access` is what
  opens the backoffice.
- L3 — The seed file is `database/init/06-seeds.sql`, which also proves the incremental
  path: an existing database with `02..05` in the ledger must apply only `06` on the next
  start, without touching the rest.
