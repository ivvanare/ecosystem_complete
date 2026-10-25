// Signup under load: proves the write path, the queue publish and the worker, not just the
// HTTP surface.
//
//   k6 run tests/k6/signup.js
//   BASE_URL=https://api.staging k6 run tests/k6/signup.js
//
// WARNING: every iteration writes a row to seg.tb_signup_request and queues one email.
// Mailpit grows by that many messages. Do not point this at an environment whose data you
// care about.
//
// Load is deliberately low: Argon2id is configured with 64 MiB and one pass, so each
// request costs real CPU and memory. Ten virtual users on a laptop saturate it long before
// the HTTP layer does, and that saturation is the point of the measurement.
import http from 'k6/http';
import { check, sleep } from 'k6';
import {
  BASE_URL,
  JSON_HEADERS,
  PASSWORD,
  SIGNUP_THRESHOLDS,
  uniqueEmail,
} from './lib/config.js';

// This script sends one deliberately invalid request every ten iterations to exercise
// validation on the same path. k6 counts a 422 as a failed request unless told otherwise,
// which made the error-rate threshold report 10.6% while every check passed.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 422));

// Pause between iterations. The default of 1s makes five VUs measure ~4 req/s, which is a
// smoke test with extra steps; set SLEEP=0 to measure actual throughput, accepting that
// Argon2id will saturate the CPU long before the HTTP layer does.
const SLEEP = __ENV.SLEEP === undefined ? 1 : Number(__ENV.SLEEP);

export const options = {
  scenarios: {
    signup: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 5 },
        { duration: '20s', target: 5 },
        { duration: '5s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: SIGNUP_THRESHOLDS,
};

export default function () {
  const payload = JSON.stringify({
    email: uniqueEmail('k6'),
    name: 'K6 Load',
    password: PASSWORD,
  });

  const response = http.post(`${BASE_URL}/v1/auth/signup`, payload, { headers: JSON_HEADERS });

  check(response, {
    'signup is 202': (r) => r.status === 202,
    // The response must not differ by account state, so it carries nothing.
    'signup has no body': (r) => !r.body,
  });

  // Validation runs on the same path, so it is measured too — but sparsely: it is not the
  // hot path, and every extra request competes with Argon2id for CPU.
  if (__ITER % 10 === 0) {
    const invalid = http.post(
      `${BASE_URL}/v1/auth/signup`,
      JSON.stringify({ email: 'no-es-email', name: ' ', password: 'corta' }),
      { headers: JSON_HEADERS },
    );
    check(invalid, {
      'invalid input is 422': (r) => r.status === 422,
      'validation uses the envelope': (r) => r.json('error.code') === 'validation_failed',
    });
  }

  sleep(SLEEP);
}
