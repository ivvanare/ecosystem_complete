# Tasks: Security Module

Specs: `openspec/changes/security-api-contract/spec.md` (C1–C14) · Contract: `api/openapi.yaml`

## Tasks

| ID | Slice | Contract | Route | Commit |
|----|-------|----------|-------|--------|
| T1 | CORS middleware: credentials on, exact origin echo, auth-free preflight, `Vary: Origin` | C13 | inline | pending |
| T2 | Seed catalogue: 10 permissions, 3 roles, role→permission links, idempotent on the model's partial uniques | C9 | inline | pending |
| T3 | Signup + verify (`tb_signup_request`), first RabbitMQ publisher + mail worker | C5 | inline | 35948ad + this |
| T4 | Login with lockout + audit | C6, C10 | inline | this |
| T4a | Session core: HS256 access token, rotating refresh family | C3 | inline | this |
| T5 | Refresh rotation with family revocation + logout | C3 | inline | this |
| T6 | Password reset/change revoking all families | C4 | — | next |
| T6a | Panel cookie + CSRF double submit | C3, C14 | inline | this |
| T7 | RBAC middleware + `/v1/me` + admin catalogues | C9 | inline | this |
| T8 | Email change | C7 | — | next |
| T8a | Admin users list, role assignment, audit read, first-admin bootstrap | C9, C10 | inline | this |
| T9 | Audit writes on every auth event | C10 | inline (auth events) | this |
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
- L7 — Session core and `POST /v1/auth/signup/verify`.
  Decision recorded: the access JWT carries `sub`/`iat`/`exp`/`jti` and nothing else.
  Embedding roles would keep a revoked role alive for up to 15 minutes, and the model
  requires capability to come from live RBAC; `/v1/me` resolves permissions per request.
  Verification holds the pending row with `FOR UPDATE`, so two concurrent attempts cannot
  consume one code. A wrong code commits the incremented counter (otherwise the attempt is
  free) and answers the same `400 invalid_code` as "no pending registration" and "expired":
  distinguishing them would confirm which addresses are mid-registration.
  `JWT_SECRET` is now required at startup with a 32-character floor; `ACCESS_TOKEN_TTL` and
  `REFRESH_TOKEN_TTL` default to 15m and 720h.
  Two defects found while wiring: `Parse` validated against the wall clock instead of the
  issuer's, which made expiry untestable; and the verify route was registered inside the
  `Signup != nil` branch, so a broker outage would have removed verification too — the
  opposite of the intent, since a user holding a code must still be able to finish.
  Not done yet: the panel's httpOnly refresh cookie (C3/C14). It belongs with refresh and
  logout, where CSRF applies; today the pair is returned in the body for every client.
- L8 — Login with lockout, plus the audit trace the user asked for on auto-login.
  `POST /v1/auth/login`: an unknown address, a wrong password and an inactive account all
  answer `401 invalid_credentials`; a miss still runs an Argon2id verification against a
  dummy hash so timing cannot enumerate accounts. At the threshold the account locks and
  answers `423` with `Retry-After`; success resets the counter.
  Audit: `seg.tb_security_event` now receives `auth.signup.verified`,
  `auth.session.created`, `auth.login.succeeded`, `auth.login.failed`,
  `auth.login.locked` and `auth.signup.code_rejected`. The email is stored as an HMAC with a
  subkey derived from `JWT_SECRET` (never the signing key itself), and an audit failure is
  logged without failing the request.
  Lockout knobs are configurable: `LOGIN_MAX_ATTEMPTS` (5) and `LOGIN_LOCK_DURATION` (15m).
  Still open: the panel's httpOnly cookie (C3/C14) belongs with refresh/logout.
- L9 — `auth-flow.js`: the whole auth surface as one k6 journey, with the 6-digit code read
  from Mailpit's API instead of hard-coded — the only way to exercise verify end to end from
  a load-testing tool. 53 checks across health, signup validation, verify (malformed, wrong,
  unknown, real), repeat signup, consumed code, login success/failure, lockout, the 404
  envelope and CORS.
  Validated against the running stack: 19 checks passed up to and including "code extracted
  from mail"; everything from `verify` on returned 404 because the container predates those
  commits. The script is correct to the edge of the deployed image.
  Also corrected a real mismatch: `CORS_ALLOWED_ORIGINS` allowed `http://localhost:5173`
  (Vite), but the panel is an Angular SPA whose `ng serve` listens on 4200. With the wrong
  origin every browser call would fail the preflight, and nothing in the API logs would say
  why.
