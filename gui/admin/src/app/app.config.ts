import { provideHttpClient, withFetch, withInterceptors } from '@angular/common/http';
import {
  ApplicationConfig,
  inject,
  provideAppInitializer,
  provideBrowserGlobalErrorListeners,
} from '@angular/core';
import { provideRouter } from '@angular/router';
import { routes } from './app.routes';
import { authInterceptor } from './core/auth.interceptor';
import { AuthService } from './core/auth.service';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes),
    provideHttpClient(withFetch(), withInterceptors([authInterceptor])),
    // Settle the auth status before the first navigation (S6): the silent refresh
    // either restores the session from the cookie or lands on `anonymous`, so the
    // guard never sees `unknown` and there is no login flash. `restoreSession()`
    // never throws, so a dead family cannot block the bootstrap.
    provideAppInitializer(() => inject(AuthService).restoreSession()),
    // API_URL is root-provided with the dev URL as its default; override it here per deploy.
  ],
};
