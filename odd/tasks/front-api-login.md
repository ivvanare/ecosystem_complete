# Feature: front-api-login

Objective: connect `gui/admin` to the API's existing browser session transport (httpOnly refresh cookie + CSRF double-submit + 15m Bearer access token), so the panel can log in, survive a reload, and call protected endpoints through `crud-maker`'s `HttpTransport` without ever holding the refresh token in JS. Backend already implements the transport; the work is contract accuracy, one additive field, and the whole front wiring.
Branch: feat/front-api-login (repo root: melia) — base `master` at `8e59f4c`
Delivery strategy: ask-on-risk; slices ~400 authored lines, tests+code together; push/PR remain user decisions
Test runners: `go test ./...` inside `api/` (contract suite included) · `npx ng test admin --watch=false` and `npx ng test crud-maker --watch=false` inside `gui/admin` after `npx ng build crud-maker`
Base evidence: exploration handoff on auth/CORS/contract/front (path:line for every claim), recorded in Log L2

## Specs

- **S1** (verbatim, 2026-10-31): option chosen by the user — "Login end-to-end (CORS + refresh cookie + HttpTransport)" — described as: "Slice contrato-first: CORS_ALLOWED_ORIGINS con credenciales, cookie de refresh desde gui/admin, HttpTransport con baseUrl y el flujo login/logout. Desbloquea todo lo demás."
  - Acceptance: the panel logs in against `/v1/auth/login`, receives the refresh token only as an httpOnly cookie, calls a protected endpoint with `Authorization: Bearer`, and logs out; no refresh token ever readable from JS.

- **S2** (derived from S1 + `api/API.md:35-72`, `api/internal/httpapi/cookies.go:29-44`): the panel uses the **browser** session transport, never the mobile JSON-body one. The server picks it by `Origin` ∈ `CORS_ALLOWED_ORIGINS`; the panel must therefore send `Origin` implicitly (same-origin XHR from the SPA) and `withCredentials` so both cookies travel.
  - Acceptance: login response for the panel has no `refresh_token` in the body and does set `refresh_token` + `csrf_token` cookies; `/v1/auth/refresh` and `/v1/auth/logout` succeed with cookies alone plus the CSRF header.

- **S3** (derived from diagram rule 7 and `api/internal/session/refresh.go:110-121`): refresh rotates on every use and presenting an already-rotated token revokes the **whole family**. Concurrent 401s must therefore share one in-flight refresh; two parallel refreshes would log the user out by design.
  - Acceptance: N simultaneous requests failing with 401 produce exactly one `POST /v1/auth/refresh`; all N retry once and succeed; a second failure does not loop.

