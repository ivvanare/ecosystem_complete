import { TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ListQuery } from 'crud-maker';
import { AdminTransport } from '../../core/admin-transport';
import { Audit } from './audit';

const ROWS = [
  {
    id: 'e1',
    event: 'auth.login.failed',
    user_id: 'f51b7735-acc5-4c3b-ba2f-63847d195c85',
    actor_user_id: null,
    email_hash: '1fe7ca7a9c6e6a8d57463a4936b009ce',
    ip: '172.18.0.1',
    country: null,
    city: null,
    user_agent: 'Firefox/153.0 on Linux x86_64',
    metadata: {},
    created_at: '2026-10-31T12:00:00Z',
  },
  {
    id: 'e2',
    event: 'role.assigned',
    user_id: 'c0ffee00-0000-4000-8000-000000000002',
    actor_user_id: 'f51b7735-acc5-4c3b-ba2f-63847d195c85',
    email_hash: null,
    ip: '172.18.0.1',
    country: 'VE',
    city: 'Caracas',
    user_agent: 'curl/8.5.0',
    metadata: {},
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

  it('shows the fields the API actually sends, not just the event name', async () => {
    const element = await render();
    const text = element.textContent ?? '';

    // Headers the log needs to be useful: who, from where, with which client.
    for (const label of ['User', 'Actor', 'Email hash', 'IP', 'Country', 'City', 'Agent']) {
      expect(text).toContain(label);
    }
    // Values reach the cells, truncated where they would break the layout.
    expect(text).toContain('Firefox/153.0');
    expect(text).toContain('Caracas');
    expect(text).toContain('1fe7ca7a9c6e');
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
