// Plans: the write path, end to end, in one iteration.
//
//   k6 run tests/k6/plans.js
//
// This is an integration check, not a load test: it walks the journey a person makes — register,
// pick a place, create a plan, put the place in it, vote — and fails loudly at the first step that
// regresses. Load belongs in discovery.js; what cannot be measured with fakes is whether the whole
// chain still works against the real database, the real worker and the real broker.
//
// It WRITES: one account, one plan and one vote per run. Do not point it at anything you care
// about. It needs Mailpit (MAILPIT_URL) because the registration code arrives by mail.
import http from 'k6/http';
import { check, group, fail } from 'k6';
import {
  BASE_URL,
  JSON_HEADERS,
  PASSWORD,
  getJSON,
  postJSON,
  uniqueEmail,
  waitForCode,
} from './lib/config.js';

// 201/204/202 are successes. 404 is the realtime route's correct answer when no broker is
// configured, and 401 is what the last group asserts on purpose: an anonymous read must not see a
// private plan. Without declaring both, the error rate reports the journey's own evidence as noise.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 401, 404));

export const options = {
  iterations: 1,
  thresholds: {
    // One journey, so a threshold on duration is a ceiling for the whole chain, not a percentile:
    // registration pays Argon2id, so the budget is generous on purpose.
    checks: ['rate>0.99'],
  },
};

// Same rule as discovery.js: the seed is Caracas, and a journey that cannot find a place fails
// instead of passing with nothing to plan. PLACE_ID short-circuits the lookup when CI knows it.
const NEAR = __ENV.K6_NEAR || '10.496,-66.853';
const PLACE_ID = __ENV.PLACE_ID || '';

function bearer(token, extra) {
  return { ...JSON_HEADERS, Authorization: `Bearer ${token}`, ...(extra || {}) };
}

function post(path, body, headers) {
  return http.post(`${BASE_URL}${path}`, JSON.stringify(body), { headers });
}

export default function () {
  const email = uniqueEmail('plans');

  // ---- register: the only way to get a real account without seeded credentials ----
  let token = '';
  group('signup opens a session', () => {
    const started = postJSON(http, '/v1/auth/signup', { email, name: 'K6 Planes', password: PASSWORD });
    check(started, { 'signup accepted': (r) => r.status === 202 });

    const code = waitForCode(http, email);
    if (!code) {
      fail('no signup code arrived in Mailpit: the worker or the broker is not running');
    }
    const verified = postJSON(http, '/v1/auth/signup/verify', { email, code });
    check(verified, {
      'verify creates the account': (r) => r.status === 201,
      'verify returns a token': (r) => typeof r.json('access_token') === 'string',
    });
    token = verified.json('access_token');
  });

  // ---- a place to put in the plan: discovery is the source of truth for ids ----
  let placeID = PLACE_ID;
  group('discovery offers a place', () => {
    if (placeID) {
      return;
    }
    const list = getJSON(http, `/v1/places?near=${NEAR}&radius=5000&limit=5`);
    check(list, {
      'discovery answers': (r) => r.status === 200,
      'the catalogue has places to plan with': (r) => r.json('total') > 0,
    });
    const rows = list.json('data') || [];
    if (rows.length === 0) {
      fail('no places near the locator: seed the catalogue, or set PLACE_ID');
    }
    placeID = rows[0].coPlace;
  });

  // ---- the plan: created empty, owned by the caller ----
  let planID = '';
  group('plan is created and owned', () => {
    const created = post('/v1/plans', { name: `K6 plan ${Date.now()}`, partySize: 4 }, bearer(token));
    check(created, {
      'plan created': (r) => r.status === 201,
      'the creator is OWNER': (r) => r.json('viewerRole') === 'OWNER',
      'a plan is PRIVATE by default': (r) => r.json('visibility') === 'PRIVATE',
    });
    planID = created.json('coRoute');
  });

  // ---- the itinerary: add, vote, and read the ranking back ----
  group('a place joins the plan and the vote is counted', () => {
    const added = post(`/v1/plans/${planID}/places`, { coPlace: placeID, order: 1 }, bearer(token));
    check(added, { 'place added': (r) => r.status === 204 });

    const voted = post(`/v1/plans/${planID}/places/${placeID}/votes`, { vote: 1 }, bearer(token));
    check(voted, { 'vote recorded': (r) => r.status === 204 });

    // The itinerary is a member read: without the bearer it answers 401, which is exactly what the
    // first version of this script did and what the failing checks caught.
    const places = http.get(`${BASE_URL}/v1/plans/${planID}/places`, { headers: bearer(token) });
    check(places, {
      'itinerary reads back': (r) => r.status === 200,
      'the place is in it': (r) => (r.json('data') || []).some((row) => row.coPlace === placeID),
      'the vote is counted': (r) => {
        const row = (r.json('data') || []).find((p) => p.coPlace === placeID);
        return row && row.netVotes === 1 && row.myVote === 1;
      },
    });
  });

  // ---- the live subscription: private channel, signed by the API ----
  group('the plan channel can be subscribed to', () => {
    const signed = post('/v1/realtime/auth', {
      socketId: '123.456',
      channel: `private-plan-${planID}`,
    }, bearer(token));
    // 404 is the correct answer when the broker is not configured: the route is absent rather than
    // accepted and failed. A 500 would be the regression this check exists for.
    check(signed, {
      'subscription is signed or realtime is absent': (r) => r.status === 200 || r.status === 404,
      'a signed answer carries auth': (r) => r.status !== 200 || typeof r.json('auth') === 'string',
    });
  });

  // ---- a non-member must not be able to shape someone else's plan ----
  group('the plan stays private to strangers', () => {
    const anonymous = getJSON(http, `/v1/plans/${planID}`);
    check(anonymous, { 'no token means no plan': (r) => r.status === 401 });
  });
}
