import { Component, computed, inject, signal } from '@angular/core';
import { AdminApi, type Permission, type Role } from '../../core/admin-api';
import { apiErrorCode } from '../../core/api-error';

/**
 * Roles and the permission catalogue, read-only (S4): the API lets a role be
 * assigned to a user, not edited here, so showing mutation controls would be a
 * button that always fails. The two lists answer the question the screen exists
 * for — which role grants what.
 */
@Component({
  selector: 'app-roles',
  templateUrl: './roles.html',
  styleUrl: './roles.css',
})
export class Roles {
  private readonly api = inject(AdminApi);

  protected readonly roles = signal<readonly Role[]>([]);
  protected readonly permissions = signal<readonly Permission[]>([]);
  protected readonly loading = signal(true);
  protected readonly errorCode = signal<string | null>(null);

  /** Permission catalogue grouped by module, the way the API orders it. */
  protected readonly grouped = computed(() => {
    const buckets = new Map<string, Permission[]>();
    for (const permission of this.permissions()) {
      const bucket = buckets.get(permission.module) ?? [];
      bucket.push(permission);
      buckets.set(permission.module, bucket);
    }
    return [...buckets.entries()]
      .map(([module, items]) => ({ module, items }))
      .sort((a, b) => a.module.localeCompare(b.module));
  });

  constructor() {
    void this.load();
  }

  protected async load(): Promise<void> {
    this.loading.set(true);
    this.errorCode.set(null);
    try {
      const [roles, permissions] = await Promise.all([
        this.api.listRoles(),
        this.api.listPermissions(),
      ]);
      this.roles.set(roles);
      this.permissions.set(permissions);
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.loading.set(false);
    }
  }
}
