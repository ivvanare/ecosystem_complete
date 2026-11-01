import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, inject, signal } from '@angular/core';
import { form, FormField, minLength, required } from '@angular/forms/signals';
import { Router, RouterLink } from '@angular/router';
import { AuthService } from '../core/auth.service';

interface SignupFormModel {
  name: string;
  email: string;
  password: string;
}

const EMPTY: SignupFormModel = { name: '', email: '', password: '' };

/** Where a verified registration lands: the panel it was invited to. */
const AFTER_SIGNUP = '/panel';

/**
 * Registration in two steps, mirroring the API: `POST /v1/auth/signup` answers 202 and
 * mails a six-digit code; `POST /v1/auth/signup/verify` proves the address, creates the
 * account and opens the session. Until now registration existed only in the API, so an
 * invitation had nowhere to send anyone.
 *
 * The first step always answers the same, so the screen never reveals whether the address
 * is already registered: the second step is where a taken email becomes visible.
 */
@Component({
  selector: 'app-signup',
  imports: [FormField, RouterLink],
  templateUrl: './signup.html',
  styleUrl: './signup.css',
})
export class Signup {
  private readonly auth = inject(AuthService);
  private readonly router = inject(Router);

  /** `details` is the step currently on screen; the email is immutable after step one. */
  protected readonly step = signal<'details' | 'code'>('details');
  protected readonly submitting = signal(false);
  protected readonly errorCode = signal<string | null>(null);
  protected readonly sentTo = signal('');

  protected readonly model = signal<SignupFormModel>({ ...EMPTY });
  protected readonly detailsForm = form(this.model, (schema) => {
    required(schema.name, { message: 'Name is required' });
    required(schema.email, { message: 'Email is required' });
    required(schema.password, { message: 'Password is required' });
    minLength(schema.password, 12, { message: 'At least 12 characters' });
  });

  protected readonly code = signal('');
  /** Disabled until the six digits are there: the API rejects anything else. */
  protected readonly canVerify = computed(() => /^\d{6}$/.test(this.code()));

  protected async submitDetails(event: Event): Promise<void> {
    event.preventDefault();
    if (this.submitting() || this.detailsForm().invalid()) return;

    this.submitting.set(true);
    this.errorCode.set(null);
    try {
      const { name, email, password } = this.model();
      await this.auth.startSignup(name.trim(), email.trim(), password);
      this.sentTo.set(email.trim());
      this.step.set('code');
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.submitting.set(false);
    }
  }

  protected async submitCode(event: Event): Promise<void> {
    event.preventDefault();
    if (this.submitting() || !this.canVerify()) return;

    this.submitting.set(true);
    this.errorCode.set(null);
    try {
      await this.auth.verifySignup(this.sentTo(), this.code());
      await this.router.navigateByUrl(AFTER_SIGNUP);
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.submitting.set(false);
    }
  }

  /** The API makes the email immutable mid-flow: changing it means starting over. */
  protected startOver(): void {
    this.step.set('details');
    this.code.set('');
    this.errorCode.set(null);
  }
}

/** Clients parse `error.code`, never `message` (`api/API.md` §1). */
function apiErrorCode(error: unknown): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { error?: { code?: unknown } } | null;
    const code = body?.error?.code;
    if (typeof code === 'string' && code !== '') return code;
  }
  return 'network_error';
}
