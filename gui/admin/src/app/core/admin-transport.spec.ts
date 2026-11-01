import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { ListQuery } from 'crud-maker';
import { AdminTransport } from './admin-transport';
import { API_URL } from './api-url.token';

const API = 'https://api.test/v1';

function query(overrides: Partial<ListQuery> = {}): ListQuery {
  return { page: 1, pageSize: 20, sort: null, filters: {}, search: null, ...overrides };
}

describe('AdminTransport', () => {
  let transport: AdminTransport;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    });
    transport = TestBed.inject(AdminTransport);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
  });

  it('maps the engine query to this API dialect and normalizes the envelope', async () => {
    const pending = transport.list(
      'admin/users',
      query({ page: 2, pageSize: 10, search: 'ana', filters: { status_eq: 'ACTIVE' } }),
    );
    const req = http.expectOne((r) => r.url === `${API}/admin/users`);

    expect(req.request.params.get('page')).toBe('2');
    expect(req.request.params.get('limit')).toBe('10');
    expect(req.request.params.get('q')).toBe('ana');
    // The engine key is `status_eq`; the API reads `status`.
    expect(req.request.params.get('status')).toBe('ACTIVE');
    expect(req.request.params.has('status_eq')).toBe(false);

    req.flush({ data: [{ id: 'u1' }], page: 2, limit: 10, total: 42 });
    await expect(pending).resolves.toEqual({ rows: [{ id: 'u1' }], total: 42 });
  });

  it('drops a filter the endpoint does not expose instead of sending it', async () => {
    const pending = transport.list('admin/audit-events', query({ filters: { event_eq: 'auth.login.failed' } }));
    const req = http.expectOne((r) => r.url === `${API}/admin/audit-events`);

    expect(req.request.params.get('event')).toBe('auth.login.failed');
    // audit-events has no search parameter, so a term must not travel as `q`.
    expect(req.request.params.has('q')).toBe(false);

    req.flush({ data: [], page: 1, limit: 20, total: 0 });
    await pending;
  });

  it('refuses an endpoint it has no dialect for', async () => {
    await expect(transport.list('admin/nowhere', query())).rejects.toThrow(/no list/i);
  });

  it('surfaces the API error code on a failed list', async () => {
    const pending = transport.list('admin/users', query());
    http.expectOne((r) => r.url === `${API}/admin/users`).flush(
      { error: { code: 'permission_denied', message: 'Nope.' } },
      { status: 403, statusText: 'Forbidden' },
    );

    await expect(pending).rejects.toThrow('permission_denied');
  });

  it('refuses the mutations this API does not offer', async () => {
    await expect(transport.create('admin/users', {})).rejects.toThrow(/no create/i);
    await expect(transport.get('admin/users', 'u1')).rejects.toThrow(/no get/i);
    await expect(transport.remove('admin/users', 'u1')).rejects.toThrow(/no remove/i);
  });
});
