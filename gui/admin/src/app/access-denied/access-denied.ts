import { Component, inject } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { AuthService } from '../core/auth.service';

/**
 * Where `adminAccessGuard` sends a session that signed in but carries no
 * `admin.access` (S8). It lives outside the panel chrome on purpose: showing the
 * sidebar to a session that cannot use a single module would be a promise the
 * API breaks with `403 permission_denied`.
 */
@Component({
  selector: 'app-access-denied',
  imports: [RouterLink],
  templateUrl: './access-denied.html',
  styleUrl: './access-denied.css',
})
export class AccessDenied {
  protected readonly auth = inject(AuthService);
  private readonly router = inject(Router);

  protected async signOut(): Promise<void> {
    await this.auth.logout();
    await this.router.navigateByUrl('/login');
  }
}
