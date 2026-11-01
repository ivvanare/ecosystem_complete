import { Component, effect, inject, OnDestroy, signal } from '@angular/core';
import { CrudListState, defineCrudConfig, type ListQuery } from 'crud-maker';
import { AdminApi, type AuditEvent } from '../../core/admin-api';
import { apiErrorCode } from '../../core/api-error';
import { formatDateTime } from '../../core/format';

/**
 * The log is append-only and read-only (`api/API.md` §7), so the config exposes
 * exactly one filter — the exact event name — and no mutation action. Paging goes
 * through `CrudListState`; the engine's `event_eq` key is translated to the API's
 * `event` query parameter here, because this API has no filter grammar (L9).
 */
const AUDIT_CONFIG = defineCrudConfig({
  key: 'audit-events',
  endpoint: 'admin/audit-events',
  primaryKey: 'id',
  titleField: 'event',
  pageSize: 20,
  searchFields: [],
  actions: { create: false, update: false, delete: false },
  fields: [
    { name: 'id', in: { list: false, form: false, detail: true } },
    { name: 'event', label: 'Event', filters: ['eq'] },
    { name: 'actor_user_id', label: 'Actor', sortable: false },
    { name: 'ip', label: 'IP', sortable: false },
    { name: 'country', label: 'Country', sortable: false },
    { name: 'created_at', label: 'When', type: 'datetime' },
  ],
});

@Component({
  selector: 'app-audit',
  templateUrl: './audit.html',
  styleUrl: './audit.css',
})
export class Audit implements OnDestroy {
  private readonly api = inject(AdminApi);

  protected readonly list = new CrudListState(AUDIT_CONFIG);
  protected readonly formatDateTime = formatDateTime;

  protected readonly rows = signal<readonly AuditEvent[]>([]);
  protected readonly total = signal(0);
  protected readonly loading = signal(true);
  protected readonly errorCode = signal<string | null>(null);
  protected readonly pageCount = signal(1);

  private filterTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    effect(() => {
      const query = this.list.query();
      void this.fetch(query);
    });
  }

  ngOnDestroy(): void {
    if (this.filterTimer) clearTimeout(this.filterTimer);
  }

  protected onFilter(value: string): void {
    if (this.filterTimer) clearTimeout(this.filterTimer);
    this.filterTimer = setTimeout(() => this.list.setFilter('event', 'eq', value), 250);
  }

  protected clear(): void {
    this.list.clearFilters();
  }

  protected previous(): void {
    if (this.list.query().page > 1) this.list.setPage(this.list.query().page - 1);
  }

  protected next(): void {
    if (this.list.query().page < this.pageCount()) this.list.setPage(this.list.query().page + 1);
  }

  protected retry(): void {
    void this.fetch(this.list.query());
  }

  private async fetch(query: ListQuery): Promise<void> {
    this.loading.set(true);
    this.errorCode.set(null);
    try {
      const filter = this.list.getFilter('event', 'eq');
      const page = await this.api.listAuditEvents({
        page: query.page,
        limit: query.pageSize,
        event: typeof filter === 'string' ? filter : null,
      });
      this.rows.set(page.data);
      this.total.set(page.total);
      this.pageCount.set(Math.max(1, Math.ceil(page.total / query.pageSize)));
    } catch (error) {
      this.errorCode.set(apiErrorCode(error));
    } finally {
      this.loading.set(false);
    }
  }
}
