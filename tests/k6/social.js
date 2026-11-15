// Social: the consumer surface a phone will actually use — reviews, follow, check-in, block and
// notification preferences — in one journey.
//
//   k6 run tests/k6/social.js
//
// The three rules this exists to protect are the ones a fake cannot see:
//   - a review is unique per person and place, and the partial index says so (`409`);
//   - `viaFollowedAuthor` is viewer-relative: the same review answers false to a stranger and true
//     to someone who follows its author;
//   - a block hides content in both directions, and a check-in outside the radius is refused with
//     `not_near_place` instead of being recorded.
//
// It WRITES two accounts, two reviews, a follow, a block and one check-in per run. The check-in is
// append-only and stays; everything else is cleaned up at the end.
import http from 'k6/http';
import { check, fail, group } from 'k6';
import {
  BASE_URL,
  JSON_HEADERS,
  PASSWORD,
  postJSON,
  uniqueEmail,
  waitForCode,
} from './lib/config.js';

// The expected refusals are part of the evidence, not failures: 409 is the unique review and the
// day bucket, 422 is the radius and the self-follow.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 409, 422));

export const options = {
  iterations: 1,
  thresholds: { checks: ['rate>0.99'] },
};

const NEAR = __ENV.K6_NEAR || '10.496,-66.853';

function bearer(token) {
  return { ...JSON_HEADERS, Authorization: `Bearer ${token}` };
}

function post(path, body, token) {
  return http.post(`${BASE_URL}${path}`, JSON.stringify(body), { headers: bearer(token) });
}

function get(path, token) {
  return http.get(`${BASE_URL}${path}`, { headers: bearer(token) });
}

function patch(path, body, token) {
  return http.patch(`${BASE_URL}${path}`, JSON.stringify(body), { headers: bearer(token) });
}

function del(path, token) {
  return http.del(`${BASE_URL}${path}`, null, { headers: bearer(token) });
}

// register creates one account and returns its token and id.
function register(prefix) {
  const email = uniqueEmail(prefix);
  postJSON(http, '/v1/auth/signup', { email, name: prefix, password: PASSWORD });
  const code = waitForCode(http, email);
  if (!code) {
    fail(`no signup code for ${email}: Mailpit or the worker is not running`);
  }
  const verified = postJSON(http, '/v1/auth/signup/verify', { email, code });
  if (!check(verified, { [`${prefix} is registered`]: (r) => r.status === 201 })) {
    fail(`${prefix} could not register: ${verified.status} ${verified.body}`);
  }
  const token = verified.json('access_token');
  const me = get('/v1/me', token);
  return { token, id: me.json('id') };
}

