import { Component, computed, effect, inject, OnDestroy, signal } from '@angular/core';
import { CrudListState, defineCrudConfig, type ListQuery } from 'crud-maker';
import { AdminApi, type Role, type UserStatus, type UserSummary } from '../../core/admin-api';
import { apiErrorCode } from '../../core/api-error';
import { formatDateTime } from '../../core/format';

/**
 * The users entity as the engine sees it: read-only, because the API exposes no
 * create/update/delete for users — only `PATCH /v1/admin/users/{id}/roles`, which
 * is a domain call, not a generic update. `CrudListState` still owns paging and
 * search so the screen cannot invent a second pagination.
 */
const USERS_CONFIG = defineCrudConfig({
  key: 'users',
  endpoint: 'admin/users',
  primaryKey: 'id',
  titleField: 'email',
  pageSize: 20,
  searchFields: ['email', 'name'],
  actions: { create: false, update: false, delete: false },
  fields: [
    { name: 'id', in: { list: false, form: false, detail: true } },
    { name: 'name', label: 'Name' },
    { name: 'email', label: 'Email', type: 'email' },
    { name: 'status', label: 'Status' },
    { name: 'roles', label: 'Roles', sortable: false },
    { name: 'created_at', label: 'Created', type: 'datetime' },
  ],
});

/** Statuses the API accepts as a filter (`UserStatus` in `api/openapi.yaml`). */
const STATUSES: readonly (UserStatus | '')[] = ['', 'ACTIVE', 'INACTIVE', 'SUSPENDED'];

/**
 * Users screen: list, search, status filter, and the one mutation the API offers —
 * replacing a user's role set (S4, S8). The save sends exactly the checked names
 * because `PATCH .../roles` is a replacement, never a delta.
 */
@Component({
  selector: 'app-users',
  templateUrl: './users.html',
  styleUrl: './users.css',
})
export class Users implements OnDestroy {
  private readonly api = inject(AdminApi);

  protected readonly list = new CrudListState(USERS_CONFIG);
  protected readonly statuses = STATUSES;
  protected readonly formatDateTime = formatDateTime;

  protected readonly rows = signal<readonly UserSummary[]>([]);
  protected readonly total = signal(0);
  protected readonly loading = signal(true);
  protected readonly errorCode = signal<string | null>(null);

  protected readonly roles = signal<readonly Role[]>([]);
  protected readonly editingId = signal<string | null>(null);
  protected readonly draftRoles = signal<readonly string[]>([]);
  protected readonly savingId = signal<string | null>(null);
  protected readonly saveErrorCode = signal<string | null>(null);

  protected readonly pageCount = computed(() =>
    Math.max(1, Math.ceil(this.total() / this.list.query().pageSize)),
  );
  protected readonly hasPrevious = computed(() => this.list.query().page > 1);
  protected readonly hasNext = computed(() => this.list.query().page < this.pageCount());

  private searchTimer: ReturnType<typeof setTimeout> | null = null;
  private filterTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    void this.loadRoles();
    // One effect owns "the query changed, reload": paging, search and the status
    // filter all funnel through `list`, so there is no second source of truth.
    effect(() => {
      const query = this.list.query();
      void this.fetch(query);
    });
  }

  ngOnDestroy(): void {
    if (this.searchTimer) clearTimeout(this.searchTimer);
    if (this.filterTimer) clearTimeout(this.filterTimer);
  }

  protected onSearch(value: string): void {
    if (this.searchTimer) clearTimeout(this.searchTimer);
    this.searchTimer = setTimeout(() => this.list.setSearch(value), 250);
  }

  protected onStatus(value: string): void {
    if (this.filterTimer) clearTimeout(this.filterTimer);
    this.filterTimer = setTimeout(() => {
      this.list.clearFilters();
      if (value !== '') {
        // The API has no filter grammar: the screen maps its own control to
        // `status`, and `list` still validates the value's type.
        this.statusFilter.set(value as UserStatus);
        this.list.setPage(1);
      } else {
        this.statusFilter.set(null);
      }
      this.list.setPage(1);
    }, 200);
  }

  /** Status travels as a plain query param, not through the engine's filter keys. */
  protected readonly statusFilter = signal<UserStatus | null>(null);

  protected previous(): void {
    if (this.hasPrevious()) this.list.setPage(this.list.query().page - 1);
  }

  protected next(): void {
    if (this.hasNext()) this.list.setPage(this.list.query().page + 1);
  }

  protected retry(): void {
    void this.fetch(this.list.query());
  }

  protected openRoles(user: UserSummary): void {
    this.saveErrorCode.set(null);
    this.editingId.set(this.editingId() === user.id ? null : user.id);
    this.draftRoles.set([...user.roles]);
  }

  protected isChecked(role: Role): boolean {
    return this.draftRoles().includes(role.name);
  }

  protected toggleRole(role: Role): void {
    const current = this.draftRoles();
    this.draftRoles.set(
      current.includes(role.name) ? current.filter((name) => name !== role.name) : [...current, role.name],
    );
  }

  protected async saveRoles(user: UserSummary): Promise<void> {
    if (this.savingId() !== null) return;
    this.savingId.set(user.id);
    this.saveErrorCode.set(null);
    try {
      const updated = await this.api.setUserRoles(user.id, [...this.draftRoles()]);
      this.rows.set(this.rows().map((row) => (row.id === updated.id ? updated : row)));
      this.editingId.set(null);
    } catch (error) {
      this.saveErrorCode.set(apiErrorCode(error));
    } finally {
      this.savingId.set(null);
    }
  }

  private async loadRoles(): Promise<void> {
    try {
      this.roles.set(await this.api.listRoles());
    } catch {
      // The list is still useful without the editor; the save path reports its own
      // failure. Nothing to tell the user until they try to edit.
      this.roles.set([]);
    }
  }

  private async fetch(query: ListQuery): Promise<void> {
    this.loading.set(true);
    this.errorCode.set(null);
    try {
      const page = await this.api.listUsers({
        page: query.page,
        limit: query.pageSize,
        q: query.search,
        status: this.statusFilter(),
      });
      this.rows.set(page.data);
      this.total.set(page.total);
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.loading.set(false);
    }
  }
}
