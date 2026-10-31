import { InjectionToken } from '@angular/core';

/**
 * Base URL of the versioned API, e.g. `http://localhost:9000/v1` in dev.
 *
 * This default is the single place the dev URL may appear (S7): components,
 * services and the interceptor inject the token, so tests point it at a fake
 * and a deploy repoints every call by overriding this one provider.
 */
export const API_URL = new InjectionToken<string>('API_URL', {
  providedIn: 'root',
  factory: () => 'http://localhost:9000/v1',
});
