import { Component, inject } from '@angular/core';
import { CrudController, defineCrudConfig } from 'crud-maker';
import { CrudListScreen } from 'crud-maker-zorro';
import { AdminTransport } from '../../core/admin-transport';

/**
 * The audit log as config: read-only, newest first (the API's own order, so every
 * field is unsortable), and exactly one filter — the exact event name — which is the
 * only one `admin/audit-events` accepts.
 *
 * The event code renders as text, not as a tag: `crud-cell`'s `variantMap` matches
 * exact values, and audit events are open-ended (`auth.login.failed`, `role.assigned`,
 * …), so colouring them would need a pattern rule the adapter does not have yet.
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
    { name: 'event', label: 'Event', sortable: false, filters: ['eq'] },
    { name: 'actor_user_id', label: 'Actor', sortable: false },
    { name: 'ip', label: 'Origin IP', sortable: false },
    { name: 'country', label: 'Country', sortable: false },
    { name: 'created_at', label: 'When', type: 'datetime', sortable: false },
  ],
});

/**
 * Audit screen: the listing, its filter and its pagination come from
 * `crud-list-screen`; the page only states what the log is.
 */
@Component({
  selector: 'app-audit',
  imports: [CrudListScreen],
  templateUrl: './audit.html',
  styleUrl: './audit.css',
})
export class Audit {
  protected readonly config = AUDIT_CONFIG;
  protected readonly crud = new CrudController<Record<string, unknown>>(
    AUDIT_CONFIG,
    inject(AdminTransport),
  );

  constructor() {
    void this.crud.load();
  }
}
