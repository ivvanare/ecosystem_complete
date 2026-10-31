import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { vi } from 'vitest';
import { API_URL } from './api-url.token';
import { AuthService } from './auth.service';

const API = 'http://api.test/v1';

/**
 * L11: `provideAppInitializer` awaits the silent refresh, so a request that never
 * answers used to hold the app hostage — in dev it reads as "the panel does not
 * load". The rotation is bounded, so the worst case is landing on /login.
 */
describe('AuthService boot bound (L11)', () => {
  let auth: AuthService;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    sessionStorage.clear();
    vi.useFakeTimers();
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: API_URL, useValue: API },
      ],
    });
    auth = TestBed.inject(AuthService);
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it('resolves anonymous instead of hanging when the refresh never answers', async () => {
    const boot = auth.restoreSession();
    const refresh = httpMock.expectOne(`${API}/auth/refresh`);

    await vi.advanceTimersByTimeAsync(8_001);

    await expect(boot).resolves.toBe(false);
    expect(auth.status()).toBe('anonymous');
    // The bound unsubscribes the rotation instead of leaving it dangling.
    expect(refresh.cancelled).toBe(true);
  });
});
