import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { type ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter, Router } from '@angular/router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API_URL } from '../core/api-url.token';
import type { Me, TokenPairResponse } from '../core/auth.service';
import { Signup } from './signup';

const API = 'https://api.test/v1';

/** One macrotask: drains every microtask chained off a flushed response. */
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const PAIR: TokenPairResponse = {
  access_token: 'access-1',
  token_type: 'Bearer',
  expires_in: 900,
  refresh_expires_in: 2_592_000,
  csrf_token: 'csrf-1',
};

const ME: Me = {
  id: 'u1',
  name: 'Ana',
  email: 'ana@melia.test',
  status: 'ACTIVE',
  email_verified_at: '2026-11-01T00:00:00Z',
  roles: [],
  permissions: [],
  created_at: '2026-11-01T00:00:00Z',
};

describe('Signup', () => {
  let fixture: ComponentFixture<Signup>;
  let http: HttpTestingController;
  let router: Router;

  beforeEach(async () => {
    sessionStorage.clear();
    await TestBed.configureTestingModule({
      imports: [Signup],
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    router = TestBed.inject(Router);
    vi.spyOn(router, 'navigateByUrl').mockResolvedValue(true);
    fixture = TestBed.createComponent(Signup);
    fixture.detectChanges();
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
  });

  function fill(selector: string, value: string): void {
    const input = fixture.nativeElement.querySelector(selector) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  function submit(): void {
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(
      new Event('submit'),
    );
    fixture.detectChanges();
  }

  function text(): string {
    return (fixture.nativeElement as HTMLElement).textContent ?? '';
  }

  function fillDetails(): void {
    fill('#name', 'Ana');
    fill('#email', 'ana@melia.test');
    fill('#password', 'una-clave-larga-123');
  }

  it('asks the API for a code and moves to the second step', async () => {
    fillDetails();
    submit();

    const req = http.expectOne(`${API}/auth/signup`);
    expect(req.request.method).toBe('POST');
    expect(req.request.body).toEqual({
      name: 'Ana',
      email: 'ana@melia.test',
      password: 'una-clave-larga-123',
    });
    // The browser transport: cookies travel, the panel never sees the refresh token.
    expect(req.request.withCredentials).toBe(true);
    req.flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    expect(text()).toContain('Check your email');
    expect(text()).toContain('ana@melia.test');
  });

  it('confirms the code, opens the session and lands in the panel', async () => {
    fillDetails();
    submit();
    http.expectOne(`${API}/auth/signup`).flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    fill('#code', '042317');
    submit();

    const verify = http.expectOne(`${API}/auth/signup/verify`);
    expect(verify.request.body).toEqual({ email: 'ana@melia.test', code: '042317' });
    verify.flush(PAIR, { status: 201, statusText: 'Created' });
    await tick();
    http.expectOne(`${API}/me`).flush(ME);
    await tick();

    expect(router.navigateByUrl).toHaveBeenCalledWith('/panel');
  });

  it('shows the API code when the code is rejected', async () => {
    fillDetails();
    submit();
    http.expectOne(`${API}/auth/signup`).flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    fill('#code', '000000');
    submit();
    http
      .expectOne(`${API}/auth/signup/verify`)
      .flush(
        { error: { code: 'invalid_code', message: 'That code is not valid.' } },
        { status: 400, statusText: 'Bad Request' },
      );
    await tick();
    fixture.detectChanges();

    const alert = (fixture.nativeElement as HTMLElement).querySelector('[role="alert"]');
    expect(alert?.textContent).toContain('invalid_code');
    expect(alert?.textContent).not.toContain('That code is not valid');
    expect(router.navigateByUrl).not.toHaveBeenCalled();
  });

  it('never sends a first step with a password shorter than the contract', () => {
    fill('#name', 'Ana');
    fill('#email', 'ana@melia.test');
    fill('#password', 'corta');

    const button = (fixture.nativeElement as HTMLElement).querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);

    submit();
    http.expectNone(`${API}/auth/signup`);
  });
});
