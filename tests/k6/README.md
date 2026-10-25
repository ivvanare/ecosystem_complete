# k6 load tests

Scripts for [k6](https://k6.io/). They check the API's contract **and** its cost, which is
what unit tests cannot see: a handler that passes `go test` can still melt under Argon2id.

## Scripts

| Script | What it does | Writes data |
|---|---|---|
| `smoke.js` | `/health/live`, `/health/ready` every 5th iteration, and the 404 envelope. 2 VUs, 10s. | No |
| `signup.js` | `POST /v1/auth/signup` under a ramping load, plus validation checks. 5 VUs peak. | **Yes** — one `tb_signup_request` row and one queued email per iteration |

`smoke.js` is safe against any environment, including production. `signup.js` is not.

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
| `ORIGIN` | unset | when set, sends `Origin` so the CORS path is exercised as a browser would |
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

`login`, `refresh` and the authenticated routes have no load script because they do not
exist. They arrive with the session core; `lib/config.js` already centralises what they
will need.
