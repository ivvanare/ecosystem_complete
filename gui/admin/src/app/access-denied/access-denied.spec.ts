import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AccessDenied } from './access-denied';
import { AuthService } from '../core/auth.service';

const USER = {
  id: 'u1',
  name: 'Carlos',
  email: 'carlos@melia.test',
  status: 'ACTIVE',
  email_verified_at: null,
  roles: ['EDITOR'],
  permissions: [],
  created_at: '2026-01-01T00:00:00Z',
};

describe('AccessDenied', () => {
  const logout = vi.fn().mockResolvedValue(undefined);
  const navigateByUrl = vi.fn().mockResolvedValue(true);

  beforeEach(() => {
    logout.mockClear();
    navigateByUrl.mockClear();
    TestBed.resetTestingModule();
    TestBed.configureTestingModule({
      imports: [AccessDenied],
      providers: [
        provideRouter([]),
        { provide: Router, useValue: { navigateByUrl } },
        {
          provide: AuthService,
          useValue: { user: signal(USER), permissions: signal([]), logout },
        },
      ],
    });
  });

  it('explains the missing permission and names the session it applies to', () => {
    const fixture = TestBed.createComponent(AccessDenied);
    fixture.detectChanges();
    const text = (fixture.nativeElement as HTMLElement).textContent ?? '';

    expect(text).toContain('no panel access');
    expect(text).toContain('admin.access');
    expect(text).toContain('carlos@melia.test');
    expect(text).toContain('EDITOR');
  });

  it('signs out and returns to the login page', async () => {
    const fixture = TestBed.createComponent(AccessDenied);
    fixture.detectChanges();

    const button = (fixture.nativeElement as HTMLElement).querySelector('button') as HTMLButtonElement;
    button.click();
    await fixture.whenStable();

    expect(logout).toHaveBeenCalled();
    expect(navigateByUrl).toHaveBeenCalledWith('/login');
  });
});
