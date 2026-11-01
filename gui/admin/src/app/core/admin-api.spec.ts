import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { API_URL } from './api-url.token';
import { AdminApi } from './admin-api';

const API = 'https://api.test/v1';

describe('AdminApi', () => {
  let api: AdminApi;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        { provide: API_URL, useValue: API },
        provideHttpClient(),
        provideHttpClientTesting(),
      ],
    });
    api = TestBed.inject(AdminApi);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
  });

  it('maps a users query to page/limit/q and omits an empty filter', async () => {
    const pending = api.listUsers({ page: 2, limit: 20, q: 'ana', status: null });
    const req = http.expectOne((r) => r.url === `${API}/admin/users`);

    expect(req.request.method).toBe('GET');
    expect(req.request.params.get('page')).toBe('2');
    expect(req.request.params.get('limit')).toBe('20');
    expect(req.request.params.get('q')).toBe('ana');
    // A null filter must not travel as `status=` — the API would see a value.
    expect(req.request.params.has('status')).toBe(false);

    req.flush({ data: [], page: 2, limit: 20, total: 0 });
    await expect(pending).resolves.toEqual({ data: [], page: 2, limit: 20, total: 0 });
  });

  it('replaces the role set with a PATCH carrying exactly the given names', async () => {
    const pending = api.setUserRoles('user-1', ['EDITOR']);
    const req = http.expectOne(`${API}/admin/users/user-1/roles`);

    expect(req.request.method).toBe('PATCH');
    expect(req.request.body).toEqual({ roles: ['EDITOR'] });

    req.flush({ id: 'user-1', status: 'ACTIVE', roles: ['EDITOR'] });
    await expect(pending).resolves.toMatchObject({ roles: ['EDITOR'] });
  });

  it('unwraps the data array of the role and permission catalogues', async () => {
    const roles = api.listRoles();
    http
      .expectOne(`${API}/admin/roles`)
      .flush({ data: [{ id: 'r1', name: 'ADMIN', description: null, status: 'ACTIVE', permissions: [] }] });
    await expect(roles).resolves.toHaveLength(1);

    const permissions = api.listPermissions();
    http
      .expectOne(`${API}/admin/permissions`)
      .flush({ data: [{ id: 'p1', code: 'admin.access', module: 'ADMIN', name: 'Access', status: 'ACTIVE' }] });
    await expect(permissions).resolves.toHaveLength(1);
  });

  it('sends only the audit filters that have a value', async () => {
    const pending = api.listAuditEvents({ page: 1, limit: 20, event: 'auth.login.failed' });
    const req = http.expectOne((r) => r.url === `${API}/admin/audit-events`);

    expect(req.request.params.get('event')).toBe('auth.login.failed');
    expect(req.request.params.has('userId')).toBe(false);
    expect(req.request.params.has('from')).toBe(false);

    req.flush({ data: [], page: 1, limit: 20, total: 0 });
    await pending;
  });
});
