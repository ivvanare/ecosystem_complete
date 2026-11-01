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
    // A range: the toolbar renders a from/to pair and the transport splits it into
    // the API's `from`/`to`.
    {
      name: 'created_at',
      label: 'When',
      type: 'datetime',
      sortable: false,
      width: 'narrow',
      filters: ['between'],
    },
    // Who the event is about, and who caused it when they differ (an admin acting
    // on someone else's account). Both are optional: a failed login has neither.
    {
      name: 'user_id',
      label: 'User',
      sortable: false,
      filters: ['eq'],
      formatter: { name: 'truncate', params: { length: 12 } },
    },
    { name: 'actor_user_id', label: 'Actor', sortable: false, formatter: { name: 'truncate', params: { length: 12 } } },
    // Never a clear address: the API stores an HMAC-SHA256 (api/API.md §7).
    {
      name: 'email_hash',
      label: 'Email hash',
      sortable: false,
      filters: ['eq'],
      formatter: { name: 'truncate', params: { length: 12 } },
    },
    { name: 'ip', label: 'IP', sortable: false, width: 'narrow' },
    { name: 'country', label: 'Country', sortable: false, width: 'narrow' },
    { name: 'city', label: 'City', sortable: false, width: 'narrow' },
    { name: 'user_agent', label: 'Agent', sortable: false, formatter: { name: 'truncate', params: { length: 34 } } },
    // `metadata` is a JSON object and the cell renders text: it would print
    // "[object Object]". It stays out of the list until a JSON cell exists.
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
