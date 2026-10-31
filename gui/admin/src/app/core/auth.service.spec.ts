import { HttpErrorResponse, provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API_URL } from './api-url.token';
import { AuthService, CSRF_STORAGE_KEY, type Me, type TokenPairResponse } from './auth.service';

const API = 'https://api.test/v1';

/** One macrotask: drains every microtask chained off a flushed response. */
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

function apiError(status: number, code: string): [object, { status: number; statusText: string }] {
  return [{ error: { code, message: `${code} happened` } }, { status, statusText: String(status) }];
}

describe('AuthService', () => {
  let auth: AuthService;
  let http: HttpTestingController;

  beforeEach(() => {
    // Storage survives a reload by design (L9) — so it must not leak between tests either.
    sessionStorage.clear();
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    });
    auth = TestBed.inject(AuthService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
  });

  /** Drives a full login through the testing backend. */
  async function login(): Promise<void> {
    const pending = auth.login('ana@melia.test', 'una-clave-larga-123');
    http.expectOne(`${API}/auth/login`).flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await pending;
  }

  it('starts unknown, with nothing in memory', () => {
    expect(auth.status()).toBe('unknown');
    expect(auth.accessToken()).toBeNull();
    expect(auth.csrfToken()).toBeNull();
    expect(auth.user()).toBeNull();
  });

  it('login uses the cookie transport and lands authenticated with the profile', async () => {
    const pending = auth.login('ana@melia.test', 'una-clave-larga-123');

    const req = http.expectOne(`${API}/auth/login`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ email: 'ana@melia.test', password: 'una-clave-larga-123' });
    // The browser transport: credentials let the Set-Cookie pair land (S2).
    expect(req.request.withCredentials).toBe(true);
    req.flush(tokenPair());

    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await pending;

    expect(auth.status()).toBe('authenticated');
    expect(auth.accessToken()).toBe('access-1');
    expect(auth.csrfToken()).toBe('csrf-1');
    expect(auth.user()).toEqual(ME);
  });

  it('a rejected login propagates the API error and changes no state', async () => {
    const pending = auth.login('ana@melia.test', 'wrong');
    http.expectOne(`${API}/auth/login`).flush(...apiError(401, 'invalid_credentials'));

    await expect(pending).rejects.toBeInstanceOf(HttpErrorResponse);
    expect(auth.status()).toBe('unknown');
    expect(auth.accessToken()).toBeNull();
  });

  it('me() fetches the profile that carries the permissions', async () => {
    const pending = auth.me();
    const req = http.expectOne(`${API}/me`);
    expect(req.request.method).toBe('GET');
    req.flush(ME);

    await expect(pending).resolves.toEqual(ME);
    expect(auth.user()).toEqual(ME);
  });

  it('refresh rotates the pair and echoes the stored CSRF token as a header (S2)', async () => {
    await login();

    const pending = auth.refresh();
    const req = http.expectOne(`${API}/auth/refresh`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({});
    expect(req.request.withCredentials).toBe(true);
    expect(req.request.headers.get('X-CSRF-Token')).toBe('csrf-1');
    req.flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));

    await expect(pending).resolves.toBe('access-2');
    expect(auth.accessToken()).toBe('access-2');
    // The rotated value replaces the old one; the next call must echo csrf-2.
    expect(auth.csrfToken()).toBe('csrf-2');
  });

  it('shares one in-flight refresh between concurrent callers (S3)', async () => {
    const first = auth.refresh();
    const second = auth.refresh();
    const third = auth.refresh();

    // A single rotation for the whole burst: a second parallel call would present
    // the just-rotated cookie and the API would revoke the family as theft.
    const req = http.expectOne(`${API}/auth/refresh`);
    req.flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));

    await expect(first).resolves.toBe('access-2');
    await expect(second).resolves.toBe('access-2');
    await expect(third).resolves.toBe('access-2');
    http.expectNone(`${API}/auth/refresh`);
  });

  it('a fresh rotation runs once the shared one settles', async () => {
    const first = auth.refresh();
    http.expectOne(`${API}/auth/refresh`).flush(tokenPair());
    await first;

    const second = auth.refresh();
    http.expectOne(`${API}/auth/refresh`).flush(tokenPair({ access_token: 'access-2' }));
    await expect(second).resolves.toBe('access-2');
  });

  it('drops to anonymous when the refresh is rejected: the family is dead', async () => {
    await login();

    const pending = auth.refresh();
    http.expectOne(`${API}/auth/refresh`).flush(...apiError(401, 'refresh_reused'));

    await expect(pending).rejects.toBeInstanceOf(HttpErrorResponse);
    expect(auth.status()).toBe('anonymous');
    expect(auth.accessToken()).toBeNull();
    expect(auth.csrfToken()).toBeNull();
    expect(auth.user()).toBeNull();
    // A dead family also drops the persisted double-submit value (L9).
    expect(sessionStorage.getItem(CSRF_STORAGE_KEY)).toBeNull();
  });

  it('restoreSession refreshes silently and lands authenticated with the profile', async () => {
    const pending = auth.restoreSession();

    http.expectOne(`${API}/auth/refresh`).flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);

    await expect(pending).resolves.toBe(true);
    expect(auth.status()).toBe('authenticated');
    expect(auth.accessToken()).toBe('access-1');
    expect(auth.user()?.email).toBe('ana@melia.test');
  });

  it('restoreSession lands anonymous, quietly, when there is no live session', async () => {
    const pending = auth.restoreSession();
    http.expectOne(`${API}/auth/refresh`).flush(...apiError(401, 'refresh_invalid'));

    await expect(pending).resolves.toBe(false);
    expect(auth.status()).toBe('anonymous');
    http.expectNone(`${API}/me`);
  });

  it('boot echoes the CSRF value kept in sessionStorage, so the silent refresh is no 403 (L9)', async () => {
    sessionStorage.setItem(CSRF_STORAGE_KEY, 'csrf-boot');
    // A fresh instance is a fresh page load: memory is gone, storage survived.
    const booted = TestBed.runInInjectionContext(() => new AuthService());
    expect(booted.accessToken()).toBeNull();
    expect(booted.csrfToken()).toBe('csrf-boot');

    const pending = booted.restoreSession();
    const req = http.expectOne(`${API}/auth/refresh`);
    // The header the API compares against the CSRF cookie: without it the boot
    // refresh answers 403 csrf_invalid and the panel would fall to /login (L9).
    expect(req.request.headers.get('X-CSRF-Token')).toBe('csrf-boot');
    req.flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);

    await expect(pending).resolves.toBe(true);
    expect(booted.status()).toBe('authenticated');
    // Rotation overwrote the stored value, so the next reload echoes csrf-1.
    expect(sessionStorage.getItem(CSRF_STORAGE_KEY)).toBe('csrf-1');
  });

  it('a failed restore drops the stored CSRF value too', async () => {
    sessionStorage.setItem(CSRF_STORAGE_KEY, 'csrf-stale');
    const booted = TestBed.runInInjectionContext(() => new AuthService());

    const pending = booted.restoreSession();
    http.expectOne(`${API}/auth/refresh`).flush(...apiError(401, 'refresh_invalid'));

    await expect(pending).resolves.toBe(false);
    expect(booted.status()).toBe('anonymous');
    expect(sessionStorage.getItem(CSRF_STORAGE_KEY)).toBeNull();
  });

  it('logout echoes the current CSRF token, then clears the local session', async () => {
    await login();
    // Rotate once so the echoed value proves it tracks the latest pair.
    const rotated = auth.refresh();
    http
      .expectOne(`${API}/auth/refresh`)
      .flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));
    await rotated;

    const pending = auth.logout();
    const req = http.expectOne(`${API}/auth/logout`);
    expect(req.request.method).toBe('POST');
    expect(req.request.withCredentials).toBe(true);
    expect(req.request.headers.get('X-CSRF-Token')).toBe('csrf-2');
    req.flush(null, { status: 204, statusText: 'No Content' });

    await pending;
    expect(auth.status()).toBe('anonymous');
    expect(auth.accessToken()).toBeNull();
    expect(auth.csrfToken()).toBeNull();
    expect(auth.user()).toBeNull();
    expect(sessionStorage.getItem(CSRF_STORAGE_KEY)).toBeNull();
  });

  it('logout clears the local session even when the request fails', async () => {
    await login();

    const pending = auth.logout();
    http.expectOne(`${API}/auth/logout`).flush(...apiError(500, 'internal_error'));

    await expect(pending).rejects.toBeInstanceOf(HttpErrorResponse);
    expect(auth.status()).toBe('anonymous');
    expect(auth.accessToken()).toBeNull();
  });

  it('persists only the CSRF value — the access token never touches storage (L4 + L9)', async () => {
    const setItem = vi.spyOn(Storage.prototype, 'setItem');

    await login();
    const rotated = auth.refresh();
    http
      .expectOne(`${API}/auth/refresh`)
      .flush(tokenPair({ access_token: 'access-2', csrf_token: 'csrf-2' }));
    await rotated;

    // Exactly one key may ever be written, overwritten on each rotation (L9).
    expect(setItem).toHaveBeenCalled();
    for (const [key] of setItem.mock.calls) {
      expect(key).toBe(CSRF_STORAGE_KEY);
    }
    expect(sessionStorage.getItem(CSRF_STORAGE_KEY)).toBe('csrf-2');
    expect(sessionStorage.length).toBe(1);
    expect(localStorage.length).toBe(0);

    // The 15-minute access token stays memory-only (L4): no stored value may be it.
    const stored = [sessionStorage, localStorage].flatMap((store) =>
      Array.from({ length: store.length }, (_, i) => store.getItem(store.key(i) as string)),
    );
    expect(stored).not.toContain('access-1');
    expect(stored).not.toContain('access-2');
  });
});
