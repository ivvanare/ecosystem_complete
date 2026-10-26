// Full auth journey: every case the security module exposes today, in one run.
//
//   k6 run tests/k6/auth-flow.js
//
// This is an integration suite, not a load test: one iteration walks the whole flow so a
// regression in any step fails loudly. It needs Mailpit, because it reads the 6-digit code
// the way a human would.
//
// WARNING: it creates accounts and trips a lockout. Run it against disposable data.
import http from 'k6/http';
import { check, group, sleep } from 'k6';
import {
  BASE_URL,
  MAILPIT_URL,
  ORIGIN_HEADER,
  PASSWORD,
  postJSON,
  refreshCookieName,
  uniqueEmail,
  waitForCode,
  waitForMail,
} from './lib/config.js';

// The journey requests 400, 401, 404, 422 and 423 on purpose. Declaring them expected keeps
// http_req_failed meaningful instead of reporting half the run as failures; the per-case
// checks still assert the exact status each step must return.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 400, 401, 404, 422, 423));

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: {
    checks: ['rate>0.99'],
  },
};

const MAX_ATTEMPTS = 5;

export default function () {
  const email = uniqueEmail('flow');
  const lockEmail = uniqueEmail('lock');

  group('health', () => {
    const live = http.get(`${BASE_URL}/health/live`);
    check(live, { 'live is 200': (r) => r.status === 200 });

    const ready = http.get(`${BASE_URL}/health/ready`);
    check(ready, { 'ready is 200': (r) => r.status === 200 });
  });

  group('signup: validation rejects bad input', () => {
    const cases = [
      { name: 'bad email', body: { email: 'no-es-email', name: 'Ana', password: PASSWORD }, field: 'email' },
      { name: 'empty name', body: { email: email, name: '  ', password: PASSWORD }, field: 'name' },
      { name: 'short password', body: { email: email, name: 'Ana', password: 'corta' }, field: 'password' },
    ];
    for (const tc of cases) {
      const res = postJSON(http, '/v1/auth/signup', tc.body);
      check(res, {
        [`${tc.name} is 422`]: (r) => r.status === 422,
        [`${tc.name} names ${tc.field}`]: (r) =>
          (r.json('error.details.fields') || []).some((f) => f.field === tc.field),
      });
    }
  });

  group('signup: starts and mails a code', () => {
    const res = postJSON(http, '/v1/auth/signup', { email: email, name: 'Ana', password: PASSWORD });
    check(res, {
      'signup is 202': (r) => r.status === 202,
      'signup has no body': (r) => !r.body,
    });

    const message = waitForMail(http, email);
    check(message, { 'signup mail arrived': (m) => m !== null });
  });

  group('signup: verify rejects a malformed code before any work', () => {
    const res = postJSON(http, '/v1/auth/signup/verify', { email: email, code: '12ab' });
    check(res, {
      'malformed code is 422': (r) => r.status === 422,
      'malformed code is validation_failed': (r) => r.json('error.code') === 'validation_failed',
    });
  });

  group('signup: verify rejects a wrong code and keeps the counter', () => {
    const wrong = postJSON(http, '/v1/auth/signup/verify', { email: email, code: '000000' });
    check(wrong, {
      'wrong code is 400': (r) => r.status === 400,
      'wrong code is invalid_code': (r) => r.json('error.code') === 'invalid_code',
    });
  });

  group('signup: verify rejects an unknown address with the same answer', () => {
    const unknown = postJSON(http, '/v1/auth/signup/verify', {
      email: uniqueEmail('nadie'),
      code: '000000',
    });
    check(unknown, {
      'unknown address is 400': (r) => r.status === 400,
      'unknown address is indistinguishable': (r) => r.json('error.code') === 'invalid_code',
    });
  });

  group('signup: verify accepts the real code and opens a session', () => {
    const code = waitForCode(http, email);
    check(code, { 'code extracted from mail': (c) => c !== null });

    const res = postJSON(http, '/v1/auth/signup/verify', { email: email, code: code });
    check(res, {
      'verify is 201': (r) => r.status === 201,
      'verify returns an access token': (r) => typeof r.json('access_token') === 'string' && r.json('access_token').length > 0,
      'verify returns a refresh token': (r) => typeof r.json('refresh_token') === 'string',
      'token type is Bearer': (r) => r.json('token_type') === 'Bearer',
      'access lifetime is 900s': (r) => r.json('expires_in') === 900,
    });
  });

  group('signup: a second registration of the same address leaks nothing', () => {
    const res = postJSON(http, '/v1/auth/signup', { email: email, name: 'Ana', password: PASSWORD });
    check(res, {
      'repeat signup is 202': (r) => r.status === 202,
      'repeat signup has no body': (r) => !r.body,
    });

    // Wait for the specific mail: this address already received the code, so a plain
    // "newest message" lookup would return that older one and fail without waiting.
    const message = waitForMail(http, email, 12, (m) => /cuenta/i.test(m.Subject || ''));
    check(message, {
      'repeat signup mails the owner instead of a code': (m) => m !== null,
    });
  });

  group('signup: the code cannot be reused', () => {
    // The registration is consumed, so the same code is now just a wrong code.
    const res = postJSON(http, '/v1/auth/signup/verify', { email: email, code: '000000' });
    check(res, {
      'consumed registration is 400': (r) => r.status === 400,
    });
  });

  let loginTokens = null;

  group('login: success', () => {
    const res = postJSON(http, '/v1/auth/login', {
      email: email,
      password: PASSWORD,
      device: 'k6',
    });
    loginTokens = res.json();
    check(res, {
      'login is 200': (r) => r.status === 200,
      'login returns a pair': (r) => !!r.json('access_token') && !!r.json('refresh_token'),
    });
  });

  group('login: wrong password', () => {
    const res = postJSON(http, '/v1/auth/login', { email: email, password: 'equivocada' });
    check(res, {
      'wrong password is 401': (r) => r.status === 401,
      'wrong password is invalid_credentials': (r) => r.json('error.code') === 'invalid_credentials',
    });
  });

  group('login: unknown account is indistinguishable', () => {
    const res = postJSON(http, '/v1/auth/login', {
      email: uniqueEmail('nadie'),
      password: PASSWORD,
    });
    check(res, {
      'unknown account is 401': (r) => r.status === 401,
      'unknown account is invalid_credentials': (r) => r.json('error.code') === 'invalid_credentials',
    });
  });

  group('login: validation', () => {
    const res = postJSON(http, '/v1/auth/login', { email: 'no-es-email', password: '' });
    check(res, {
      'login validation is 422': (r) => r.status === 422,
      'login validation is validation_failed': (r) => r.json('error.code') === 'validation_failed',
    });
  });

  group('login: lockout after repeated failures', () => {
    // A dedicated account: this case deliberately locks it.
    const start = postJSON(http, '/v1/auth/signup', { email: lockEmail, name: 'Lock', password: PASSWORD });
    check(start, { 'lockout account created': (r) => r.status === 202 });
    const code = waitForCode(http, lockEmail);
    check(code, { 'lockout code extracted': (c) => c !== null });
    const verified = postJSON(http, '/v1/auth/signup/verify', { email: lockEmail, code: code });
    check(verified, { 'lockout account verified': (r) => r.status === 201 });

    const statuses = [];
    for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
      const res = postJSON(http, '/v1/auth/login', { email: lockEmail, password: 'mala' });
      statuses.push(res.status);
      if (attempt < MAX_ATTEMPTS) {
        check(res, { [`attempt ${attempt} is 401`]: (r) => r.status === 401 });
      } else {
        check(res, {
          [`attempt ${attempt} locks the account`]: (r) => r.status === 423,
          'lockout carries Retry-After': (r) => !!r.headers['Retry-After'],
          'lockout reports retry_after_seconds': (r) => r.json('error.details.retry_after_seconds') > 0,
          'lockout code is account_locked': (r) => r.json('error.code') === 'account_locked',
        });
      }
    }

    // Even the right password is refused while locked: that is the point of the lock.
    const correct = postJSON(http, '/v1/auth/login', { email: lockEmail, password: PASSWORD });
    check(correct, {
      'correct password is still refused while locked': (r) => r.status === 423,
    });
  });

  group('refresh: rotation', () => {
    const first = postJSON(http, '/v1/auth/refresh', { refresh_token: loginTokens.refresh_token });
    const rotated = first.json();
    check(first, {
      'refresh is 200': (r) => r.status === 200,
      'refresh returns a new refresh token': (r) => r.json('refresh_token') !== loginTokens.refresh_token,
      'refresh returns a new access token': (r) => r.json('access_token') !== loginTokens.access_token,
      'rotated token stays in a session': (r) => r.json('expires_in') === 900,
    });

    // Replaying the consumed token is the theft signal.
    const replay = postJSON(http, '/v1/auth/refresh', { refresh_token: loginTokens.refresh_token });
    check(replay, {
      'replaying a rotated token is 401': (r) => r.status === 401,
      'replay reports refresh_reused': (r) => r.json('error.code') === 'refresh_reused',
    });

    // ... and it takes the whole family with it, including the token just issued.
    const afterReuse = postJSON(http, '/v1/auth/refresh', { refresh_token: rotated.refresh_token });
    check(afterReuse, {
      'family revocation kills the rotated token': (r) => r.status === 401,
    });
  });

  group('refresh: unknown and malformed tokens', () => {
    const unknown = postJSON(http, '/v1/auth/refresh', { refresh_token: 'inventado' });
    check(unknown, {
      'unknown refresh token is 401': (r) => r.status === 401,
      'unknown refresh token is refresh_invalid': (r) => r.json('error.code') === 'refresh_invalid',
    });

    const missing = postJSON(http, '/v1/auth/refresh', {});
    check(missing, {
      'missing refresh token is 401': (r) => r.status === 401,
    });
  });

  group('logout: revokes the family and is idempotent', () => {
    const fresh = postJSON(http, '/v1/auth/login', { email: email, password: PASSWORD });
    const pair = fresh.json();
    check(fresh, { 're-login after reuse is 200': (r) => r.status === 200 });

    const out = postJSON(http, '/v1/auth/logout', { refresh_token: pair.refresh_token });
    check(out, { 'logout is 204': (r) => r.status === 204 });

    const after = postJSON(http, '/v1/auth/refresh', { refresh_token: pair.refresh_token });
    check(after, { 'refresh after logout is 401': (r) => r.status === 401 });

    const again = postJSON(http, '/v1/auth/logout', { refresh_token: 'inventado' });
    check(again, { 'logout of an unknown token is still 204': (r) => r.status === 204 });
  });

  group('browser session: cookie plus CSRF', () => {
    // A browser client is recognised by an allowed Origin. It gets the refresh token as an
    // httpOnly cookie and never in the body, and every cookie call must carry the
    // double-submit token.
    http.cookieJar().clear(BASE_URL);

    const login = postJSON(
      http,
      '/v1/auth/login',
      { email: email, password: PASSWORD },
      { Origin: ORIGIN_HEADER },
    );
    check(login, {
      'browser login is 200': (r) => r.status === 200,
      'browser login hides the refresh token from the body': (r) => r.json('refresh_token') === undefined,
      'browser login still returns an access token': (r) => !!r.json('access_token'),
    });

    const jar = http.cookieJar().cookiesForURL(BASE_URL);
    check(jar, {
      'refresh cookie is set': (j) => Array.isArray(j[refreshCookieName]) && j[refreshCookieName].length > 0,
      'csrf cookie is set': (j) => Array.isArray(j[csrfCookieName]) && j[csrfCookieName].length > 0,
    });

    const csrf = (jar[csrfCookieName] || [])[0];

    // No header: the cookie alone must not be enough.
    const noCsrf = http.post(`${BASE_URL}/v1/auth/refresh`, '{}', {
      headers: { 'Content-Type': 'application/json', Origin: ORIGIN_HEADER },
    });
    check(noCsrf, {
      'cookie refresh without CSRF is 403': (r) => r.status === 403,
      'it reports csrf_invalid': (r) => r.json('error.code') === 'csrf_invalid',
    });

    const withCsrf = http.post(`${BASE_URL}/v1/auth/refresh`, '{}', {
      headers: { 'Content-Type': 'application/json', Origin: ORIGIN_HEADER, 'X-CSRF-Token': csrf },
    });
    check(withCsrf, {
      'cookie refresh with CSRF is 200': (r) => r.status === 200,
      'cookie refresh hides the rotated token too': (r) => r.json('refresh_token') === undefined,
    });

    const freshCsrf = (http.cookieJar().cookiesForURL(BASE_URL)[csrfCookieName] || [])[0];
    const logout = http.post(`${BASE_URL}/v1/auth/logout`, '{}', {
      headers: { 'Content-Type': 'application/json', Origin: ORIGIN_HEADER, 'X-CSRF-Token': freshCsrf },
    });
    check(logout, { 'browser logout is 204': (r) => r.status === 204 });

    http.cookieJar().clear(BASE_URL);
  });

  group('errors: unknown path uses the envelope', () => {
    const res = http.get(`${BASE_URL}/v1/does-not-exist`);
    check(res, {
      'unknown path is 404': (r) => r.status === 404,
      'unknown path is not_found': (r) => r.json('error.code') === 'not_found',
    });
  });

  group('cors: preflight and disallowed origin', () => {
    const allowed = http.options(`${BASE_URL}/v1/auth/login`, null, {
      headers: {
        Origin: ORIGIN_HEADER,
        'Access-Control-Request-Method': 'POST',
        'Access-Control-Request-Headers': 'content-type',
      },
    });
    check(allowed, {
      'preflight is 204': (r) => r.status === 204,
      'preflight echoes the origin': (r) => r.headers['Access-Control-Allow-Origin'] === ORIGIN_HEADER,
      'preflight allows credentials': (r) => r.headers['Access-Control-Allow-Credentials'] === 'true',
    });

    const denied = http.options(`${BASE_URL}/v1/auth/login`, null, {
      headers: { Origin: 'https://evil.test', 'Access-Control-Request-Method': 'POST' },
    });
    check(denied, {
      'foreign origin gets no CORS header': (r) => !r.headers['Access-Control-Allow-Origin'],
    });
  });

  sleep(1);
}
