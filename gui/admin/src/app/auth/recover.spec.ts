import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { type ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, convertToParamMap, provideRouter } from '@angular/router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API_URL } from '../core/api-url.token';
import { Recover } from './recover';

const API = 'https://api.test/v1';
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('Recover', () => {
  let http: HttpTestingController;
  /** Address the URL carries, as if the mail had been opened. */
  let linkedEmail: string | null;

  beforeEach(async () => {
    sessionStorage.clear();
    linkedEmail = null;
    await TestBed.configureTestingModule({
      imports: [Recover],
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
        provideRouter([]),
        {
          provide: ActivatedRoute,
          useFactory: () => ({
            snapshot: {
              queryParamMap: convertToParamMap(linkedEmail ? { email: linkedEmail } : {}),
            },
          }),
        },
      ],
    }).compileComponents();
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    vi.restoreAllMocks();
  });

  function create(): ComponentFixture<Recover> {
    const fixture = TestBed.createComponent(Recover);
    fixture.detectChanges();
    return fixture;
  }

  function fill(fixture: ComponentFixture<Recover>, selector: string, value: string): void {
    const input = fixture.nativeElement.querySelector(selector) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  function submit(fixture: ComponentFixture<Recover>): void {
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(
      new Event('submit'),
    );
    fixture.detectChanges();
  }

  const text = (fixture: ComponentFixture<Recover>) =>
    (fixture.nativeElement as HTMLElement).textContent ?? '';

  it('asks for a code and moves to the second step', async () => {
    const fixture = create();
    fill(fixture, '#email', 'ana@melia.test');
    submit(fixture);

    const req = http.expectOne(`${API}/auth/password/forgot`);
    expect(req.request.body).toEqual({ email: 'ana@melia.test' });
    req.flush(null, { status: 202, statusText: 'Accepted' });
    await tick();
    fixture.detectChanges();

    expect(text(fixture)).toContain('Set a new password');
    expect(text(fixture)).toContain('has an account');
  });

  it('opens on the code step when the mail link carries the address', () => {
    // Arriving from the mail: the code is already in hand, so no request is made.
    linkedEmail = 'ana@melia.test';
    const fixture = create();

    expect(text(fixture)).toContain('Set a new password');
    expect(text(fixture)).toContain('ana@melia.test');
    http.expectNone(`${API}/auth/password/forgot`);
  });

  it('spends the code and sets the new password', async () => {
    linkedEmail = 'ana@melia.test';
    const fixture = create();

    fill(fixture, '#code', '042317');
    fill(fixture, '#password', 'otra-clave-larga-123');
    submit(fixture);

    const req = http.expectOne(`${API}/auth/password/reset`);
    expect(req.request.body).toEqual({
      email: 'ana@melia.test',
      code: '042317',
      new_password: 'otra-clave-larga-123',
    });
    req.flush(null, { status: 200, statusText: 'OK' });
    await tick();
    fixture.detectChanges();

    expect(text(fixture)).toContain('Password updated');
  });

  it('shows the API code when the password change is rejected', async () => {
    linkedEmail = 'ana@melia.test';
    const fixture = create();

    fill(fixture, '#code', '000000');
    fill(fixture, '#password', 'otra-clave-larga-123');
    submit(fixture);
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
    linkedEmail = 'ana@melia.test';
    const fixture = create();

    fill(fixture, '#code', '123');
    fill(fixture, '#password', 'corta');

    const button = (fixture.nativeElement as HTMLElement).querySelector(
      'button[type="submit"]',
    ) as HTMLButtonElement;
    expect(button.disabled).toBe(true);

    submit(fixture);
    http.expectNone(`${API}/auth/password/reset`);
  });
});
