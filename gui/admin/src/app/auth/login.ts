import { HttpErrorResponse } from '@angular/common/http';
import { Component, inject, signal } from '@angular/core';
import { form, FormField, required } from '@angular/forms/signals';
import { ActivatedRoute, Router } from '@angular/router';
import { AuthService } from '../core/auth.service';

interface LoginFormModel {
  email: string;
  password: string;
}

/** Landing after a successful sign-in that has no returnUrl to honour. */
const DEFAULT_RETURN_URL = '/faqs';

/**
 * Extracts the machine-readable code from the API error envelope
 * (`{"error":{"code","message"}}`): clients parse `code`, never `message`.
 * A failure without an envelope (network down, CORS) shows as `network_error`.
 */
function apiErrorCode(error: unknown): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { error?: { code?: unknown } } | null;
    const code = body?.error?.code;
    if (typeof code === 'string' && code !== '') {
      return code;
    }
  }
  return 'network_error';
}

/**
 * The only public route. Signs in on the browser transport (S2) and lands on the
 * `returnUrl` the guard bounced from, or on the default route. A failure shows
 * the API's `error.code` verbatim so the user can quote it to an administrator.
 */
@Component({
  selector: 'app-login',
  imports: [FormField],
  templateUrl: './login.html',
  styleUrl: './login.css',
})
export class Login {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);
  private readonly route = inject(ActivatedRoute);

  protected readonly credentials = signal<LoginFormModel>({ email: '', password: '' });
  protected readonly loginForm = form(this.credentials, (schema) => {
    required(schema.email, { message: 'Email is required' });
    required(schema.password, { message: 'Password is required' });
  });

  protected readonly submitting = signal(false);
  /** The API `error.code` of the last failed attempt, shown verbatim. */
  protected readonly errorCode = signal<string | null>(null);

  protected async submit(event: Event): Promise<void> {
    event.preventDefault();
    // Invalid form, or a request already in flight: never send twice.
    if (this.submitting() || this.loginForm().invalid()) {
      return;
    }
    this.submitting.set(true);
    this.errorCode.set(null);
    try {
      const { email, password } = this.credentials();
      await this.auth.login(email, password);
      await this.router.navigateByUrl(this.returnUrl());
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.submitting.set(false);
    }
  }

  /**
   * Where a successful sign-in lands: the URL the guard bounced from, but only
   * when it is an in-app path — absolute URLs and protocol-relative `//…` would
   * turn the panel into an open redirector, so they fall back to the default.
   */
  private returnUrl(): string {
    const candidate = this.route.snapshot.queryParamMap.get('returnUrl');
    return candidate !== null && candidate.startsWith('/') && !candidate.startsWith('//')
      ? candidate
      : DEFAULT_RETURN_URL;
  }
}
