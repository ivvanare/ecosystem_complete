# Spec: Security API Contract

Source of truth for shapes: `api/openapi.yaml`. This file records the rules a schema
cannot express. Quotes are verbatim from `database/diagram_base.txt`, which remains the
authority for the data model.

## Specs

### C1 — One contract, versioned in the path

`api/openapi.yaml` is the single source of truth for the HTTP surface. All business
endpoints live under `/v1`; `/health/*` is unversioned because it is infrastructure.
Adding a breaking field means a new version, not a silent change.

### C2 — One error envelope, stable codes, no enumeration

Every error — including 404 — is:

```json
{"error": {"code": "invalid_credentials", "message": "Email or password is incorrect."}}
```

`code` is the machine contract (`^[a-z][a-z0-9_]*$`); `message` is text safe to show and
may change. `details` is optional and structured (e.g. `{"fields":[...]}`).

Two flows must never reveal whether an account exists:

> "NUNCA guarda el email en claro de un intento fallido (sería un oráculo de
> enumeración de usuarios), guarda un HMAC"

- `POST /v1/auth/signup` and `POST /v1/auth/password/forgot` answer `202` regardless.
- `POST /v1/auth/login` answers `401 invalid_credentials` for both an unknown email and a
  wrong password.

### C3 — Two tokens, different natures

> "Access token corto y SIN ESTADO: no se guarda en ninguna tabla.
> Refresh token largo y CON ESTADO: se guarda su hash (nunca el token crudo) y
> rota en cada uso. Reusar un refresh ya rotado invalida la familia completa."

- Access: HS256 JWT, 15 minutes, `Authorization: Bearer`. Not stored server-side.
- Refresh: opaque, 30 days. Only its SHA-256 lives in `tb_refresh_token.tx_token_hash`.
  Transport depends on the client, because the clients are not equivalent:
  - **Mobile**: returned in the JSON body and kept in the device's secure storage.
  - **Panel (SPA)**: returned as `Set-Cookie: refresh_token=...; HttpOnly; Secure;
    SameSite=Lax; Path=/v1/auth` — JavaScript can never read it.
  `/v1/auth/refresh` and `/v1/auth/logout` accept the token from the body **or** from that
  cookie; the body is optional when the cookie is present.
- Every `/v1/auth/refresh` revokes the presented token and issues a new one in the same
  `co_token_family`.
- Presenting an already-rotated token is treated as theft: the whole family is revoked and
  the answer is `401 refresh_reused`.

### C4 — Credential changes end every session

Password reset (`/v1/auth/password/reset`), password change (`/v1/auth/password/change`)
and email change success revoke **all** refresh families of the user, including the
caller's. A stolen session must not survive the recovery that was meant to stop it.

### C5 — Email ownership is proven before the user exists

> "El email se prueba ANTES de crear la fila en tb_user: mientras nadie demuestre
> acceso a la casilla, el registro vive en tb_signup_request."

- `POST /v1/auth/signup` writes `tb_signup_request` (email, name, password hash) and sends
  a 6-digit code. The password is hashed with Argon2id before storage, verification or not.
- The email is immutable for the flow: a different email is a new signup with a new code.
- The code is single use, expires in 10 minutes, allows at most 5 attempts, and is stored
  only as a SHA-256 hash.
- `POST /v1/auth/signup/verify` creates `tb_user` and returns `201` with a token pair.

### C6 — Login is rate-limited and lockout-aware

- Consecutive failures increment `nu_failed_attempts` and reset on success.
- Past the threshold the account answers `423 account_locked` with `details.retry_after_seconds`
  until `dt_locked_until` passes.
- Every attempt, successful or not, appends to `tb_security_event` with an HMAC of the
  attempted email and the client IP.

### C7 — Email change targets the new address

`POST /v1/auth/email/change` requires the current password (a stolen access token alone
must not move the account) and sends the code to the **new** address. The code is issued
for a specific `tx_target`; if the account email changed meanwhile, verification is
rejected rather than applied to the new state.

### C8 — Social login resolves identity by provider subject, never by email

