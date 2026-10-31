import { signal } from '@angular/core';
import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { HttpTransport } from 'crud-maker';
import { API_URL } from './api-url.token';
import { AuthService } from './auth.service';
import { authInterceptor } from './auth.interceptor';

/**
 * S8: the engine keeps its seam. `HttpTransport` is constructed with nothing but an
 * `HttpClient` and the base URL, and the session reaches it through the interceptor —
 * no fork of `crud-maker`, no auth knowledge inside the transport.
 */
describe('HttpTransport over the real API', () => {
  let http: HttpTransport;
  let httpMock: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting(),
        { provide: API_URL, useValue: 'http://api.test/v1' },
        {
          provide: AuthService,
          useValue: {
            accessToken: signal('session-token'),
            status: signal('authenticated'),
            refresh: () => Promise.resolve('session-token'),
          },
        },
      ],
    });

    http = new HttpTransport(TestBed.inject(HttpClient), TestBed.inject(API_URL));
    httpMock = TestBed.inject(HttpTestingController);
  });

  afterEach(() => httpMock.verify());

  it('builds the URL from API_URL and carries the session through the interceptor', async () => {
    const pending = http.list('users', {
      page: 1,
      pageSize: 20,
      sort: null,
      filters: {},
      search: null,
    });

    const req = httpMock.expectOne((r) => r.url === 'http://api.test/v1/users');
    expect(req.request.headers.get('Authorization')).toBe('Bearer session-token');

    req.flush({ data: [], total: 0, page: 1, pageSize: 20 });
    await pending.then(
      () => undefined,
      () => undefined,
    );
  });
});
