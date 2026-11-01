import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminApi, type Role, type UserSummary } from '../../core/admin-api';
import { AdminTransport } from '../../core/admin-transport';
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

/** One macrotask: drains the microtasks chained off a resolved promise. */
const tick = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

describe('Users', () => {
  const list = vi.fn();
  const listRoles = vi.fn();
  const setUserRoles = vi.fn();

  beforeEach(() => {
    TestBed.resetTestingModule();
    list.mockReset().mockResolvedValue({ rows: [USER], total: 1 });
    listRoles.mockReset().mockResolvedValue(ROLES);
    setUserRoles.mockReset().mockResolvedValue({ ...USER, roles: ['EDITOR', 'ADMIN'] });

    TestBed.configureTestingModule({
      imports: [Users],
      providers: [
        // The screen never talks HTTP in a unit test: the transport is the seam the
        // engine was built around, so replacing it is enough.
        {
          provide: AdminTransport,
          useValue: {
            list,
            get: vi.fn(),
            create: vi.fn(),
            update: vi.fn(),
            remove: vi.fn(),
          },
        },
        { provide: AdminApi, useValue: { listRoles, setUserRoles } },
      ],
    });
  });

  afterEach(() => {
    // The overlay lives outside the fixture; leaving it behind leaks into the next test.
    document.body.querySelectorAll('.cdk-overlay-container, .ant-modal-root').forEach((node) => node.remove());
  });

  // The dialog is portalled to <body>, so its content is not inside the fixture.
  function inDialog<T extends Element>(selector: string): NodeListOf<T> {
    return document.body.querySelectorAll<T>(selector);
  }

  function dialogButton(text: string): HTMLButtonElement {
    const found = [...inDialog<HTMLButtonElement>('button')].find(
      (button) => button.textContent?.trim() === text,
    );
    if (!found) throw new Error(`no dialog button labelled "${text}"`);
    return found;
  }

  async function render(): Promise<{ element: HTMLElement; fixture: ReturnType<typeof TestBed.createComponent<Users>> }> {
    const fixture = TestBed.createComponent(Users);
    fixture.detectChanges();
    await tick();
    fixture.detectChanges();
    return { element: fixture.nativeElement as HTMLElement, fixture };
  }

  function buttonByText(element: HTMLElement, text: string): HTMLButtonElement {
    const found = [...element.querySelectorAll('button')].find(
      (button) => button.textContent?.trim() === text,
    );
    if (!found) throw new Error(`no button labelled "${text}"`);
    return found as HTMLButtonElement;
  }

  it('renders the rows the engine loaded, through the config columns', async () => {
    const { element } = await render();
    const text = element.textContent ?? '';

    expect(list).toHaveBeenCalled();
    expect(text).toContain('ana@melia.test');
    expect(text).toContain('ACTIVE');
    expect(text).toContain('1 account');
  });

  it('replaces the whole role set from the row action', async () => {
    const { element, fixture } = await render();

    buttonByText(element, 'Roles').click();
    fixture.detectChanges();
    await tick();

    const checkboxes = [...inDialog<HTMLInputElement>('input[type="checkbox"]')];
    expect(checkboxes).toHaveLength(ROLES.length);
    const admin = checkboxes[1]!;
    admin.checked = true;
    admin.dispatchEvent(new Event('change'));

    dialogButton('Save roles').click();
    await tick();

    // The endpoint is a replacement: unchecked EDITOR would be sent too if the
    // screen were building a delta.
    expect(setUserRoles).toHaveBeenCalledWith('u1', ['EDITOR', 'ADMIN']);
  });

  it('shows the API code when the role change is rejected', async () => {
    const { HttpErrorResponse } = await import('@angular/common/http');
    setUserRoles.mockRejectedValue(
      new HttpErrorResponse({
        status: 403,
        error: { error: { code: 'permission_denied', message: 'Nope.' } },
      }),
    );
    const { element, fixture } = await render();

    buttonByText(element, 'Roles').click();
    fixture.detectChanges();
    await tick();
    dialogButton('Save roles').click();
    await tick();
    await tick();
    fixture.detectChanges();

    const alert = inDialog<HTMLElement>('[role="alert"]');
    expect(alert.length).toBeGreaterThan(0);
    expect(alert[0]!.textContent).toContain('permission_denied');
    expect(alert[0]!.textContent).not.toContain('Nope.');
  });
});
