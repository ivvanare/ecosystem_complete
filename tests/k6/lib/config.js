import { sleep } from 'k6';

// Shared settings for the k6 suites. Every script reads BASE_URL so the same file works
// against compose (http://localhost:9000), a staging host, or a container network name.
export const BASE_URL = __ENV.BASE_URL || 'http://localhost:9000';

// Mailpit is where development mail lands. The auth journey reads the 6-digit code from it,
// which is what makes an end-to-end flow possible in a load-test tool.
export const MAILPIT_URL = __ENV.MAILPIT_URL || 'http://localhost:8025';

// The admin panel is an Angular SPA; `ng serve` defaults to 4200. This must match
// CORS_ALLOWED_ORIGINS on the server or every browser call fails the preflight.
// Must match the API's cookie names (api/internal/httpapi/cookies.go).
export const refreshCookieName = 'refresh_token';
export const csrfCookieName = 'csrf_token';

export const ORIGIN_HEADER = __ENV.ORIGIN || 'http://localhost:4200';

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

// ---- helpers shared by the journey -----------------------------------------

export function postJSON(http, path, body, extraHeaders) {
  return http.post(`${BASE_URL}${path}`, JSON.stringify(body), {
    headers: { ...JSON_HEADERS, ...(extraHeaders || {}) },
  });
}

export function getJSON(http, path) {
  return http.get(`${BASE_URL}${path}`, { headers: JSON_HEADERS });
}

// latestMailTo returns the newest Mailpit message addressed to the given address, or null.
export function latestMailTo(http, email) {
  const res = http.get(`${MAILPIT_URL}/api/v1/messages?limit=50`);
  if (res.status !== 200) {
    return null;
  }
  const messages = res.json('messages') || [];
  for (const message of messages) {
    const recipients = (message.To || []).map((t) => t.Address);
    if (recipients.includes(email)) {
      return message;
    }
  }
  return null;
}

// mailText returns the plain-text body of a Mailpit message.
export function mailText(http, id) {
  const res = http.get(`${MAILPIT_URL}/api/v1/message/${id}`);
  if (res.status !== 200) {
    return '';
  }
  return res.json('Text') || '';
}

// waitForMail polls until a message for the address satisfies the predicate, because
// delivery goes through RabbitMQ and the worker: the API answers 202 before the mail exists.
//
// The predicate matters: an address that already received a code will have an older message
// sitting in Mailpit, and returning "the newest match for this address" would hand back that
// stale one without waiting. The first version of this helper did exactly that and reported
// a false failure for the "account already exists" mail.
export function waitForMail(http, email, attempts = 12, matches = () => true) {
  for (let i = 0; i < attempts; i++) {
    const res = http.get(`${MAILPIT_URL}/api/v1/messages?limit=50`);
    if (res.status === 200) {
      for (const message of res.json('messages') || []) {
        const recipients = (message.To || []).map((t) => t.Address);
        if (recipients.includes(email) && matches(message)) {
          return message;
        }
      }
    }
    sleep(0.5);
  }
  return null;
}

// waitForCode returns the 6-digit code mailed to the address, or null.
//
// subjectPattern narrows which mail to read: an address accumulates signup, "already
// registered" and password mails, and the wrong one has a code too.
export function waitForCode(http, email, attempts = 12, subjectPattern = null) {
  const message = waitForMail(
    http,
    email,
    attempts,
    subjectPattern ? (m) => subjectPattern.test(m.Subject || '') : () => true,
  );
  if (!message) {
    return null;
  }
  const match = mailText(http, message.ID).match(/\b(\d{6})\b/);
  return match ? match[1] : null;
}