export default function () {
  const alice = register('alice');
  const bob = register('bob');

  // A real place from discovery, with the coordinates the check-in radius is measured against.
  const list = http.get(`${BASE_URL}/v1/places?near=${NEAR}&radius=5000&limit=1`);
  const rows = list.json('data') || [];
  if (rows.length === 0) {
    fail('no place near the locator: seed the catalogue, or set K6_NEAR');
  }
  const place = rows[0];

  let aliceReview = '';
  group('a review is unique per person and place', () => {
    const created = post(`/v1/places/${place.coPlace}/reviews`, { rating: 5, comment: 'k6 alice' }, alice.token);
    check(created, {
      'review created': (r) => r.status === 201,
      'review carries the rating': (r) => r.json('rating') === 5,
      'a fresh review is not via a followed author': (r) => r.json('viaFollowedAuthor') === false,
    });
    aliceReview = created.json('coReview') || '';

    const again = post(`/v1/places/${place.coPlace}/reviews`, { rating: 4 }, alice.token);
    check(again, {
      'a second review is 409': (r) => r.status === 409,
      'the refusal is review_already_exists': (r) => r.json('error.code') === 'review_already_exists',
    });

    // Bob writes his own, so the block below has content to hide.
    const bobs = post(`/v1/places/${place.coPlace}/reviews`, { rating: 3, comment: 'k6 bob' }, bob.token);
    check(bobs, { 'a second person reviews the same place': (r) => r.status === 201 });
  });

  group('the highlight is viewer-relative', () => {
    const stranger = get(`/v1/places/${place.coPlace}/reviews`, bob.token);
    const before = (stranger.json('data') || []).find((r) => r.coReview === aliceReview);
    check(before || {}, {
      'a stranger sees the review': (r) => r.coReview === aliceReview,
      'a stranger does not get the highlight': (r) => r.viaFollowedAuthor === false,
    });

    const followed = post('/v1/follows', { coUser: alice.id }, bob.token);
    check(followed, { 'following is idempotent 204': (r) => r.status === 204 });
    const again = post('/v1/follows', { coUser: alice.id }, bob.token);
    check(again, { 'following twice is still 204': (r) => r.status === 204 });

    const now = get(`/v1/places/${place.coPlace}/reviews`, bob.token);
    const after = (now.json('data') || []).find((r) => r.coReview === aliceReview);
    check(after || {}, {
      'the highlight follows the viewer': (r) => r.viaFollowedAuthor === true,
    });

    const self = post('/v1/follows', { coUser: bob.id }, bob.token);
    check(self, {
      'self-follow is 422': (r) => r.status === 422,
    });
  });

  group('a check-in is bounded by the place radius', () => {
    const inside = post(`/v1/places/${place.coPlace}/checkins`,
      { near: `${place.latitude},${place.longitude}` }, bob.token);
    check(inside, {
      'a check-in near the place is 201': (r) => r.status === 201,
      'the proximity claim is recorded as a verdict': (r) => typeof r.json('verification') === 'string',
    });

    const twice = post(`/v1/places/${place.coPlace}/checkins`,
      { near: `${place.latitude},${place.longitude}` }, bob.token);
    check(twice, {
      'a second check-in the same day is 409': (r) => r.status === 409,
      'the refusal is already_checked_in': (r) => r.json('error.code') === 'already_checked_in',
    });

    // A degree of latitude is ~111 km: far outside any radius the catalogue carries.
    const far = post(`/v1/places/${place.coPlace}/checkins`,
      { near: `${place.latitude + 1},${place.longitude}` }, bob.token);
    check(far, {
      'a check-in from far away is 422': (r) => r.status === 422,
      'the refusal is not_near_place': (r) => r.json('error.code') === 'not_near_place',
    });
  });

  group('a block hides content in both directions', () => {
    const blocked = post('/v1/blocks', { coUser: bob.id }, alice.token);
    check(blocked, { 'blocking is idempotent 204': (r) => r.status === 204 });

    const visible = get(`/v1/places/${place.coPlace}/reviews`, alice.token);
    check(visible, {
      'the blocked person disappears from the list': (r) => {
        const rowsNow = r.json('data') || [];
        return rowsNow.every((review) => review.author.id !== bob.id);
      },
    });

    const blocks = get('/v1/blocks', alice.token);
    check(blocks, {
      'the block is listed': (r) => (r.json('data') || []).some((row) => row.coUser === bob.id),
    });

    check(del(`/v1/blocks/${bob.id}`, alice.token), { 'unblocking is 204': (r) => r.status === 204 });
  });

  group('notification preferences are enumerable and reversible', () => {
    const before = get('/v1/notifications/preferences', bob.token);
    check(before, {
      'every kind is listed': (r) => (r.json('data') || []).length > 0,
    });

    // PATCH, not POST: the route answers 405 to the wrong verb, and a check written against the
    // wrong verb would have been green on a route that does nothing.
    const off = patch('/v1/notifications/preferences', { kind: 'invitation.received', enabled: false }, bob.token);
    check(off, { 'turning a kind off is 204': (r) => r.status === 204 });

    const after = get('/v1/notifications/preferences', bob.token);
    check(after, {
      'the kind reads back disabled': (r) => {
        const row = (r.json('data') || []).find((p) => p.kind === 'invitation.received');
        return row !== undefined && row.enabled === false;
      },
    });

    const on = patch('/v1/notifications/preferences', { kind: 'invitation.received', enabled: true }, bob.token);
    check(on, { 'turning it back on is 204': (r) => r.status === 204 });
  });

  // ---- cleanup: reviews are ours to remove; the check-in is append-only and stays ----
  if (aliceReview) {
    del(`/v1/places/${place.coPlace}/reviews/${aliceReview}`, alice.token);
  }
  del(`/v1/follows/${alice.id}`, bob.token);
}
