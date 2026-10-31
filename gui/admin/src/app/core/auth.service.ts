import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { firstValueFrom, timeout } from 'rxjs';
import { API_URL } from './api-url.token';

/**
 * Token pair as the API answers a browser client (`tokenPairResponse` in auth.go):
 * the refresh token never appears here — for a trusted `Origin` it travels only as
 * an httpOnly cookie. `csrf_token` is the double-submit half the panel must echo
 * back as `X-CSRF-Token` on refresh and logout (S4).
 */
/**
 * How long a rotation may take before it counts as failed. Boot awaits the silent
 * refresh, so without a bound a dead API holds the panel hostage (L11).
 */
const REFRESH_TIMEOUT_MS = 8_000;

export interface TokenPairResponse {
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_expires_in: number;
  csrf_token?: string;
}

/** `GET /v1/me` payload: identity plus the permissions the panel renders from. */
export interface Me {
  id: string;
  name: string | null;
  email: string | null;
  status: string;
  email_verified_at: string | null;
  roles: string[];
  permissions: string[];
  created_at: string;
}

export type AuthStatus = 'unknown' | 'authenticated' | 'anonymous';

/**
 * The one value web storage may hold: the CSRF double-submit token (L9). It is
 * not a credential — it only defeats requests a browser builds by itself — so
 * persisting it does not weaken L4, which rules out storage for the access
 * token. The refresh token never leaves its httpOnly cookie.
 */
export const CSRF_STORAGE_KEY = 'melia.csrf_token';

/**
 * Session state for the panel, on the browser transport (S2): the refresh token
 * lives in an httpOnly cookie that JS cannot read, and the access token lives in
 * memory only (L4). The CSRF double-submit value is the single exception: it is
 * kept in `sessionStorage` (tab-scoped) so a reload can echo it on the silent
 * refresh — without it the API answers `403 csrf_invalid` and every reload would
 * fall to /login (L9). Errors propagate as `HttpErrorResponse`; callers parse
 * `error.error.code`, never `message`.
 */
@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = inject(API_URL).replace(/\/+$/, '');

  private readonly accessTokenState = signal<string | null>(null);
  private readonly csrfTokenState = signal<string | null>(null);
  private readonly userState = signal<Me | null>(null);
  private readonly statusState = signal<AuthStatus>('unknown');

  readonly accessToken = this.accessTokenState.asReadonly();
  readonly csrfToken = this.csrfTokenState.asReadonly();
  readonly user = this.userState.asReadonly();
  readonly status = this.statusState.asReadonly();

  /** The one in-flight rotation every concurrent caller shares (S3). */
  private refreshInFlight: Promise<string> | null = null;

  constructor() {
    // Boot (L9): the in-memory tokens died with the page, but the double-submit
    // value survived in sessionStorage — seed the state from it so the silent
    // refresh can echo `X-CSRF-Token` instead of failing as 403 csrf_invalid.
    this.csrfTokenState.set(sessionStorage.getItem(CSRF_STORAGE_KEY));
  }

  /**
   * Opens a session. The body asks for the browser transport implicitly — the SPA's
   * `Origin` is what the server trusts — and `withCredentials` lets the browser
   * store the `Set-Cookie` pair. The profile is loaded before resolving so that
   * `status() === 'authenticated'` always comes with a populated `user()`.
   */
  async login(email: string, password: string): Promise<void> {
    const pair = await firstValueFrom(
      this.http.post<TokenPairResponse>(
        `${this.apiUrl}/auth/login`,
        { email, password },
        { withCredentials: true },
      ),
    );
    this.applyTokenPair(pair);
    await this.me();
  }

  /**
   * Revokes the family server-side and always clears the local state, even when the
   * call fails: the panel must not keep credentials it can no longer exercise.
   */
  async logout(): Promise<void> {
    try {
      await firstValueFrom(
        this.http.post<void>(`${this.apiUrl}/auth/logout`, {}, {
          withCredentials: true,
          headers: this.csrfHeaders(),
        }),
      );
    } finally {
      this.clearSession();
    }
  }

  /**
   * Boot path: the access token died with the page but the refresh cookie may still
   * be alive, so one silent rotation reopens the session without a login flash.
   * Resolves `false` — and lands on `anonymous` — when there is no session to
   * restore; never throws.
   */
  async restoreSession(): Promise<boolean> {
    try {
      await this.refresh();
      await this.me();
      return true;
    } catch {
      this.clearSession();
      return false;
    }
  }

  /** Loads the profile and permissions; the access token itself carries no roles. */
  async me(): Promise<Me> {
    const me = await firstValueFrom(this.http.get<Me>(`${this.apiUrl}/me`));
    this.userState.set(me);
    return me;
  }

  /**
   * Rotates the session and resolves with the new access token. Single-flight on
   * purpose (S3): the API rotates the refresh cookie on every use and treats a
   * replayed token as theft, so two parallel refreshes would revoke the whole
   * family and log the user out. Concurrent callers share the in-flight promise.
   */
  refresh(): Promise<string> {
    this.refreshInFlight ??= this.rotate().finally(() => {
      this.refreshInFlight = null;
    });
    return this.refreshInFlight;
  }

  private async rotate(): Promise<string> {
    try {
      // The handler decodes the body before falling back to the cookie, and an empty
      // body is a 400 — `{}` is the well-formed "take it from the cookie" payload.
      const pair = await firstValueFrom(
        this.http
          .post<TokenPairResponse>(`${this.apiUrl}/auth/refresh`, {}, {
            withCredentials: true,
            headers: this.csrfHeaders(),
          })
          .pipe(timeout(REFRESH_TIMEOUT_MS)),
      );
      this.applyTokenPair(pair);
      return pair.access_token;
    } catch (error) {
      // refresh_invalid / refresh_reused both mean the family is dead.
      if (error instanceof HttpErrorResponse && error.status === 401) {
        this.clearSession();
      }
      throw error;
    }
  }

  /**
   * Double-submit header for the cookie-based auth calls. The value comes from
   * memory after a login or rotation, and from sessionStorage right after a
   * reload (L9); when neither exists the call is made bare and the API decides.
   */
  private csrfHeaders(): Record<string, string> {
    const csrf = this.csrfTokenState();
    return csrf ? { 'X-CSRF-Token': csrf } : {};
  }

  private applyTokenPair(pair: TokenPairResponse): void {
    this.accessTokenState.set(pair.access_token);
    // The CSRF value rotates with every refresh: keep the new one, drop the old,
    // and overwrite the stored copy so the next reload echoes the latest (L9).
    if (pair.csrf_token) {
      this.csrfTokenState.set(pair.csrf_token);
      sessionStorage.setItem(CSRF_STORAGE_KEY, pair.csrf_token);
    }
    this.statusState.set('authenticated');
  }

  private clearSession(): void {
    this.accessTokenState.set(null);
    this.csrfTokenState.set(null);
    sessionStorage.removeItem(CSRF_STORAGE_KEY);
    this.userState.set(null);
    this.statusState.set('anonymous');
  }
}
