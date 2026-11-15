// Photos: the only route where the size of the body is part of the contract.
//
//   ADMIN_EMAIL=ana@melia.test ADMIN_PASSWORD='...' k6 run tests/k6/photos.js
//
// It uploads through the real multipart path and asserts both edges: a file inside the 5 MB
// budget is stored and linked, and a body past it is refused with the field named. This is the
// route where a missing limit is a denial of service with extra steps, and nothing in `go test`
// sees the memory a 5 MB part costs.
//
// It WRITES: an image on disk, a row for it, and the gallery of the place it picks. It cleans up
// after itself, but do not point it at a catalogue you care about.
//
// Why it needs credentials: uploading requires `place.update` scoped by ownership, and a fresh
// signup has no role. Grant one first, as AGENTS.md documents:
//
//   docker compose run --rm --entrypoint /usr/local/bin/bootstrap \
//     -e EMAIL=ana@melia.test -e ROLE=SUPERADMIN api
import http from 'k6/http';
import { check, fail } from 'k6';
import { BASE_URL, JSON_HEADERS, postJSON } from './lib/config.js';

// 201 is the upload; 422 is the refusal the size check asserts; 404 is a photo already gone.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 401, 404, 422));

export const options = {
  iterations: 1,
  thresholds: {
    // The upload is a 200 KB body, not the 5 MB one: the big body is expected to be refused, and
    // timing a rejection would measure nothing useful.
    checks: ['rate>0.99'],
  },
};

const ADMIN_EMAIL = __ENV.ADMIN_EMAIL || '';
const ADMIN_PASSWORD = __ENV.ADMIN_PASSWORD || '';
const LIMIT_BYTES = 5 * 1024 * 1024;

function bearer(token) {
  return { ...JSON_HEADERS, Authorization: `Bearer ${token}` };
}

// fakeJPEG is what the handler accepts when the part declares a type it allows: the bytes are
// verified only when the declaration is not one of jpeg/png/webp, so this exercises the fast path
// without shipping a binary in the repository.
function fakeJPEG(bytes) {
  return 'x'.repeat(bytes);
}

export default function () {
  if (!ADMIN_EMAIL || !ADMIN_PASSWORD) {
    fail(
      'set ADMIN_EMAIL and ADMIN_PASSWORD to an account holding place.update. ' +
        'Grant one with: docker compose run --rm --entrypoint /usr/local/bin/bootstrap ' +
        "-e EMAIL=<email> -e ROLE=SUPERADMIN api",
    );
  }

  const login = postJSON(http, '/v1/auth/login', { email: ADMIN_EMAIL, password: ADMIN_PASSWORD });
  if (!check(login, { 'admin logs in': (r) => r.status === 200 })) {
    fail(`login as ${ADMIN_EMAIL} failed: ${login.status} ${login.body}`);
  }
  const token = login.json('access_token');

  // A place this account may edit. With place.manage_all the first row will do; an owner-editor
  // gets their own, which is the same call.
  const places = http.get(`${BASE_URL}/v1/admin/places?limit=1`, { headers: bearer(token) });
  if (!check(places, { 'the backoffice lists a place': (r) => r.status === 200 })) {
    fail(`listing places failed: ${places.status} ${places.body}`);
  }
  const rows = places.json('data') || [];
  if (rows.length === 0) {
    fail('no place to attach a photo to: create one from the panel or the seed');
  }
  const placeID = rows[0].coPlace;

  // ---- inside the budget: stored, linked, and readable back ----
  let photoID = '';
  const small = http.post(
    `${BASE_URL}/v1/admin/places/${placeID}/photos`,
    {
      file: http.file(fakeJPEG(200 * 1024), 'k6.jpg', 'image/jpeg'),
      type: 'GALLERY',
      order: '1',
    },
    { headers: { Authorization: `Bearer ${token}` } },
  );
  check(small, {
    'upload is 201': (r) => r.status === 201,
    'upload returns the photo': (r) => typeof r.json('coPhoto') === 'string',
    'upload returns a URL': (r) => typeof r.json('url') === 'string' && r.json('url').length > 0,
    'upload keeps the declared kind': (r) => r.json('type') === 'GALLERY',
  });
  photoID = small.json('coPhoto') || '';

  // ---- past the budget: refused by name, not silently truncated ----
  // The multipart envelope adds a few hundred bytes, so the file alone at the limit is enough to
  // cross it: that is the point of asserting on the body, not on the file.
  const big = http.post(
    `${BASE_URL}/v1/admin/places/${placeID}/photos`,
    { file: http.file(fakeJPEG(LIMIT_BYTES + 1024), 'big.jpg', 'image/jpeg') },
    { headers: { Authorization: `Bearer ${token}` } },
  );
  check(big, {
    'an oversized upload is 422': (r) => r.status === 422,
    'the refusal names the file': (r) => {
      const fields = r.json('error.details.fields') || [];
      return fields.some((f) => f.field === 'file');
    },
  });

  // ---- the gallery replacement, over the wire ----
  if (photoID) {
    const replaced = http.put(
      `${BASE_URL}/v1/admin/places/${placeID}/photos`,
      JSON.stringify({ photos: [{ coPhoto: photoID, type: 'COVER', order: 0 }] }),
      { headers: bearer(token) },
    );
    check(replaced, { 'the gallery is replaced': (r) => r.status === 204 });

    const listed = http.get(`${BASE_URL}/v1/admin/places/${placeID}`, { headers: bearer(token) });
    check(listed, {
      'the place reads back': (r) => r.status === 200,
    });

    // ---- cleanup: retire what this run created ----
    const removed = http.del(
      `${BASE_URL}/v1/admin/places/${placeID}/photos/${photoID}`,
      null,
      { headers: bearer(token) },
    );
    check(removed, { 'the photo is retired': (r) => r.status === 204 });
  }
}
