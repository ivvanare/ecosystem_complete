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
- L4 — Live evidence (rebuilt image, existing database): the ledger went from `02..05` to
  `02..06`, so the incremental path applies only the new file. Ten permissions landed with
  their modules. Preflight from `http://localhost:5173` answered `204` with the exact
  `Allow-Origin`, `Allow-Credentials: true`, `Allow-Headers` including `X-CSRF-Token`,
  `Allow-Methods`, `Max-Age: 600` and `Vary: Origin`; a preflight from an unknown origin
  answered `404` with no CORS headers at all. One query error was mine, not the data's:
  `GROUP BY 1` on a concatenation containing `count(*)`.
- L5 — k6 suites added under `tests/k6/`: `smoke.js` (contract, writes nothing) and
  `signup.js` (write path plus Argon2id cost, writes one pending row and one queued mail
  per iteration). Both were validated with a real k6 binary (v2.3.0, run from /tmp, nothing
  installed system-wide).
  Live results: `smoke.js` green — 460/460 checks, `http_req_failed` 0%, p(95) 3.74 ms over
  220 requests. `signup.js` returned 404 on 151/151 requests because the running container
  predates the signup commit, which is the expected signal of a stale image and not a
  script defect.
  One defect found in the suite itself: `smoke.js` requests a 404 on purpose, and k6 counts
  non-2xx as failed requests, so the first run reported 45% failures with every check
  green. Fixed with `http.setResponseCallback(http.expectedStatuses(...))` and documented.
- L6 — After the rebuild, the signup chain was verified end to end from the host:
  `POST /v1/auth/signup` answered `202`, and Mailpit's API showed "Tu código de registro"
  addressed to that exact address with a 6-digit code in the body — so the API published,
  the worker consumed and SMTP delivered.
  `signup.js` initially failed its error-rate threshold at 10.59% with 302/302 checks green:
  the script sends one invalid request every ten iterations on purpose and k6 counts a 422
  as a failed request. Fixed with `expectedStatuses(..., 422)`; re-run reports 0% failed and
  p(95) 36.5 ms.
  The same run exposed a design flaw in the suite: `sleep(1)` per iteration with 5 VUs
  measures ~4 req/s, which is a smoke test, not load. `SLEEP` is now an env var and the
  README says plainly that `SLEEP=0` is the throughput measurement.
