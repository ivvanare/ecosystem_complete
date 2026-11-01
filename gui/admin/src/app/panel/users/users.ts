import { Component, computed, inject, signal } from '@angular/core';
import { CrudController, defineCrudConfig } from 'crud-maker';
import { CrudListScreen } from 'crud-maker-zorro';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzModalModule } from 'ng-zorro-antd/modal';
import { AdminApi, type Role, type UserStatus, type UserSummary } from '../../core/admin-api';
import { AdminTransport } from '../../core/admin-transport';
import { apiErrorCode } from '../../core/api-error';

/**
 * The users entity as JSON-shaped config: the engine derives columns, search and
 * the status filter from it, and `admin/users` is the endpoint `AdminTransport`
 * knows how to speak. No create/update/delete: the API exposes none for users, so
 * every mutation here is the domain call below (`PATCH .../roles`).
 */
const USERS_CONFIG = defineCrudConfig({
  key: 'users',
  endpoint: 'admin/users',
  primaryKey: 'id',
  titleField: 'email',
  pageSize: 20,
  searchFields: ['email', 'name'],
  // `update: true` is what makes the table render its Edit button; the screen turns
  // that event into the profile dialog. Creation and deletion stay out: accounts are
  // invited, never created or deleted from here.
  actions: { create: false, update: true, delete: false },
  fields: [
    // The primary key must be declared, but it is not a column.
    { name: 'id', in: { list: false, form: false, detail: true } },
    // Every field is unsortable on purpose: this API takes no sort parameter, so a
    // sort affordance would change state and return the same order.
    { name: 'name', label: 'Name', sortable: false },
    { name: 'email', label: 'Email', type: 'email', sortable: false },
    {
      name: 'status',
      label: 'Status',
      sortable: false,
      filters: ['eq'],
      options: [
        { label: 'Active', value: 'ACTIVE' },
        { label: 'Inactive', value: 'INACTIVE' },
        { label: 'Suspended', value: 'SUSPENDED' },
      ],
      cell: { render: 'tag', variantMap: { ACTIVE: 'green', INACTIVE: 'default', SUSPENDED: 'orange' } },
    },
    { name: 'roles', label: 'Roles', sortable: false },
    {
      // Not a column: the API answers `scope` from the roles' permissions, so it can
      // only narrow the list. `detail: true` keeps the validator happy about having a
      // view, while the flag stays out of the form.
      name: 'scope',
      label: 'Scope',
      sortable: false,
      in: { list: false, form: false, detail: true },
      filters: ['eq'],
      options: [
        { label: 'Panel access', value: 'admin' },
        { label: 'App users', value: 'app' },
        { label: 'No role', value: 'none' },
      ],
    },
    { name: 'created_at', label: 'Created', type: 'datetime', sortable: false },
  ],
});

/**
 * Users screen (S5): the listing, its search and its filter come from
 * `crud-list-screen`; the only thing written by hand is the domain action the
 * adapter cannot know about — replacing a user's role set.
 */
@Component({
  selector: 'app-users',
  imports: [CrudListScreen, NzButtonModule, NzModalModule],
  templateUrl: './users.html',
  styleUrl: './users.css',
})
export class Users {
  private readonly api = inject(AdminApi);

  protected readonly config = USERS_CONFIG;
  protected readonly crud = new CrudController<UserSummary>(USERS_CONFIG, inject(AdminTransport));

  /**
   * The header line reads the controller's `total` signal inside a `computed`, so it
   * recomputes only when the total changes — never once per change-detection pass.
   */
  protected readonly subtitle = computed(() => {
    const total = this.crud.total();
    return `${total} ${total === 1 ? 'account' : 'accounts'} · roles are replaced, never merged`;
  });

  protected readonly roles = signal<readonly Role[]>([]);
  protected readonly editingUser = signal<UserSummary | null>(null);
  protected readonly rolesModalOpen = signal(false);

  protected readonly profileUser = signal<UserSummary | null>(null);
  protected readonly profileModalOpen = signal(false);
  protected readonly profileName = signal('');
  protected readonly profileStatus = signal<UserStatus>('ACTIVE');
  protected readonly profileSaving = signal(false);
  protected readonly profileErrorCode = signal<string | null>(null);
  protected readonly statuses: readonly UserStatus[] = ['ACTIVE', 'INACTIVE', 'SUSPENDED'];
  protected readonly draftRoles = signal<readonly string[]>([]);
  protected readonly saving = signal(false);
  protected readonly saveErrorCode = signal<string | null>(null);

  constructor() {
    void this.crud.load();
    void this.loadRoles();
  }

  protected openRoles(user: UserSummary): void {
    this.saveErrorCode.set(null);
    this.editingUser.set(user);
    this.draftRoles.set([...user.roles]);
    this.rolesModalOpen.set(true);
  }

  protected closeRoles(): void {
    this.rolesModalOpen.set(false);
    this.editingUser.set(null);
    this.saveErrorCode.set(null);
  }

  protected openProfile(user: UserSummary): void {
    this.profileErrorCode.set(null);
    this.profileUser.set(user);
    this.profileName.set(user.name ?? '');
    this.profileStatus.set(user.status);
    this.profileModalOpen.set(true);
  }

  protected closeProfile(): void {
    this.profileModalOpen.set(false);
    this.profileUser.set(null);
    this.profileErrorCode.set(null);
  }

  protected onProfileName(value: string): void {
    this.profileName.set(value);
  }

  protected onProfileStatus(value: string): void {
    this.profileStatus.set(value as UserStatus);
  }

  protected async saveProfile(): Promise<void> {
    const user = this.profileUser();
    if (!user || this.profileSaving()) return;
    this.profileSaving.set(true);
    this.profileErrorCode.set(null);
    try {
      await this.api.updateUser(user.id, {
        name: this.profileName().trim(),
        status: this.profileStatus(),
      });
      await this.crud.load();
      this.closeProfile();
    } catch (error) {
      this.profileErrorCode.set(apiErrorCode(error));
    } finally {
      this.profileSaving.set(false);
    }
  }

  protected isChecked(role: Role): boolean {
    return this.draftRoles().includes(role.name);
  }

  protected toggleRole(role: Role): void {
    const current = this.draftRoles();
    this.draftRoles.set(
      current.includes(role.name)
        ? current.filter((name) => name !== role.name)
        : [...current, role.name],
    );
  }

  /** The endpoint replaces the set, so exactly the checked names are sent. */
  protected async saveRoles(): Promise<void> {
    const user = this.editingUser();
    if (!user || this.saving()) return;
    this.saving.set(true);
    this.saveErrorCode.set(null);
    try {
      await this.api.setUserRoles(user.id, [...this.draftRoles()]);
      await this.crud.load();
      this.closeRoles();
    } catch (error) {
      this.saveErrorCode.set(apiErrorCode(error));
    } finally {
      this.saving.set(false);
    }
  }

  private async loadRoles(): Promise<void> {
    try {
      this.roles.set(await this.api.listRoles());
    } catch {
      // The listing still works without the catalogue; the editor says so.
      this.roles.set([]);
    }
  }
}