> "La identidad se resuelve SIEMPRE por (provider, sub) en tb_user_identity, NUNCA por
> email ... El token del proveedor NUNCA es la credencial de la API."

- `POST /v1/auth/social` receives the ID token the mobile app obtained with the native SDK,
  validates it against the provider's JWKS and issues this API's own JWT.
- No redirect, no client secret, no `golang.org/x/oauth2` on this path.
- First sight of a `(provider, sub)` creates the user; `tx_email` may be absent.

### C9 — The panel authorizes on permissions, not role names

`GET /v1/me` returns `roles` and the effective `permissions`. The panel decides what to
show from `permissions`; it must not infer capability from a role name.

Admin endpoints require the `admin.access` permission and answer `403 permission_denied`
without it. Role assignment (`PATCH /v1/admin/users/{userId}/roles`) takes the complete
desired set of role names, not a delta.

### C10 — The audit log is read-only through the API

> "Bitácora append-only ... NUNCA guarda códigos, tokens ni contraseñas ... y NO lleva FK a
> tb_user, porque la auditoría tiene que sobrevivir al borrado del usuario."

`GET /v1/admin/audit-events` reads. No endpoint updates or deletes an event. `email_hash`
is an HMAC; the clear address is never returned.

### C11 — Conventions

UUID strings; RFC 3339 UTC timestamps; lists paginated with `page` (1-based) and `limit`
(1–100, default 20) and a response carrying `page`, `limit`, `total`; identifiers in JSON
are the API's names, never the `co_`/`tx_` column names.

### C12 — Open decisions, recorded rather than assumed

| Decision | Current contract | Revisit when |
|---|---|---|
| Panel auth transport | **Decided:** the panel is a SPA. Access token (bearer) stays in memory only; the refresh token travels in an httpOnly cookie and is never exposed to JavaScript | — |
| Rate limiting | Not in the contract; `429` shape defined | An edge exists (LB/ingress) or a Go middleware lands |
| Token lifetimes | 15 min / 30 days | Product decides otherwise; they become env vars |
| Password policy | `minLength: 12` | A policy with strength rules is agreed |

### C13 — A browser SPA needs CORS, and the contract must say so

Because the panel is a SPA served from a different origin than the API, every call is a
cross-origin request and `Authorization` makes it non-simple: the browser sends an
`OPTIONS` preflight before the real request.

- The API answers preflight for the allowed origins only, configured by
  `CORS_ALLOWED_ORIGINS` (comma-separated, no wildcard when credentials are involved).
- Allowed request headers: `Authorization`, `Content-Type`. Allowed methods: the ones the
  contract uses.
- `Access-Control-Max-Age` is set so the preflight is not paid on every call.
- Preflight must not require authentication: an `OPTIONS` that answers `401` breaks the
  SPA before it can send a token.
- `Access-Control-Allow-Credentials: true` is required, because the refresh cookie must be
  sent cross-origin — which is exactly why the origin list can never contain `*`. With
  credentials, a wildcard origin is invalid and silently breaks the browser.
- `SameSite=Lax` is enough when the SPA and the API share a registrable domain
  (`admin.example.com` → `api.example.com`); `SameSite=None` is required only if they are
  different sites, and it widens the CSRF surface (see C14).

This is a server requirement, not a schema one: `openapi.yaml` documents the operations,
and CORS is the transport rule that lets a browser reach them.

### C14 — The refresh cookie needs CSRF protection

A cookie is sent by the browser automatically, so a cross-site form post can trigger a
refresh with the user's session. Two rules:

- The refresh and logout endpoints accept `Content-Type: application/json` only, which
  blocks simple cross-site form posts.
- They additionally require the double-submit token: the SPA sends `X-CSRF-Token` with a
  value that matches a non-HttpOnly `csrf_token` cookie issued alongside the refresh
  cookie. A cross-site caller can send the cookie but cannot read it to echo the header.
- `logout` is idempotent and revokes only the presented family, so a forced logout is the
  worst a bypass achieves.

The mobile path is unaffected: it sends the refresh token in the body and no cookie.
