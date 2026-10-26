# api — melZone Go service

The sole owner of the HTTP API and of writes. Go replaces the former Laravel
scaffolding (`api-core/`), which contained no product code.

This change lands the runtime only. No domain or security endpoint exists yet.

## Requirements

- Go 1.27
- PostgreSQL 15 (`melia`, schemas `seg` + `core`), Redis 7, RabbitMQ 3
- Or Docker for the whole stack

## Run

```bash
cp .env.example .env      # then export the values, the service reads the environment
go run ./cmd/api
```

With Docker (from the repository root):

```bash
docker compose up --build
```

## Endpoints

The HTTP contract lives in **[`openapi.yaml`](openapi.yaml)** (OpenAPI 3.1): 19 operations
covering health, auth, identity and the admin RBAC surface. It is the source of truth for
the mobile app and the admin panel; new endpoints update it in the same commit.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health/live` | Liveness. Never touches a dependency. `200 {"status":"ok"}` |
| `GET` | `/health/ready` | Readiness. Probes postgres + redis (required) and rabbitmq (optional). `200` or `503` |
| any | anything else | `404 {"error":"not found"}` |

`/health/ready` statuses: `ok` (all required probes answered), `degraded` (a required
probe answered but an optional one failed), `unavailable` (a required probe failed).
Both non-`ok` states return `503`.

## Configuration

| Variable | Required | Default | Meaning |
|---|---|---|---|
| `DATABASE_URL` | yes | — | PostgreSQL DSN |
| `REDIS_ADDR` | yes | — | `host:port` |
| `HTTP_ADDR` | no | `:8080` | listen address |
| `RABBITMQ_URL` | no | empty | when empty, the rabbitmq probe is omitted |
| `CORS_ALLOWED_ORIGINS` | no | empty | comma-separated browser origins; empty disables CORS |
| `JWT_SECRET` | yes | — | HS256 signing key, at least 32 characters |
| `ACCESS_TOKEN_TTL` | no | `15m` | access token lifetime |
| `REFRESH_TOKEN_TTL` | no | `720h` | refresh token lifetime |
| `LOGIN_MAX_ATTEMPTS` | no | `5` | consecutive failures before the account locks |
| `LOGIN_LOCK_DURATION` | no | `15m` | how long the lock lasts |
| `MAIL_QUEUE` | no | `melia.mail` | durable queue the API publishes mail to |
| `SMTP_ADDR` | no | `mailpit:1025` | SMTP target used by the worker |
| `MAIL_FROM` | no | `no-reply@melia.test` | envelope sender |
| `DB_INIT_DIR` | no | `/app/database/init` | directory holding `NN-*.sql` |
| `DB_AUTO_MIGRATE` | no | `true` | apply pending schema files at startup |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |

Missing required variables fail startup with the variable name in the error. There is no
fallback to a default database: a service that silently connects to the wrong database is
worse than one that refuses to start.

## Schema bootstrap

`database/init/NN-*.sql` is the single source of truth for the schema, and the service is
the only thing that applies it. The files are **copied into the image** at
`/app/database/init` — not bind-mounted — so the container user's uid never has to match
a host directory; rebuild the image after editing the DDL. The PostgreSQL container creates the database
(`POSTGRES_DB=melia`) and nothing else; there is no `docker-entrypoint-initdb.d` mount,
because two appliers of the same DDL cannot coexist — the entrypoint runs on a fresh
volume, the service runs on every start, and the second one would hit
`schema "seg" already exists`.

At startup the service applies the pending files and records each in
`seg.tb_schema_migration`, so restarts are no-ops.

Two adaptations are deliberate:

- `database/provision/01-create-database.sql` is not part of the set. A service
  connected to `melia` cannot create it; it exists for a manual bootstrap against a
  bare server.
- psql meta-commands (`\connect ...`) are stripped. They are psql directives, not SQL.

When the ledger is empty but `seg.tb_user` already exists — a volume created before the
ledger, or a manual `psql` run — the service **adopts** the schema: it records every file
as applied without executing it, logs a warning, and points at
`database/verify_base_model.sql`. Adoption is gated on that table, so a database without
it is never adopted and a failed application stops startup loudly. Adoption cannot tell a
complete schema from a half-built one that still has `seg.tb_user`, which is exactly why
the drift script exists.

Set `DB_AUTO_MIGRATE=false` when another tool owns the schema.

## Signup and mail

`POST /v1/auth/signup` writes the pending registration to `seg.tb_signup_request` and
publishes one message to the `melia.mail` queue. A separate process (`cmd/worker`) consumes
it and delivers it over SMTP. Keeping them apart means a slow mail server cannot hold an
HTTP request.

In development the SMTP target is **Mailpit** (`docker compose up`): it accepts everything
and shows it at <http://localhost:8025>. Nothing leaves the machine.

The code is never stored in clear: the row keeps `tx_code_hash` (SHA-256) and the mail
carries the only copy of the digits. If the queue is unreachable the request fails with
`500` rather than pretending a code was sent.

`POST /v1/auth/signup/verify` consumes the code under a row lock, promotes the pending
registration into `seg.tb_user` with `dt_email_verified_at` set, and returns the first token
pair. Five wrong attempts kill the registration. The access JWT carries only
`sub`/`iat`/`exp`/`jti`: roles and permissions are resolved per request, so revoking one
takes effect immediately instead of waiting for a token to expire.

## Login and lockout

`POST /v1/auth/login` answers `401 invalid_credentials` for an unknown address, a wrong
password and an inactive account alike; an unknown address still pays for an Argon2id
verification, so timing cannot enumerate accounts. Past `LOGIN_MAX_ATTEMPTS` the account
answers `423 account_locked` with `Retry-After` and `details.retry_after_seconds`, and a
successful login resets the counter.

Every attempt is written to `seg.tb_security_event` with an HMAC of the email, the client IP
and the user agent. The log never receives a password, a code or a clear address, and an
audit failure is logged but never fails the request.

## Sessions

`POST /v1/auth/refresh` rotates: the presented token is revoked in the same transaction that
inserts its successor, inside the same family. Presenting a token that was already rotated is
the theft signal — the whole family is revoked and the answer is `401 refresh_reused`, so the
client must log in again. An unknown or expired token answers `401 refresh_invalid` and
touches nothing.

`POST /v1/auth/logout` revokes the family and answers `204` even for an unknown token: the
desired state is reached either way, and an error would confirm whether the token existed.

## Two clients, two transports

The mobile app and the Angular panel are not equivalent, so the token pair is not delivered
the same way.

| Client | Recognised by | Refresh token | CSRF |
|---|---|---|---|
| Mobile | no `Origin` (or an untrusted one) | JSON body, stored in secure storage | not needed |
| Panel (SPA) | `Origin` in `CORS_ALLOWED_ORIGINS` | `Set-Cookie: refresh_token=...; HttpOnly; Secure; SameSite=Lax; Path=/v1/auth` | `X-CSRF-Token` must match the readable `csrf_token` cookie |

A browser never receives the refresh token in the body, so a script injected into the page
cannot read a long-lived credential. The cookie is httpOnly and scoped to `/v1/auth`.

Because the browser attaches the cookie automatically, `/v1/auth/refresh` and
`/v1/auth/logout` require the double-submit token when the cookie is the source: a
cross-site caller can send the cookie but cannot read it to echo the header. A mismatch
answers `403 csrf_invalid`. A token sent in the body needs no CSRF, since nothing attaches
it automatically.

`COOKIE_SECURE` defaults to `true`. Compose sets it to `false` for local http; leaving it
false anywhere else is a silent downgrade.

## Identity and RBAC

`GET /v1/me` returns the profile with its **effective** roles and permissions. The panel
decides what to show from `permissions`; it must never infer capability from a role name.

`GET /v1/admin/roles` and `GET /v1/admin/permissions` are guarded by `admin.access`. The
check reads the database on every request — user → active role → active permission — so
disabling a role takes effect on the next call instead of waiting for an access token to
expire. That is the whole reason the token carries identity only.

A valid token for an account that was deleted answers `401`, not `404`: the token is real,
the subject is gone.

## Password recovery and change

| Endpoint | Auth | Behaviour |
|---|---|---|
| `POST /v1/auth/password/forgot` | none | always `202`; mails a 6-digit code only if the account exists |
| `POST /v1/auth/password/reset` | none | `200` on success, `400 invalid_code`, `429 too_many_attempts` |
| `POST /v1/auth/password/change` | bearer | re-checks the current password, `400 invalid_password` if wrong |

The recovery code lives in `seg.tb_user_token` with purpose `PASSWORD_RESET`: only its
SHA-256, a 10-minute expiry, at most five attempts and a single use. Because `tx_code_hash`
is unique across the table, a generated code that already belongs to another account is
redrawn instead of failing the request.

Both `reset` and `change` revoke **every** refresh family of the user, including the caller's:
a session stolen before the recovery must not survive it. A successful reset also clears the
login lockout, so a user who locked themselves out can get back in.

An account created only through a social provider has no password; `change` answers
`400 invalid_password` for it rather than pretending to succeed.

## Email change

| Endpoint | Auth | Behaviour |
|---|---|---|
| `POST /v1/auth/email/change` | bearer | `202`; mails the code to the **new** address, requires the password |
| `POST /v1/auth/email/change/verify` | bearer | `200`, `400 invalid_code`, `409 email_unavailable` |

The current password is required, so a stolen access token alone cannot move an account. The
code goes to the new address, proving ownership of it before anything changes. A successful
change revokes every refresh family: the address that identified the account just changed.

Requesting a new code invalidates the previous pending one, so only the newest code can ever
be consumed — that is what makes a stale code unable to move the account later.

### Quotas on code requests

Both flows that mail a code enforce a per-user quota with a 60-second cooldown, but the limit
differs because the flows do:

| Flow | Limit | Why |
|---|---|---|
| Email change | **1 per 24h** | Rare operation; the strictest value costs little |
| Password recovery | **5 per 24h** | Urgent; a delayed mail must not lock someone out for a day |

A per-token attempt counter is not enough on its own: if the code can be reissued at will,
every reissue resets the counter and a 6-digit code becomes brute-forceable again. The quota
is what bounds the guess space, and it also stops the endpoint from being used to flood
somebody else's inbox.

`security.Quota` is a field on both services, so each flow states its own limit instead of one
shared value being loosened for everybody. A code lives 10 minutes: keep that in mind when
choosing a limit, because a typo or an expired code spends one of the requests.

The two endpoints report exhaustion differently, on purpose:

- **Email change** (authenticated) answers `429 rate_limited` with `Retry-After`: there is no
  account to hide from a caller who already holds a session.
- **Password recovery** (public) answers `202` and simply sends nothing. A `429` there would
  reveal that the account exists, because an unknown address never exhausts a quota — it
  never stores a token.

## Administration

| Endpoint | What it does |
|---|---|
| `GET /v1/admin/users` | paginated list, filtered by `q` (name or email) and `status` |
| `PATCH /v1/admin/users/{id}/roles` | **replaces** the role set of a user |
| `GET /v1/admin/audit-events` | reads the security log, filtered by `event`, `userId`, `from`, `to` |

All three require `admin.access`. Pagination follows the contract: `page` is 1-based, `limit`
defaults to 20 and is capped at 100, and the response carries `page`, `limit` and `total`.

Assigning roles is the only endpoint that grants capability, so every change is audited as
`rbac.roles.changed` with **both** the actor (the administrator) and the subject (the user
whose roles changed). An unknown or inactive role name rejects the whole request with `422`:
a capability change never half-applies.

### The first administrator

There is a chicken-and-egg problem: assigning roles needs `admin.access`, and nobody holds it
until someone is granted a role. `cmd/bootstrap` breaks it, once:

```bash
docker compose run --rm --entrypoint /usr/local/bin/bootstrap \
  -e EMAIL=ana@melia.test -e ROLE=SUPERADMIN api
```

It replaces that user's role set, exactly like the PATCH endpoint does. Register the account
first: it needs a verified user to attach the role to.

## Tests

```bash
go test ./...
```

No test needs a database, Redis, or RabbitMQ: probes and the migration ledger are
interfaces, and the tests pass fakes.

## Layout

```
cmd/api/            entrypoint: config, wiring, signals, graceful shutdown
internal/config/    environment parsing and validation
internal/httpapi/   router and health handlers
internal/db/        pgx pool, retry, schema bootstrap
internal/probe/     postgres, redis, rabbitmq readiness probes
```