- **S4** (derived from `cookies.go:60-69` — the CSRF cookie's `Path=/v1/auth` makes it unreadable to a document served from the SPA root, and unreadable at all cross-host): the CSRF token must reach the panel without reading a path-scoped cookie. Additive field `csrf_token` on the token-pair response; the double-submit comparison server-side stays as it is.
  - Acceptance: `POST /v1/auth/login` and `/v1/auth/refresh` include `csrf_token` in the body for browser requests; the panel echoes it as `X-CSRF-Token`; a mismatched or missing header still yields `403 csrf_invalid`. Mobile clients that ignore the field keep working.

- **S5** (derived from the two contract drifts found in the map): `api/openapi.yaml` must describe what the handlers do.
  - Acceptance: `POST /v1/auth/logout` 204 documents the `Set-Cookie` clearing both cookies, and no longer declares `security: bearerAuth` (the route is registered without `RequireAuth` and logout revokes from cookie/body token, `router.go:78`, `auth.go:309-340`).

- **S6** (derived from S2 + S3): a reload of the panel restores the session without a login flash: an access token lives in memory only, so boot issues one silent `/v1/auth/refresh` and routes to the app on success, to `/login` on `refresh_invalid`.
  - Acceptance: reload with a valid refresh cookie lands authenticated; reload with none lands on `/login` and issues no error toast.

- **S7** (derived — `find` confirms no `src/environments/`, no injection token, no window config today): the API base URL is configuration, not a literal. One injection token/hook, so tests can point at a fake and the dev server at `http://localhost:9000`.
  - Acceptance: no hardcoded `localhost` or `/v1` literal in components or in the interceptor; changing the token alone repoints every call.

- **S8** (derived — existing behaviour the change must not break): `MemoryTransport`, the FAQ demo, the `crud-maker` public API and every existing auth endpoint stay as they are. `HttpTransport`'s constructor and its three tests (`http-transport.spec.ts:43-82`) keep passing unchanged; auth is layered in an interceptor, not by forking the engine (`transport.ts:9-17` stays the seam).

## Tasks

| ID | Spec | Route | Status | Commit | Evidence |
|----|------|-------|--------|--------|----------|
| T1 | S5 | inline | pending | — | openapi.yaml logout block; verify `go test ./internal/contract` |
| T2 | S4 | inline | pending | — | Go handler + test: browser response carries `csrf_token`, mobile shape unchanged; openapi/API.md updated |
| T3 | S2, S3, S7 | inline | pending | — | app.config providers, `API_URL` token, interceptor, `AuthService`; vitest: single-flight refresh, Bearer attach, no token in storage |
| T4 | S6, S2 | inline | pending | — | login route + guard + silent refresh on boot; spec covers both landing outcomes |
| T5 | S1, S8 | inline | pending | — | one real screen consuming `/v1/me` through `HttpTransport`; demo and crud-maker suites still green |
| T6 | all | inline | pending | — | per-`S#` verdict; `go test ./...`, both ng suites, `ng build` |

Writer trigger check: T1/T2 are small backend edits and T3–T5 depend on each other left to right (no two independent units heavier than a subagent start) → parent implements inline. Sandbox note: this session's cgroup caps pids at 64, so `go test -race`, `ng build` and `ng test` may die with "Resource temporarily unavailable"; any step blocked that way is reported as unverified here and proven in CI.

## Log

- **L1** (verbatim, user, 2026-10-31): "Login end-to-end (CORS + refresh cookie + HttpTransport)" — selected from the offered verticals, with the description quoted in S1.
- **L2** (evidence, exploration handoff): access token HS256/15m with claims `sub,iss,iat,exp,jti` only and no roles (`session.go:88-157`) → permissions come from `GET /v1/me` (`identity.go:56-84`). Refresh cookie `refresh_token` Path=/v1/auth, HttpOnly, Secure=`COOKIE_SECURE` (default true), SameSite=Lax, MaxAge=refresh TTL; readable `csrf_token` alongside (`cookies.go:16-70`). Transport chosen by trusted `Origin` (`cookies.go:41-44`). CORS echoes the exact allowlisted origin with credentials, allows `X-CSRF-Token`, preflight 204 before auth (`cors.go:17-59`). Contract test enforces route↔openapi↔API.md, every emitted error code documented and tested, every env var in README + `.env.example` (`contract_test.go:118-233`). Front today: no `provideHttpClient`, no interceptor, no environment mechanism; the demo runs on `MemoryTransport` (`app.config.ts:5-10`, `demo/faqs-demo.ts:42-44`).
- **L3** (decision, parent, 2026-10-31): S4 resolved as an additive body field instead of widening the CSRF cookie's `Path` to `/`. Rationale: a `Path=/` cookie is readable by any script on that host and still invisible to a panel on another host, so it fixes dev and not a real deploy; the body field works for both, needs no cookie-visibility assumption, and the server-side comparison is unchanged. Rejected alternative recorded here so it is a decision, not an omission.
- **L4** (decision, parent, 2026-10-31): access token in memory only, never `localStorage`/`sessionStorage`. Rationale: the refresh cookie is httpOnly by design; persisting the 15m access token adds XSS surface and buys nothing, since S6 already restores the session from the cookie.
- **L5** (open question, not acted on): diagram rule 10 (`database/diagram_base.txt:83-97`) states "no hay frontend web" as the reason codes are used instead of links, and says a web client is additive. The admin panel now exists, so that premise is stale. It affects registration/password flows, not this slice; flagged for the user rather than rewritten unilaterally.
- **L6** (constraint, recorded): `SameSite=Lax` works for same-site deployments (localhost, or sibling subdomains) but not across registrable domains; a cross-domain deploy needs `SameSite=None; Secure`. Out of scope here, noted so it is not discovered in production.
