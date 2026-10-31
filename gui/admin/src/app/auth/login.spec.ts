import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { type ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, provideRouter, Router } from '@angular/router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API_URL } from '../core/api-url.token';
import type { Me, TokenPairResponse } from '../core/auth.service';
import { Login } from './login';

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

describe('Login', () => {
  let fixture: ComponentFixture<Login>;
  let http: HttpTestingController;
  let router: Router;
  let returnUrl: string | null;

  beforeEach(async () => {
    sessionStorage.clear();
    localStorage.clear();
    returnUrl = null;
    await TestBed.configureTestingModule({
      imports: [Login],
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        // The component reads the query param on a successful submit; the factory
        // resolves whatever the test set before creating it.
        {
          provide: ActivatedRoute,
          useFactory: () => ({
            snapshot: { queryParamMap: convertToParamMap(returnUrl ? { returnUrl } : {}) },
          }),
        },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
    vi.spyOn(router, 'navigateByUrl').mockResolvedValue(true);
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
  });

  function create(): void {
    fixture = TestBed.createComponent(Login);
    fixture.detectChanges();
  }

  function fill(selector: string, value: string): void {
    const input = fixture.nativeElement.querySelector(selector) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  function submitForm(): void {
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(
      new Event('submit'),
    );
    fixture.detectChanges();
  }

  function submitButton(): HTMLButtonElement {
    return fixture.nativeElement.querySelector('button[type="submit"]') as HTMLButtonElement;
  }

  function alertText(): string | null {
    const alert = fixture.nativeElement.querySelector('[role="alert"]') as HTMLElement | null;
    return alert?.textContent ?? null;
  }

  async function flushSuccessfulLogin(): Promise<void> {
    http.expectOne(`${API}/auth/login`).flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await tick();
    fixture.detectChanges();
  }

  it('signs in through the cookie transport and lands on the default route (no returnUrl)', async () => {
    create();
    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
    submitForm();

    const req = http.expectOne(`${API}/auth/login`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({ email: 'ana@melia.test', password: 'una-clave-larga-123' });
    expect(req.request.withCredentials).toBe(true);
    req.flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await tick();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/faqs');
  });

  it('signs in and lands on the returnUrl the guard bounced from', async () => {
    returnUrl = '/faqs?category=general';
    create();
    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
    submitForm();
    await flushSuccessfulLogin();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/faqs?category=general');
  });

  it('refuses a returnUrl that points off-app and falls back to the default', async () => {
    returnUrl = 'https://evil.example/phish';
    create();
    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
    submitForm();
    await flushSuccessfulLogin();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/faqs');
  });

  it("shows the API's error.code — never the message — on a rejected login", async () => {
    create();
    fill('#email', 'ana@melia.test');
    fill('#password', 'wrong');
    submitForm();

    http.expectOne(`${API}/auth/login`).flush(
      { error: { code: 'invalid_credentials', message: 'Those credentials do not match.' } },
      { status: 401, statusText: 'Unauthorized' },
    );
    await tick();
    fixture.detectChanges();

    // Clients parse `code`, never `message` (api/API.md).
    expect(alertText()).toContain('invalid_credentials');
    expect(alertText()).not.toContain('Those credentials');
    expect(router.navigateByUrl).not.toHaveBeenCalled();
    // The form is ready for another attempt.
    expect(submitButton().disabled).toBe(false);
    expect(submitButton().textContent).toContain('Sign in');
  });

  it('disables the button and ignores a second submit while the request is in flight', async () => {
    create();
    // Required fields start empty: the button stays disabled until the form is valid.
    expect(submitButton().disabled).toBe(true);

    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
    expect(submitButton().disabled).toBe(false);

    submitForm();
    expect(submitButton().disabled).toBe(true);
    expect(submitButton().textContent).toContain('Signing in');

    // A second dispatch in flight must not issue a second login request.
    submitForm();
    http.expectOne(`${API}/auth/login`).flush(tokenPair());
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await tick();
    http.expectNone(`${API}/auth/login`);
  });

  it('never sends an invalid form', () => {
    create();
    submitForm(); // both fields empty: the required validators fail

    http.expectNone(`${API}/auth/login`);
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('shows a generic code when the failure carries no API error envelope', async () => {
    create();
    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
    submitForm();

    http.expectOne(`${API}/auth/login`).error(new ProgressEvent('error'));
    await tick();
    fixture.detectChanges();

    expect(alertText()).toContain('network_error');
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });
});