- L10 — First full run of `auth-flow.js` against the rebuilt stack: 52/53. The single failure
  was the suite's own defect: `waitForMail` returned the first message for the address, and
  that address already held the signup code, so the "account already exists" check read the
  stale mail and never waited. `waitForMail` now takes a predicate. That also means the
  anti-enumeration mail was previously unverified; after the fix it is proven to arrive.
  `http_req_failed` reported 51% because the journey requests 400/401/404/422/423 on
  purpose; those are now declared expected, leaving the metric meaningful.
  Final: 53/53 checks, 0% failed requests, p(95) 27ms.
- L11 — Refresh rotation and logout.
  `POST /v1/auth/refresh` revokes the presented token in the same transaction that inserts
  its successor, inside the same family, so there is no window where both are live.
  Presenting an already-revoked token revokes the **whole family** and answers
  `401 refresh_reused`; an unknown or expired token answers `401 refresh_invalid` without
  touching anything. Client metadata (device, user agent) is carried over the rotation; the
  IP comes from the current request.
  `POST /v1/auth/logout` revokes the family and is idempotent: an unknown token still
  answers `204`, because an error would confirm whether it existed.
  Both are exercised in `auth-flow.js`: rotation, replay, family death after replay, unknown
  and missing tokens, and logout followed by a refresh that must fail.
  Still open: the panel's httpOnly cookie and CSRF. The handlers accept the token in the body
  only, which is right for mobile and wrong for the SPA — it is the last piece of C3/C14.
- L12 — The panel's httpOnly cookie and CSRF, closing C3/C14.
  A client is treated as a browser when it sends an `Origin` that `CORS_ALLOWED_ORIGINS`
  already trusts — the same list that decides whether the browser may call at all, so there
  is one source of truth. Such a client receives `refresh_token` as an httpOnly cookie scoped
  to `/v1/auth` and **never** in the body; the mobile app keeps receiving it in the body.
  Cookie-sourced refresh and logout require `X-CSRF-Token` matching the readable
  `csrf_token` cookie, and answer `403 csrf_invalid` otherwise; body tokens need no CSRF
  because nothing attaches them automatically. Logout expires both cookies.
  `COOKIE_SECURE` defaults to true and compose sets false for local http.
  The k6 journey gained ten checks for this path, using k6's cookie jar.
  One defect caught by the tests: the login handler still used the old body writer, so the
  first browser login returned the refresh token in the body despite the new helper existing.
- L13 — Two defects in the k6 journey, both mine, found on the first run of the cookie group:
  `csrfCookieName` was used but not imported (ReferenceError), and
  `http.cookieJar().cookiesForURL(BASE_URL)` queries with path `/`, while the session cookies
  are scoped to `/v1/auth` — the jar returns nothing and the check reads as "cookie not set"
  even when it is. Fixed by importing the constant and querying a URL inside the cookie path.
  Result: 77/77 checks, 0% failed requests, p(95) 25ms.
- L14 — `/v1/me`, the RBAC guard and the two catalogue reads.
  `GET /v1/me` merges the profile with the live roles and permissions; the panel authorizes
  on `permissions`, never on a role name. `RequireAuth` parses the bearer token and puts the
  subject in the context; `RequirePermission("admin.access")` re-reads capability from the
  database on every request, so a disabled role stops working immediately instead of when the
  token expires. `GET /v1/admin/roles` and `/v1/admin/permissions` sit behind it.
  A valid token whose account was deleted answers `401`, not `404`.
  Queries count only ACTIVE roles and ACTIVE permissions with no soft delete, matching the
  model's lifecycle rules.
  Covered by Go tests (allow, deny, lookup failure, missing user, deleted account) and by nine
  k6 checks. The **allowed** admin path is only covered by Go tests: it needs a role
  assignment and no endpoint exposes one yet.
- L15 — Administration surface: `GET /v1/admin/users`, `PATCH /v1/admin/users/{id}/roles`,
  `GET /v1/admin/audit-events`, plus `cmd/bootstrap` for the first administrator.
  Role assignment is a **replace** inside one transaction: the bridge table is rewritten, so
  a role removed in the request is really gone, and an unknown or inactive name aborts the
  whole change with `422` instead of half-applying capability. Every change is audited as
  `rbac.roles.changed` naming both actor and subject; a rejected change writes nothing.
  Pagination follows the contract (1-based, default 20, cap 100) and the response carries
  `page`, `limit` and `total`.
  The bootstrap command exists because roles cannot be granted before someone holds
  `admin.access`. It replaces the role set of an existing, verified user.
  Open risk, not invented into the contract: nothing stops an administrator from removing
  their own `admin.access` and locking themselves out. A guard would be a product decision
  (409? refuse only self-demotion? require a second admin?), so it is recorded here instead
  of being guessed.
