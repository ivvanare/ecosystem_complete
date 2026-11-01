import { HttpErrorResponse } from '@angular/common/http';
import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminApi, type Role, type UserSummary } from '../../core/admin-api';
import { Users } from './users';

const USER: UserSummary = {
  id: 'u1',
  name: 'Ana',
  email: 'ana@melia.test',
  status: 'ACTIVE',
  email_verified_at: null,
  roles: ['EDITOR'],
  created_at: '2026-01-01T00:00:00Z',
};

const ROLES: Role[] = [
  { id: 'r1', name: 'EDITOR', description: 'Content', status: 'ACTIVE', permissions: [] },
  { id: 'r2', name: 'ADMIN', description: null, status: 'ACTIVE', permissions: [] },
];

/** One macrotask: drains every microtask chained off a resolved response. */
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('Users', () => {
  const setUserRoles = vi.fn();

  function setup(listUsers: ReturnType<typeof vi.fn>): void {
    TestBed.configureTestingModule({
      imports: [Users],
      providers: [
        {
          provide: AdminApi,
          useValue: {
            listUsers,
            setUserRoles,
            listRoles: vi.fn().mockResolvedValue(ROLES),
          },
        },
      ],
    });
  }

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(Users);
    fixture.detectChanges();
    await tick();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  beforeEach(() => {
    TestBed.resetTestingModule();
    setUserRoles.mockReset();
    setUserRoles.mockResolvedValue({ ...USER, roles: ['EDITOR', 'ADMIN'] });
  });

  it('renders the page of users with their roles', async () => {
    setup(vi.fn().mockResolvedValue({ data: [USER], page: 1, limit: 20, total: 1 }));
    const element = await render();
    const text = element.textContent ?? '';

    expect(text).toContain('ana@melia.test');
    expect(text).toContain('EDITOR');
    expect(text).toContain('1 account');
  });

  it('saves the whole role set, because the API replaces it', async () => {
    setup(vi.fn().mockResolvedValue({ data: [USER], page: 1, limit: 20, total: 1 }));
    const element = await render();

    const rolesButton = [...element.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === 'Roles',
    ) as HTMLButtonElement;
    rolesButton.click();
    await tick();

    const checkboxes = [...element.querySelectorAll<HTMLInputElement>('input[type="checkbox"]')];
    const admin = checkboxes[1]!;
    admin.checked = true;
    admin.dispatchEvent(new Event('change'));

    const save = [...element.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === 'Save roles',
    ) as HTMLButtonElement;
    save.click();
    await tick();

    expect(setUserRoles).toHaveBeenCalledWith('u1', ['EDITOR', 'ADMIN']);
  });

  it('shows the API code when a role change is rejected', async () => {
    setup(vi.fn().mockResolvedValue({ data: [USER], page: 1, limit: 20, total: 1 }));
    setUserRoles.mockRejectedValue(
      new HttpErrorResponse({
        status: 403,
        error: { error: { code: 'permission_denied', message: 'Nope.' } },
      }),
    );
    const element = await render();

    const rolesButton = [...element.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === 'Roles',
    ) as HTMLButtonElement;
    rolesButton.click();
    await tick();

    const save = [...element.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === 'Save roles',
    ) as HTMLButtonElement;
    save.click();
    await tick();
    await tick();

    const alert = element.querySelector('[role="alert"]') as HTMLElement | null;
    expect(alert?.textContent).toContain('permission_denied');
    expect(alert?.textContent).not.toContain('Nope.');
  });
});
