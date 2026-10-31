import { inject } from '@angular/core';
import { Router, type CanActivateFn } from '@angular/router';
import { AuthService } from './auth.service';

/**
 * Protects the app routes; `/login` stays public. `provideAppInitializer` resolves
 * `restoreSession()` before the first navigation, so by the time this guard runs
 * the auth status is settled and an `anonymous` answer is final — no login flash
 * for a session that the silent refresh could have restored (S6). The bounced
 * URL travels as `returnUrl` so the login page can land the user back on it.
 */
export const authGuard: CanActivateFn = (_route, state) => {
  const auth = inject(AuthService);
  if (auth.status() === 'authenticated') {
    return true;
  }
  return inject(Router).createUrlTree(['/login'], {
    queryParams: { returnUrl: state.url },
  });
};
