import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { type ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API_URL } from '../core/api-url.token';
import { Recover } from './recover';

const API = 'https://api.test/v1';
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('Recover', () => {
  let fixture: ComponentFixture<Recover>;
  let http: HttpTestingController;

  beforeEach(async () => {
    sessionStorage.clear();
    await TestBed.configureTestingModule({
      imports: [Recover],
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
    fixture = TestBed.createComponent(Recover);
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

  const text = () => (fixture.nativeElement as HTMLElement).textContent ?? '';

  it('asks for a code and moves to the second step', async () => {
    fill('#email', 'ana@melia.test');
    submit();

    const req = http.expectOne(`${API}/auth/password/forgot`);
    expect(req.request.body).toEqual({ email: 'ana@melia.test' });
    req.flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    expect(text()).toContain('Set a new password');
    // The screen cannot claim the account exists: it says "if it has an account".
    expect(text()).toContain('has an account');
  });

  it('spends the code and sets the new password', async () => {
    fill('#email', 'ana@melia.test');
    submit();
    http.expectOne(`${API}/auth/password/forgot`).flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    fill('#code', '042317');
    fill('#password', 'otra-clave-larga-123');
    submit();

    const req = http.expectOne(`${API}/auth/password/reset`);
    expect(req.request.body).toEqual({
      email: 'ana@melia.test',
      code: '042317',
      new_password: 'otra-clave-larga-123',
    });
    req.flush(null, { status: 200, statusText: 'OK' });
    await tick();
    fixture.detectChanges();

    expect(text()).toContain('Password updated');
  });

  it('shows the API code when the password change is rejected', async () => {
    fill('#email', 'ana@melia.test');
    submit();
    http.expectOne(`${API}/auth/password/forgot`).flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    fill('#code', '000000');
    fill('#password', 'otra-clave-larga-123');
    submit();
    http
      .expectOne(`${API}/auth/password/reset`)
      .flush(
        { error: { code: 'invalid_code', message: 'That code is not valid.' } },
        { status: 400, statusText: 'Bad Request' },
      );
    await tick();
    fixture.detectChanges();

    const alert = (fixture.nativeElement as HTMLElement).querySelector('[role="alert"]');
    expect(alert?.textContent).toContain('invalid_code');
    expect(alert?.textContent).not.toContain('That code is not valid');
  });

  it('never sends a short password or a partial code', () => {
    fill('#email', 'ana@melia.test');
    submit();
    http.expectOne(`${API}/auth/password/forgot`).flush(null, { status: 202, statusText: 'Accepted' });
    return tick().then(() => {
      fixture.detectChanges();
      fill('#code', '123');
      fill('#password', 'corta');
      const button = (fixture.nativeElement as HTMLElement).querySelector(
        'button[type="submit"]',
      ) as HTMLButtonElement;
      expect(button.disabled).toBe(true);
      submit();
      http.expectNone(`${API}/auth/password/reset`);
    });
  });
});
