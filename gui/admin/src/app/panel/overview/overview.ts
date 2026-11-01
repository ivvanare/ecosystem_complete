import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { AdminApi, type AuditEvent } from '../../core/admin-api';
import { apiErrorCode } from '../../core/api-error';
import { AuthService } from '../../core/auth.service';
import { formatDateTime } from '../../core/format';
import { ICON_PATHS, visibleModules } from '../panel-nav';

/**
 * Panel home. Every number here comes from an endpoint that exists — there are no
 * placeholder statistics, because a fake metric is worse than no metric. The four
 * modules without an API are listed from the catalogue with their `soon` status,
 * so the home answers "what works today?" without lying.
 */
@Component({
  selector: 'app-overview',
  imports: [RouterLink],
  templateUrl: './overview.html',
  styleUrl: './overview.css',
})
export class Overview {
  private readonly api = inject(AdminApi);
  protected readonly auth = inject(AuthService);

  protected readonly icons = ICON_PATHS;
  protected readonly modules = computed(() => visibleModules(this.auth.permissions()));
  protected readonly formatDateTime = formatDateTime;

  protected readonly loading = signal(true);
  protected readonly errorCode = signal<string | null>(null);

  protected readonly usersTotal = signal<number | null>(null);
  protected readonly rolesTotal = signal<number | null>(null);
  protected readonly permissionsTotal = signal<number | null>(null);
  protected readonly eventsTotal = signal<number | null>(null);
  protected readonly recent = signal<readonly AuditEvent[]>([]);

  /** First name, or the local part of the email when there is no name. */
  protected readonly greetingName = computed(() => {
    const user = this.auth.user();
    const name = user?.name?.trim();
    if (name) return name.split(/\s+/)[0]!;
    const email = user?.email?.trim();
    return email ? email.split('@')[0]! : 'there';
  });

  constructor() {
    void this.load();
  }

  /**
   * One round trip per source, in parallel: totals come from the same list
   * endpoints the modules use, with `limit=1`, so the home never needs an
   * aggregate endpoint the API does not have.
   */
  protected async load(): Promise<void> {
    this.loading.set(true);
    this.errorCode.set(null);
    try {
      const [users, roles, permissions, events] = await Promise.all([
        this.api.listUsers({ page: 1, limit: 1 }),
        this.api.listRoles(),
        this.api.listPermissions(),
        this.api.listAuditEvents({ page: 1, limit: 5 }),
      ]);
      this.usersTotal.set(users.total);
      this.rolesTotal.set(roles.length);
      this.permissionsTotal.set(permissions.length);
      this.eventsTotal.set(events.total);
      this.recent.set(events.data);
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.loading.set(false);
    }
  }
}
