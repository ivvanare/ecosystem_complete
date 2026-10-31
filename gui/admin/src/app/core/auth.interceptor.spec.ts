import {
  HttpClient,
  provideHttpClient,
  withInterceptors,
} from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { API_URL } from './api-url.token';
import { authInterceptor } from './auth.interceptor';
import { AuthService, type Me, type TokenPairResponse } from './auth.service';

const API = 'https://api.test/v1';

/** One macrotask: drains the microtasks chained off a flushed response. */
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

function tokenPair(overrides: Partial<TokenPairResponse> = {}): TokenPairResponse {
  return {
    access_token: 'access-1',
    token_type: 'Bearer',
    expires_in: 900,
    refresh_expires_in: 2_592_000,
    csrf_token: 'csrf-1',
    ...overrides,
  };
}

const ME: Me = {
  id: '9f1c4d2e-0000-4000-8000-000000000001',
  name: 'Ana',
  email: 'ana@melia.test',
  status: 'ACTIVE',
  email_verified_at: '2026-03-15T10:00:00Z',
  roles: ['EDITOR'],
  permissions: ['admin.access'],
  created_at: '2026-03-15T10:00:00Z',
};

const UNAUTHORIZED = {
  body: { error: { code: 'unauthorized', message: 'Authentication is required.' } },
  opts: { status: 401, statusText: 'Unauthorized' },
};

function unauthorized(code = 'unauthorized') {
  return {
    body: { error: { code, message: `${code} happened` } },
    opts: { status: 401, statusText: 'Unauthorized' },
  };
}

