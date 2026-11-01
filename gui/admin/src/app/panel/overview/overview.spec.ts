import { HttpErrorResponse } from '@angular/common/http';
import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminApi, type AuditEvent } from '../../core/admin-api';
import { AuthService } from '../../core/auth.service';
import { Overview } from './overview';

const ME = {
  id: 'u1',
  name: 'Ana Díaz',
  email: 'ana@melia.test',
  status: 'ACTIVE',
  email_verified_at: null,
  roles: ['SUPERADMIN'],
  permissions: ['admin.access', 'user.read', 'role.manage', 'place.create'],
  created_at: '2026-01-01T00:00:00Z',
};

const EVENT: AuditEvent = {
  id: 'e1',
  event: 'auth.login.failed',
  user_id: null,
  actor_user_id: null,
  email_hash: null,
  ip: '127.0.0.1',
  country: 'VE',
  city: null,
  user_agent: null,
  metadata: null,
  created_at: '2026-10-31T12:00:00Z',
};

/** One macrotask: drains every microtask chained off a resolved response. */
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('Overview', () => {
  function setup(listUsers: ReturnType<typeof vi.fn>): void {
    TestBed.configureTestingModule({
      imports: [Overview],
      providers: [
        provideRouter([]),
        {
          provide: AdminApi,
          useValue: {
            listUsers,
            listRoles: vi.fn().mockResolvedValue([{}, {}, {}]),
            listPermissions: vi.fn().mockResolvedValue([{}, {}]),
            listAuditEvents: vi
              .fn()
              .mockResolvedValue({ data: [EVENT], page: 1, limit: 5, total: 42 }),
          },
        },
        {
          provide: AuthService,
          useValue: { user: signal(ME), permissions: signal(ME.permissions) },
        },
      ],
    });
  }

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(Overview);
    fixture.detectChanges();
    await tick();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  beforeEach(() => {
    TestBed.resetTestingModule();
  });

  it('renders the real totals, the recent events and the module status', async () => {
    setup(vi.fn().mockResolvedValue({ data: [], page: 1, limit: 1, total: 7 }));
    const element = await render();
    const text = element.textContent ?? '';

    expect(text).toContain('Welcome back, Ana');
    expect(text).toContain('7'); // users total
    expect(text).toContain('3'); // roles
    expect(text).toContain('2'); // permissions
    expect(text).toContain('42'); // audit events
    expect(text).toContain('auth.login.failed');
    expect(text).toContain('Users');
    // Places exists in the catalogue without an API, and says so.
    expect(text).toContain('Places');
    expect(text).toContain('Coming soon');
  });

  it('shows the API error code, never the message, when a total cannot load', async () => {
    setup(
      vi
        .fn()
        .mockRejectedValue(
          new HttpErrorResponse({
            status: 403,
            error: { error: { code: 'permission_denied', message: 'No.' } },
          }),
        ),
    );
    const element = await render();
    const text = element.textContent ?? '';

    expect(text).toContain('permission_denied');
    expect(text).not.toContain('No.');
    expect(element.querySelector('[role="alert"]')).toBeTruthy();
  });
});
