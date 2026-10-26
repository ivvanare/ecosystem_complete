# k6 load tests

Scripts for [k6](https://k6.io/). They check the API's contract **and** its cost, which is
what unit tests cannot see: a handler that passes `go test` can still melt under Argon2id.

## Scripts

| Script | What it does | Writes data |
|---|---|---|
| `smoke.js` | `/health/live`, `/health/ready` every 5th iteration, and the 404 envelope. 2 VUs, 10s. | No |
| `signup.js` | `POST /v1/auth/signup` under a ramping load, plus validation checks. 5 VUs peak. | **Yes** — one `tb_signup_request` row and one queued email per iteration |
| `auth-flow.js` | One full journey through every auth case, reading the 6-digit code from Mailpit like a human would. 1 iteration. | **Yes** — two accounts per run, one of them deliberately locked |

`smoke.js` is safe against any environment, including production. The other two are not.

`auth-flow.js` is an integration suite, not a load test: one iteration walks the flow so a
regression in any step fails loudly. It needs Mailpit reachable at `MAILPIT_URL`; that is
what makes a real end-to-end run possible in a load-testing tool.

### Cases covered by `auth-flow.js`

| Group | Checks |
|---|---|
| health | liveness 200, readiness 200 |
| signup validation | bad email, empty name, short password → 422 with the offending field |
| signup start | 202, empty body, mail actually arrives |
| verify: malformed code | `12ab` → 422 before any work |
| verify: wrong code | `000000` → 400 `invalid_code`, counter kept |
| verify: unknown address | same 400 as a wrong code, so it cannot enumerate |
| verify: real code | 201, access + refresh tokens, `Bearer`, `expires_in` 900 |
| repeat signup | 202 and the owner gets "you already have an account" instead of a code |
| consumed code | the registration is gone, so the code is now just wrong |
| login | 200 with a pair |
| login failures | wrong password 401, unknown account 401, malformed body 422 |
| lockout | 4×401 then 423 with `Retry-After`, and the correct password is refused while locked |
| refresh | 200 with a new pair, the rotated token differs from the presented one |
| refresh reuse | replaying a consumed token → 401 `refresh_reused`, and the family dies with it |
| refresh failures | unknown token and missing token → 401 `refresh_invalid` |
| logout | 204, the family stops refreshing, and an unknown token still answers 204 |
| errors | unknown path → 404 `not_found` |
| CORS | preflight 204 with the exact origin and credentials; a foreign origin gets no header |

## Running

With the k6 binary:

```bash
k6 run tests/k6/smoke.js
k6 run tests/k6/signup.js
```

With Docker (no local install):

```bash
docker run --rm -i --network host -v "$PWD/tests/k6:/scripts" grafana/k6 run /scripts/smoke.js
docker run --rm -i --network host -v "$PWD/tests/k6:/scripts" grafana/k6 run /scripts/signup.js
```

`--network host` is what lets the container reach `localhost:9000`. On macOS or Windows
use `host.docker.internal` instead:

```bash
docker run --rm -i -v "$PWD/tests/k6:/scripts" -e BASE_URL=http://host.docker.internal:9000 \
  grafana/k6 run /scripts/smoke.js
```

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://localhost:9000` | API under test |
| `MAILPIT_URL` | `http://localhost:8025` | where `auth-flow.js` reads codes from |
| `ORIGIN` | `http://localhost:4200` | origin used for the CORS checks; must match `CORS_ALLOWED_ORIGINS` |
| `SLEEP` | `1` | seconds between signup iterations. `0` measures throughput instead of smoke |

## Thresholds, and why they are loose

```
smoke   p(95) < 200ms   failed < 1%   checks > 99%
signup  p(95) < 2000ms  failed < 1%   checks > 99%
```

Signup is not slow because of HTTP. `argon2id` is configured with `m=64 MiB, t=1`, so every
request allocates 64 MiB and burns tens of milliseconds of CPU by design. The 2-second
threshold is a laptop baseline for 5 VUs, not a production SLO: tighten it once the target
hardware exists.

If `signup.js` fails on latency while `smoke.js` is green, the bottleneck is hashing, not
the server — that is the signal this suite exists to produce.

`signup.js` declares `422` as an expected status for the same reason: it sends one invalid
request every ten iterations on purpose. Without it the error-rate threshold reports ~10%
while every check passes.

The default `SLEEP=1` caps each VU at about one request per second. That is a smoke run, not
a throughput measurement — use `SLEEP=0` for the latter.

`smoke.js` declares `404` and `503` as expected statuses. It probes an unknown path and the
readiness endpoint on purpose, and without that declaration k6 counts them as failed
requests: the first run reported 45% failures with every check passing.

## What is not covered yet

`refresh`, `logout`, `/v1/me`, password reset/change and the social login do not exist yet,
so no script touches them. The journey grows with them: each new endpoint adds a group here
before it gets its own load script.
