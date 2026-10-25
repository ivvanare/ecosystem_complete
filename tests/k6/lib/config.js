// Shared settings for the k6 suites. Every script reads BASE_URL so the same file works
// against compose (http://localhost:9000), a staging host, or a container network name.
export const BASE_URL = __ENV.BASE_URL || 'http://localhost:9000';

export const JSON_HEADERS = {
  'Content-Type': 'application/json',
  // The panel is a SPA on another origin; sending Origin here exercises the CORS path the
  // browser will use instead of the plain server-to-server path.
  ...(__ENV.ORIGIN ? { Origin: __ENV.ORIGIN } : {}),
};

// Passwords must clear the API's 12-character floor.
export const PASSWORD = 'k6-clave-larga-123';

// Unique address per iteration: signup is an upsert keyed by email, so reusing one would
// overwrite the pending row instead of exercising a fresh registration.
export function uniqueEmail(prefix) {
  return `${prefix}-${__VU}-${__ITER}-${Date.now()}@melia.test`;
}

// Argon2id (m=64MiB, t=1) dominates the signup response time: a request costs tens of
// milliseconds of CPU and 64 MiB of memory. Thresholds are set for a laptop, not a
// production box, and the load is kept low on purpose.
export const SMOKE_THRESHOLDS = {
  http_req_duration: ['p(95)<200'],
  http_req_failed: ['rate<0.01'],
  checks: ['rate>0.99'],
};

export const SIGNUP_THRESHOLDS = {
  http_req_duration: ['p(95)<2000'],
  http_req_failed: ['rate<0.01'],
  checks: ['rate>0.99'],
};
