import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ListQuery } from 'crud-maker';
import { AdminTransport } from '../../core/admin-transport';
import { Audit } from './audit';

const ROWS = [
  {
    id: 'e1',
    event: 'auth.login.failed',
    actor_user_id: null,
    ip: '127.0.0.1',
    country: 'VE',
    created_at: '2026-10-31T12:00:00Z',
  },
  {
    id: 'e2',
    event: 'role.assigned',
    actor_user_id: 'u1',
    ip: null,
    country: null,
    created_at: '2026-10-31T11:00:00Z',
  },
];

/** One macrotask: drains the microtasks chained off a resolved promise. */
const tick = (ms = 0) => new Promise<void>((resolve) => setTimeout(resolve, ms));

describe('Audit', () => {
  const list = vi.fn();

  beforeEach(() => {
    TestBed.resetTestingModule();
    list.mockReset().mockResolvedValue({ rows: ROWS, total: 42 });
    TestBed.configureTestingModule({
      imports: [Audit],
      providers: [
        {
          provide: AdminTransport,
          useValue: { list, get: vi.fn(), create: vi.fn(), update: vi.fn(), remove: vi.fn() },
        },
      ],
    });
  });

  async function render(): Promise<HTMLElement> {
    const fixture = TestBed.createComponent(Audit);
    fixture.detectChanges();
    await tick();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  it('renders the events the engine loaded, with the total', async () => {
    const element = await render();
    const text = element.textContent ?? '';

    expect(list).toHaveBeenCalled();
    expect(text).toContain('auth.login.failed');
    expect(text).toContain('role.assigned');
    expect(text).toContain('42 events');
  });

  it('turns the exact-event filter into a query on the transport', async () => {
    const element = await render();
    const input = element.querySelector('input[type="text"]') as HTMLInputElement | null;

    expect(input).toBeTruthy();
    input!.value = 'auth.login.failed';
    input!.dispatchEvent(new Event('input'));
    await tick(300);

    const last = list.mock.calls.at(-1)?.[1] as ListQuery;
    expect(last.filters['event_eq']).toBe('auth.login.failed');
    // The log has no search endpoint, so a toolbar search box must not exist.
    expect(element.querySelector('input[type="search"]')).toBeNull();
  });
});
