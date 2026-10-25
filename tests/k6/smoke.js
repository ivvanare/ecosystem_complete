// Smoke: the cheapest check that the process is alive and answering with the contract's
// shapes. Safe to run against anything, including production: it writes nothing.
//
//   k6 run tests/k6/smoke.js
//   BASE_URL=https://api.example.com k6 run tests/k6/smoke.js
import http from 'k6/http';
import { check, sleep } from 'k6';
import { BASE_URL, SMOKE_THRESHOLDS } from './lib/config.js';

// This script requests a 404 on purpose and may see a 503 from readiness. Without telling
// k6 those are expected, they count as failed requests and the error-rate threshold turns
// into noise: the first run reported 45% failed with every check green.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 404, 503));

export const options = {
  vus: 2,
  duration: '10s',
  thresholds: SMOKE_THRESHOLDS,
};

export default function () {
  const live = http.get(`${BASE_URL}/health/live`);
  check(live, {
    'live is 200': (r) => r.status === 200,
    'live reports ok': (r) => r.json('status') === 'ok',
  });

  // Readiness touches PostgreSQL and Redis, so it is deliberately checked less often than
  // liveness: hammering it would hammer the dependencies.
  if (__ITER % 5 === 0) {
    const ready = http.get(`${BASE_URL}/health/ready`);
    check(ready, {
      'ready is 200 or 503': (r) => r.status === 200 || r.status === 503,
      'ready has a status': (r) => ['ok', 'degraded', 'unavailable'].includes(r.json('status')),
      'ready lists checks': (r) => Array.isArray(r.json('checks')),
    });
  }

  // Unknown paths must answer the error envelope, never a bare string.
  const missing = http.get(`${BASE_URL}/v1/does-not-exist`);
  check(missing, {
    'unknown path is 404': (r) => r.status === 404,
    'unknown path uses the envelope': (r) => r.json('error.code') === 'not_found',
  });

  sleep(0.2);
}
