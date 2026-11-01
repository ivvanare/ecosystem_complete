import { inject } from '@angular/core';
import { Router, type CanActivateFn } from '@angular/router';
import { AuthService } from './auth.service';

/** The one permission that opens the backoffice (`api/API.md` §7). */
export const ADMIN_ACCESS = 'admin.access';

/**
 * Guards `/panel` (S8). The panel renders from `permissions`, never from role
 * names (`api/API.md` §6), so a session that authenticated but carries no
 * `admin.access` is sent to `/no-access` instead of into a half-broken panel:
 * every admin endpoint would answer `403 permission_denied` anyway.
 *
 * It runs after `authGuard`, so the status is settled and a missing session is
 * already bounced to `/login` with its `returnUrl`.
 */
export const adminAccessGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  if (auth.permissions().includes(ADMIN_ACCESS)) {
    return true;
  }
  return inject(Router).createUrlTree(['/no-access']);
};
