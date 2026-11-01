import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { vi } from 'vitest';
import { Account } from './account';
import { AuthService, type Me } from '../core/auth.service';

const USER: Me = {
  id: 'u1',
  name: 'Ana',
  email: 'ana@melia.test',
  status: 'ACTIVE',
  email_verified_at: null,
  roles: ['SUPERADMIN'],
  permissions: [],
  created_at: '2026-01-01T00:00:00Z',
};

describe('Account', () => {
  const user = signal<Me | null>(USER);
  const logout = vi.fn().mockResolvedValue(undefined);
  const requestEmailChange = vi.fn().mockResolvedValue(undefined);
  const confirmEmailChange = vi.fn().mockResolvedValue(undefined);
  const navigate = vi.fn().mockResolvedValue(true);

  beforeEach(() => {
    user.set(USER);
    logout.mockClear();
    requestEmailChange.mockClear();
    confirmEmailChange.mockClear();
    navigate.mockClear();
    TestBed.configureTestingModule({
      imports: [Account],
      providers: [
        { provide: AuthService, useValue: { user, logout } },
        { provide: Router, useValue: { navigate } },
      ],
    });
  });

  it('renders what GET /v1/me returned', () => {
    const fixture = TestBed.createComponent(Account);
    fixture.detectChanges();
    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';

    expect(text).toContain('Ana');
    expect(text).toContain('ana@melia.test');
    expect(text).toContain('SUPERADMIN');
  });

  it('says so while the profile is still unknown', () => {
    user.set(null);
    const fixture = TestBed.createComponent(Account);
    fixture.detectChanges();
    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';

    expect(text).toContain('Loading your profile');
  });

  it('signs out and returns to the login page', async () => {
    const fixture = TestBed.createComponent(Account);
    fixture.detectChanges();

    await fixture.componentInstance['signOut']();

    expect(logout).toHaveBeenCalledOnce();
    expect(navigate).toHaveBeenCalledWith(['/login']);
  });
});

describe('Account — change email', () => {
  const user = signal<Me | null>(USER);
  const requestEmailChange = vi.fn().mockResolvedValue(undefined);
  const confirmEmailChange = vi.fn().mockResolvedValue(undefined);

  beforeEach(() => {
    requestEmailChange.mockClear();
    confirmEmailChange.mockClear();
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [Account],
      providers: [
        {
          provide: AuthService,
          useValue: {
            user,
            permissions: signal([]),
            logout: vi.fn().mockResolvedValue(undefined),
            requestEmailChange,
            confirmEmailChange,
          },
        },
        { provide: Router, useValue: { navigate: vi.fn().mockResolvedValue(true) } },
      ],
    });
  });

  function fill(fixture: ReturnType<typeof TestBed.createComponent<Account>>, id: string, value: string): void {
    const input = fixture.nativeElement.querySelector(`#${id}`) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  it('asks for the code with the new address and the password', async () => {
    const fixture = TestBed.createComponent(Account);
    fixture.detectChanges();

    fill(fixture, 'new-email', 'nueva@melia.test');
    fill(fixture, 'current-password', 'una-clave-larga-123');
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    fixture.detectChanges();

    expect(requestEmailChange).toHaveBeenCalledWith('nueva@melia.test', 'una-clave-larga-123');
    // The code goes to the new address, so the screen has to say which one.
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('nueva@melia.test');
  });

  it('confirms the code and ends the session, because the address changed', async () => {
    const fixture = TestBed.createComponent(Account);
    fixture.detectChanges();

    fill(fixture, 'new-email', 'nueva@melia.test');
    fill(fixture, 'current-password', 'una-clave-larga-123');
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    fixture.detectChanges();

    fill(fixture, 'email-code', '042317');
    (fixture.nativeElement.querySelector('form') as HTMLFormElement).dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    fixture.detectChanges();

    expect(confirmEmailChange).toHaveBeenCalledWith('042317');
    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';
    expect(text).toContain('Email updated');
    expect(text).toContain('sign in again');
  });
});
