import { Component, inject } from '@angular/core';
import { Router } from '@angular/router';
import { AuthService } from '../core/auth.service';

/**
 * The first real screen (S1): it renders what `GET /v1/me` returned, so it proves the
 * whole path end to end — login, session transport, interceptor, protected read — and
 * gives the operator somewhere to end the session.
 */
@Component({
  selector: 'app-account',
  templateUrl: './account.html',
  styleUrl: './account.css',
})
export class Account {
  private readonly router = inject(Router);
  protected readonly auth = inject(AuthService);

  protected async signOut(): Promise<void> {
    await this.auth.logout();
    await this.router.navigate(['/login']);
  }
}