describe('authInterceptor', () => {
  let http: HttpClient;
  let httpMock: HttpTestingController;
  let auth: AuthService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
      ],
    });
    http = TestBed.inject(HttpClient);
    httpMock = TestBed.inject(HttpTestingController);
    auth = TestBed.inject(AuthService);
  });

  afterEach(() => httpMock.verify());

  /** Opens a session through the full stack so the interceptor has a token. */
  async function login(): Promise<void> {
    const pending = auth.login('ana@melia.test', 'una-clave-larga-123');
    httpMock.expectOne(`${API}/auth/login`).flush(tokenPair());
    await tick();
    httpMock.expectOne(`${API}/me`).flush(ME);
    await pending;
  }

  it('attaches the Bearer token to API requests only (S7)', async () => {
    await login();

    const apiCall = firstValueFrom(http.get(`${API}/faqs`));
    const apiReq = httpMock.expectOne(`${API}/faqs`);
    expect(apiReq.request.headers.get('Authorization')).toBe('Bearer access-1');
    apiReq.flush({ data: [] });

    const outside = firstValueFrom(http.get('https://other.example.com/data'));
    const outsideReq = httpMock.expectOne('https://other.example.com/data');
    expect(outsideReq.request.headers.has('Authorization')).toBe(false);
    expect(outsideReq.request.withCredentials).toBe(false);
    outsideReq.flush({ ok: true });

    await Promise.all([apiCall, outside]);
  });

  it('sends API requests bare while there is no session in memory', async () => {
    const call = firstValueFrom(http.get(`${API}/faqs`));
    const req = httpMock.expectOne(`${API}/faqs`);
    expect(req.request.headers.has('Authorization')).toBe(false);
    req.flush({ data: [] });
    await call;
  });

  it('sends credentials on /auth/* calls and never attaches a Bearer to them', async () => {
    const loginCall = firstValueFrom(
      http.post(`${API}/auth/login`, { email: 'a@b.c', password: 'x' }),
    );
    const loginReq = httpMock.expectOne(`${API}/auth/login`);
    expect(loginReq.request.withCredentials).toBe(true);
    expect(loginReq.request.headers.has('Authorization')).toBe(false);
    loginReq.flush(tokenPair());
    await loginCall;

    // A /v10 sibling must not even be recognised as the API.
    const sibling = firstValueFrom(http.get(`${API}0/ten`));
    const siblingReq = httpMock.expectOne(`${API}0/ten`);
    expect(siblingReq.request.headers.has('Authorization')).toBe(false);
    expect(siblingReq.request.withCredentials).toBe(false);
    siblingReq.flush({});
    await sibling;
  });

  it('on a 401 refreshes once and retries the request with the new token', async () => {
    await login();

    const call = firstValueFrom(http.get(`${API}/faqs`));
    httpMock.expectOne(`${API}/faqs`).flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);

    httpMock
      .expectOne(`${API}/auth/refresh`)
      .flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));
    await tick();

    const retry = httpMock.expectOne(`${API}/faqs`);
    expect(retry.request.headers.get('Authorization')).toBe('Bearer access-2');
    retry.flush({ data: [{ id: 1 }] });

    await expect(call).resolves.toEqual({ data: [{ id: 1 }] });
    expect(auth.accessToken()).toBe('access-2');
  });

  it('N concurrent 401s produce exactly one refresh and all N retries succeed (S3)', async () => {
    await login();

    const urls = [`${API}/faqs`, `${API}/admin/roles`, `${API}/admin/permissions`];
    const calls = urls.map((url) => ({ url, promise: firstValueFrom(http.get(url)) }));
    for (const url of urls) {
      httpMock.expectOne(url).flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);
    }

    // One rotation for the whole burst: two parallel refreshes would replay the
    // rotated cookie and the API would revoke the whole family as theft.
    httpMock
      .expectOne(`${API}/auth/refresh`)
      .flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));
    await tick();

    for (const { url } of calls) {
      const retry = httpMock.expectOne(url);
      expect(retry.request.headers.get('Authorization')).toBe('Bearer access-2');
      retry.flush({ data: [] });
    }
    for (const { promise } of calls) {
      await expect(promise).resolves.toEqual({ data: [] });
    }
    httpMock.expectNone(`${API}/auth/refresh`);
  });

  it('retries once and gives up: a second 401 propagates, with no second refresh', async () => {
    await login();

    const call = firstValueFrom(http.get(`${API}/faqs`));
    httpMock.expectOne(`${API}/faqs`).flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);
    httpMock
      .expectOne(`${API}/auth/refresh`)
      .flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));
    await tick();
    httpMock.expectOne(`${API}/faqs`).flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);

    await expect(call).rejects.toMatchObject({ status: 401 });
    httpMock.expectNone(`${API}/auth/refresh`);
  });

  it('a 401 on the refresh itself does not recurse and drops the session', async () => {
    await login();

    const call = firstValueFrom(http.get(`${API}/faqs`));
    httpMock.expectOne(`${API}/faqs`).flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);
    // The family is already dead: the answer must propagate, never loop.
    httpMock
      .expectOne(`${API}/auth/refresh`)
      .flush(unauthorized('refresh_reused').body, unauthorized('refresh_reused').opts);

    await expect(call).rejects.toMatchObject({ status: 401 });
    await tick();
    httpMock.expectNone(`${API}/auth/refresh`);
    expect(auth.status()).toBe('anonymous');
  });

  it('/auth/* failures never trigger a refresh (login 401, logout 403)', async () => {
    const loginCall = firstValueFrom(
      http.post(`${API}/auth/login`, { email: 'a@b.c', password: 'bad' }),
    );
    httpMock
      .expectOne(`${API}/auth/login`)
      .flush(unauthorized('invalid_credentials').body, unauthorized('invalid_credentials').opts);
    await expect(loginCall).rejects.toMatchObject({ status: 401 });

    const logoutCall = firstValueFrom(http.post(`${API}/auth/logout`, {}));
    httpMock.expectOne(`${API}/auth/logout`).flush(
      { error: { code: 'csrf_invalid', message: 'The CSRF token is missing or does not match.' } },
      { status: 403, statusText: 'Forbidden' },
    );
    await expect(logoutCall).rejects.toMatchObject({ status: 403 });

    await tick();
    httpMock.expectNone(`${API}/auth/refresh`);
  });

  it('ignores 401s from outside the API', async () => {
    const call = firstValueFrom(http.get('https://other.example.com/private'));
    httpMock
      .expectOne('https://other.example.com/private')
      .flush(UNAUTHORIZED.body, UNAUTHORIZED.opts);

    await expect(call).rejects.toMatchObject({ status: 401 });
    await tick();
    httpMock.expectNone(`${API}/auth/refresh`);
  });
});
