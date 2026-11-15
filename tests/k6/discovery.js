// Discovery: the read path production actually hammers, and the only one whose cost lives
// behind PostGIS and Redis instead of behind a fake.
//
//   k6 run tests/k6/discovery.js
//   BASE_URL=https://api.example.com k6 run tests/k6/discovery.js
//
// It writes nothing: every call is a public GET, and the two error cases are requests the API
// refuses before touching the database. That makes it safe to point at any environment, like
// smoke.js.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { BASE_URL } from './lib/config.js';

// Madrid: the coordinates every other suite uses, and dense enough that a 2 km radius returns
// rows instead of an empty page.
const NEAR = '40.4168,-3.7038';
const BBOX = '-3.75,40.40,-3.65,40.45';

// 422 is expected in the error checks and 404 is what a retired place answers. Without
// declaring them, k6 counts them as failed requests and the error-rate threshold reports
// noise instead of failure.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 404, 422));

const thresholds = {
  // Laptop numbers, not production ones: the point is catching a regression, not certifying
  // a server. A list query that crosses 400ms at p95 is a query that lost its index.
  http_req_duration: ['p(95)<400'],
  http_req_failed: ['rate<0.01'],
  checks: ['rate>0.99'],
};

export const options = {
  scenarios: {
    list: {
      executor: 'constant-vus',
      vus: 3,
      duration: '20s',
      exec: 'listMode',
    },
    map: {
      executor: 'constant-vus',
      vus: 2,
      duration: '20s',
      exec: 'mapMode',
      startTime: '2s',
    },
  },
  thresholds,
};

function get(path) {
  return http.get(`${BASE_URL}${path}`);
}

// listMode is the page a person opens: places near them, nearest first.
export function listMode() {
  const res = get(`/v1/places?near=${NEAR}&radius=2000&limit=20`);
  check(res, {
    'list is 200': (r) => r.status === 200,
    'list returns a page': (r) => Array.isArray(r.json('data')),
    'list reports a total': (r) => typeof r.json('total') === 'number',
    'list says whether the sort applied': (r) => typeof r.json('sortApplied') === 'string',
  });

  // The nearest-first path is the one backed by the GiST index; asking for distance order is
  // what a real client does and what a dropped index would make obvious.
  const sorted = get(`/v1/places?near=${NEAR}&radius=5000&sort=distance`);
  check(sorted, {
    'sorted list is 200': (r) => r.status === 200,
    'sorted list is not degraded': (r) => r.json('degraded') === false,
  });

  sleep(0.3);
}

// mapMode is the clustered read: bbox plus zoom, which the server turns into H3 cells.
export function mapMode() {
  const res = get(`/v1/places?bbox=${BBOX}&zoom=14`);
  check(res, {
    'map is 200': (r) => r.status === 200,
    'map answers in map mode': (r) => r.json('mode') === 'map',
    'map returns clusters': (r) => Array.isArray(r.json('clusters')),
    'map reports a resolution': (r) => {
      const resolution = r.json('resolution');
      return typeof resolution === 'number' && resolution >= 3 && resolution <= 9;
    },
  });

  sleep(0.3);
}

// The catalogue reads are public and cached upstream by nobody: they are cheap, and a
// regression in either is a panel that cannot render its selects.
export default function () {
  for (const path of ['/v1/place-categories', '/v1/place-attributes']) {
    const res = get(path);
    check(res, {
      [`${path} is 200`]: (r) => r.status === 200,
      [`${path} returns a list`]: (r) => Array.isArray(r.json('data')),
    });
  }

  // Neither locator: the contract says 422 with the field named, never a 200 listing the world.
  const noLocator = get('/v1/places');
  check(noLocator, {
    'no locator is 422': (r) => r.status === 422,
    'no locator names the field': (r) => {
      const fields = r.json('error.details.fields') || [];
      return fields.some((f) => f.field === 'locator');
    },
  });

  // Both locators at once is refused for the same reason: the mode must be unambiguous.
  const both = get(`/v1/places?near=${NEAR}&bbox=${BBOX}&zoom=14`);
  check(both, {
    'both locators is 422': (r) => r.status === 422,
  });

  // A one-character search is under the contract's floor.
  const short = get(`/v1/places?near=${NEAR}&q=a`);
  check(short, {
    'short q is 422 too_short': (r) => {
      const fields = r.json('error.details.fields') || [];
      return r.status === 422 && fields.some((f) => f.field === 'q' && f.code === 'too_short');
    },
  });
}
