import { HttpErrorResponse } from '@angular/common/http';
import { Component, computed, inject, signal } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { RouterLink } from '@angular/router';
import { AuthService } from '../core/auth.service';

/**
 * Password recovery in two steps, matching the API: `POST /v1/auth/password/forgot`
 * mails a six-digit code and always answers 202, then `POST /v1/auth/password/reset`
 * spends it and sets the new password.
 *
 * Without this screen the code a person receives has nowhere to go — the same dead end
 * the invitation had before `/registro` existed.
 */
@Component({
  selector: 'app-recover',
  imports: [RouterLink],
  templateUrl: './recover.html',
  styleUrl: './recover.css',
})
export class Recover {
  private readonly auth = inject(AuthService);
  private readonly route = inject(ActivatedRoute);

  protected readonly step = signal<'email' | 'code' | 'done'>('email');
  protected readonly email = signal('');
  protected readonly code = signal('');
  protected readonly password = signal('');
  protected readonly submitting = signal(false);
  protected readonly errorCode = signal<string | null>(null);
  protected readonly sentTo = signal('');

  constructor() {
    // The mail links here with the address, so a person who already holds a code never
    // has to ask for a second one: they land straight on the code step.
    const email = (this.route.snapshot.queryParamMap.get('email') ?? '').trim();
    if (email !== '') {
      this.email.set(email);
      this.sentTo.set(email);
      this.step.set('code');
    }
  }

  /** Mirrors the API floor: twelve characters, no composition rules. */
  protected readonly canReset = computed(
    () => /^\d{6}$/.test(this.code()) && this.password().length >= 12,
  );

  protected async requestCode(event: Event): Promise<void> {
    event.preventDefault();
    const email = this.email().trim();
    if (this.submitting() || email === '') return;

    this.submitting.set(true);
    this.errorCode.set(null);
    try {
      await this.auth.requestPasswordReset(email);
      this.sentTo.set(email);
      this.step.set('code');
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.submitting.set(false);
    }
  }

  protected async resetPassword(event: Event): Promise<void> {
    event.preventDefault();
    if (this.submitting() || !this.canReset()) return;

    this.submitting.set(true);
    this.errorCode.set(null);
    try {
      await this.auth.resetPassword(this.sentTo(), this.code(), this.password());
      this.step.set('done');
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.submitting.set(false);
    }
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
