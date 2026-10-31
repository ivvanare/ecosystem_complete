/**
 * `crud-detail` — the read-only screen of the ng-zorro adapter (T7.3).
 *
 * A plain definition list on purpose: a detail view is not a table and not a
 * form, and faking one with either makes the read path depend on widgets it
 * does not need. Values still go through `crud-cell`, so cell hints and
 * formatters are identical to the list — the same config, the same rendering.
 *
 * The component renders the record it is given; loading it stays with the
 * controller (`controller.detail(id)`), so the adapter never fetches twice.
 */
import { Component, computed, input } from '@angular/core';
import type { ResolvedCrudConfig, ResolvedField } from 'crud-maker';
import { CrudCell } from '../cell/crud-cell';

@Component({
  selector: 'crud-detail',
  imports: [CrudCell],
  template: `
    @if (value(); as row) {
      <dl class="crud-detail">
        @for (field of fields(); track field.name) {
          <dt>{{ field.label }}</dt>
          <dd><crud-cell [field]="field" [value]="valueOf(row, field)" /></dd>
        }
      </dl>
    } @else {
      <p class="crud-detail-empty">No record to show.</p>
    }
  `,
})
export class CrudDetail {
  readonly config = input.required<ResolvedCrudConfig>();
  readonly value = input<Record<string, unknown> | null>(null);

  readonly fields = computed<readonly ResolvedField[]>(() => this.config().detailFields);

  valueOf(row: Record<string, unknown>, field: ResolvedField): unknown {
    return row[field.name];
  }
}
