import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import {
  provideRouter,
  Router,
  type ActivatedRouteSnapshot,
  type RouterStateSnapshot,
  type UrlTree,
} from '@angular/router';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { API_URL } from './api-url.token';
import { authGuard } from './auth.guard';
import { AuthService, type Me, type TokenPairResponse } from './auth.service';

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

describe('authGuard', () => {
  let auth: AuthService;
  let router: Router;
  let http: HttpTestingController;

  beforeEach(() => {
    sessionStorage.clear();
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
      ],
    });
    auth = TestBed.inject(AuthService);
    router = TestBed.inject(Router);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  /** Runs the guard the way the router does: inside an injection context. */
  function run(attemptedUrl: string): boolean | UrlTree {
    return TestBed.runInInjectionContext(
      () =>
        authGuard({} as ActivatedRouteSnapshot, {
          url: attemptedUrl,
        } as RouterStateSnapshot) as boolean | UrlTree,
    );
  }

  function expectRedirectToLogin(result: boolean | UrlTree, returnUrl: string): void {
    expect(result).not.toBe(true);
    const expected = router.createUrlTree(['/login'], { queryParams: { returnUrl } });
    expect(router.serializeUrl(result as UrlTree)).toBe(router.serializeUrl(expected));
  }

  it('lets an authenticated session straight through', async () => {
    const pending = auth.login('ana@melia.test', 'una-clave-larga-123');
    http.expectOne(`${API}/auth/login`).flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await pending;
    expect(auth.status()).toBe('authenticated');

    expect(run('/faqs')).toBe(true);
  });

  it('bounces an anonymous visitor to /login with the attempted URL as returnUrl', async () => {
    // Settled anonymous: the silent restore already ran and found no live session.
    const restore = auth.restoreSession();
    http.expectOne(`${API}/auth/refresh`).flush(
      { error: { code: 'refresh_invalid', message: 'refresh_invalid happened' } },
      { status: 401, statusText: 'Unauthorized' },
    );
    await restore;
    expect(auth.status()).toBe('anonymous');

    expectRedirectToLogin(run('/faqs'), '/faqs');
  });

  it('keeps the full attempted URL — query string included — as the returnUrl', () => {
    expectRedirectToLogin(run('/faqs?category=general'), '/faqs?category=general');
  });

  it('redirects even while the status is unknown: no free pass before boot settles', () => {
    // provideAppInitializer settles the status before the first navigation, so
    // this branch is defensive at runtime — but it must never wave anyone through.
    expect(auth.status()).toBe('unknown');
    expectRedirectToLogin(run('/faqs'), '/faqs');
  });
});
