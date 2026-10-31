import { HttpErrorResponse, type HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { catchError, from, switchMap, throwError } from 'rxjs';
import { API_URL } from './api-url.token';
import { AuthService } from './auth.service';

/**
 * Wires the session into API calls so callers stay ignorant of auth (S7):
 *
 * - `/auth/*` routes go with credentials so the session cookies travel, and never
 *   get a Bearer header — the API does not read one there.
 * - Every other API request gets the in-memory access token, when there is one.
 * - On a 401 it refreshes once through `AuthService.refresh()` — single-flight, so
 *   a burst of concurrent 401s rotates exactly once — then retries the original
 *   request once with the new token. A second 401 propagates: no loop.
 * - A 401 with no session in memory is NOT refreshed: the family is dead or never
 *   existed, so the refresh could only answer `refresh_invalid` and mask the 401
 *   the caller actually received. The caller's own error propagates as-is.
 *
 * Requests outside `API_URL` pass through untouched: no token, no credentials, no
 * refresh. The same `startsWith` boundary is what keeps a `…/v10` sibling host path
 * from being mistaken for the API.
 */
export const authInterceptor: HttpInterceptorFn = (req, next) => {
  const apiUrl = inject(API_URL).replace(/\/+$/, '');
  if (req.url !== apiUrl && !req.url.startsWith(`${apiUrl}/`)) {
    return next(req);
  }

  // An /auth/* failure (bad credentials, dead family, csrf_invalid) must never
  // trigger a refresh — least of all the refresh call itself.
  if (req.url.startsWith(`${apiUrl}/auth/`)) {
    return next(req.clone({ withCredentials: true }));
  }

  const auth = inject(AuthService);
  const token = auth.accessToken();
  const outgoing = token
    ? req.clone({ setHeaders: { Authorization: `Bearer ${token}` } })
    : req;

  return next(outgoing).pipe(
    catchError((error: unknown) => {
      if (!(error instanceof HttpErrorResponse) || error.status !== 401) {
        return throwError(() => error);
      }
      // No session to refresh: the rotation could only fail, so the caller must
      // see its own 401 rather than a `refresh_invalid` it never caused.
      if (auth.status() !== 'authenticated') {
        return throwError(() => error);
      }
      return from(auth.refresh()).pipe(
        switchMap((fresh) =>
          next(req.clone({ setHeaders: { Authorization: `Bearer ${fresh}` } })),
        ),
      );
    }),
  );
};
