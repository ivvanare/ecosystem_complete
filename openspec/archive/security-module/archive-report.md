# Archive Report: security-module

## Change Summary

**What was done**: The full security surface of the API over the `seg` schema: signup with an
emailed six-digit code, verification with auto-login, login with lockout and timing equalisation,
rotating refresh tokens with family revocation on reuse, logout, the panel's httpOnly cookie
transport with CSRF, password reset and change, email change, RBAC (user → role → permission) with
the admin API, social login, and an append-only audit log.
**Why**: nothing else can be built on an identity that is not proven, and the security design was
already specified in the diagram (§7–§11) before the code existed.
**How**: one package per concern behind narrow interfaces (`signup`, `login`, `session`,
`password`, `emailchange`, `identity`, `security`, `social`), with passwords in Argon2id and every
auth event recorded.

## Delivered

- `seg.tb_user`, `tb_refresh_token`, `tb_user_token`, `tb_signup_request`, `tb_user_identity`,
  `tb_role`, `tb_permission`, `tb_user_role`, `tb_role_permission`, `tb_security_event` and their
  ledger files, plus the history tables later added for roles.
- The packages above, their suite, and the routes documented in `openapi.yaml` and `API.md`.
- The protections the failures taught: lockout with reset, refresh-token rotation with reuse
  detection, the CSRF token in the token pair, and a security event for every auth transition.

## Verification

`go test ./...` green with no services: every store is an interface and the suite passes fakes.
Behaviour was exercised end to end against the running stack during the change (signup code in
Mailpit, lockout, rotation, reuse revocation), and the contract test holds the documented error
codes to their tests.
