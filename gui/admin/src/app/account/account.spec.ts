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
  const navigate = vi.fn().mockResolvedValue(true);

  beforeEach(() => {
    user.set(USER);
    logout.mockClear();
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
