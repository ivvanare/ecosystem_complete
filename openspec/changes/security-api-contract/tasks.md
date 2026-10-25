# Tasks: Security API Contract

Change: `openspec/changes/security-api-contract/`
Specs: `spec.md` (C1–C12) · Contract: `api/openapi.yaml`

## Tasks

| ID | Task | Specs | Route | Commit |
|----|------|-------|-------|--------|
| T1 | `api/openapi.yaml`: 19 operations (health, auth, identity, admin), 11 schemas, one error envelope | C1–C11 | inline | pending |
| T2 | Structural validation of the contract: refs resolve, operationIds unique, every error response uses `ErrorEnvelope`, path params declared, authenticated operations answer 401 | C1, C2 | inline | pending |
| T3 | Align the 404 handler with the envelope (`{"error":{"code","message"}}`) + test | C2 | inline | pending |
| T4 | Record the decisions a schema cannot express | C12 | inline | pending |
| T5 | Implementation of the module, in the slices below | C3–C10 | — | next change |

## Implementation order (next change)

Each slice is one work unit with its tests; the contract is already fixed.

1. Seeds of roles and permissions (`admin.access`, `place.*`, `review.moderate`, …) and the
   pgx repository layer. No endpoints.
2. Signup + verify (`tb_signup_request`), which introduces the first RabbitMQ publisher and
   a mail worker.
3. Login + access JWT + refresh rotation with family revocation.
4. Password reset/change, revoking all families.
5. RBAC middleware + `/v1/me`.
6. Email change.
7. Audit writes (`tb_security_event`) on every auth event.
8. Social login with go-oidc.

## Log

- L1 — User decision (verbatim): "Contrato de la API primero (Recomendado)" over starting
  the security module directly, because the admin panel in `gui/` is being built in a
  parallel session and needs the shapes before it writes code.
- L2 — Shapes derived from the model, not invented: `tb_user` (nullable name/email/password,
  `nu_failed_attempts`, `dt_locked_until`, `suspendable_status`), `tb_signup_request`
  (name+password required, immutable email, code hash, attempts, 10-minute expiry),
  `tb_refresh_token` (hash, family, device, revoked_at), `tb_user_token`
  (`PASSWORD_RESET|EMAIL_CHANGE`, target, attempts, used_at), RBAC bridges, and
  `tb_security_event` (append-only, no FK to user).
- L3 — Validation evidence: the YAML parses; 19 paths / 19 operations / 11 schemas /
  8 reusable responses; every `$ref` resolves; `operationId` unique; every path parameter
  declared; every authenticated operation answers 401; all 8 error responses reference
  `ErrorEnvelope`. One YAML defect found and fixed during validation: an unquoted
  `description` containing `: ` (the `Authorization: Bearer` sentence) broke the document.
- L4 — The only pre-existing endpoint that contradicted the contract was the 404 handler,
  which returned `{"error":"not found"}`. It now returns the envelope; the test asserts the
  code instead of the string. Nothing else in the codebase returns errors yet.
- L5 — Deliberately not decided: panel transport (bearer vs httpOnly cookie), rate-limit
  placement, token lifetimes as env vars, password strength policy. They are listed in C12
  so the implementation does not quietly pick one.
