/**
 * `crud-table` — the list screen of the ng-zorro adapter (T7.1).
 *
 * It owns exactly one thing the core cannot: the translation between zorro's
 * vocabulary (`nzSortOrder` = `'ascend' | 'descend' | null`, page indexes from
 * its paginator) and the core's `CrudListState`. Everything visible comes from
 * the core — columns are `listFields`, cells go through `crud-cell`, rows and
 * totals are the controller's signals — so a second adapter shows the same data
 * for the same config.
 *
 * The table also owns the refetch that a query change implies; the consumer
 * still calls `controller.load()` once to populate the first page.
 */
import { Component, computed, input, output } from '@angular/core';
import {
  CrudController,
  type ResolvedCrudConfig,
  type ResolvedField,
  type SortDirection,
} from 'crud-maker';
import { NzButtonModule } from 'ng-zorro-antd/button';
import { NzTableModule, type NzTableSortOrder } from 'ng-zorro-antd/table';
import { CrudCell } from '../cell/crud-cell';

@Component({
  selector: 'crud-table',
  imports: [NzTableModule, NzButtonModule, CrudCell],
  template: `
    <nz-table
      #table
      [nzData]="rows()"
      [nzFrontPagination]="false"
      [nzTotal]="total()"
      [nzPageIndex]="page()"
      [nzPageSize]="pageSize()"
      [nzLoading]="isLoading()"
      [nzShowSizeChanger]="false"
      [nzShowTotal]="totalTemplate"
      nzSize="middle"
      (nzPageIndexChange)="onPage($event)"
    >
      <thead>
        <tr>
          @for (field of columns(); track field.name) {
            <th
              [nzSortOrder]="sortOrder(field)"
              [nzSortFn]="false"
              [nzShowSort]="field.sortable"
              (nzSortOrderChange)="onSort(field, $event)"
            >
              {{ field.label }}
            </th>
          }
          @if (hasActions()) {
            <th>Actions</th>
          }
        </tr>
      </thead>
      <tbody>
        @for (row of rows(); track rowKey(row)) {
          <tr>
            @for (field of columns(); track field.name) {
              <td>
                <crud-cell [field]="field" [value]="cellValue(row, field)" />
              </td>
            }
            @if (hasActions()) {
              <td>
                @if (canUpdate()) {
                  <button nz-button nzSize="small" (click)="edit.emit(row)">Edit</button>
                }
                @if (canDelete()) {
                  <button nz-button nzSize="small" nzDanger (click)="remove.emit(row)">Delete</button>
                }
              </td>
            }
          </tr>
        }
      </tbody>
    </nz-table>

    <ng-template #totalTemplate let-total>Total {{ total }} items</ng-template>
  `,
})
export class CrudTable<T extends object = Record<string, unknown>> {
  readonly config = input.required<ResolvedCrudConfig>();
  readonly controller = input.required<CrudController<T>>();

  /** A row the consumer should open for editing; the adapter never mutates. */
  readonly edit = output<T>();
  /** A row the consumer should delete, after its own confirmation. */
  readonly remove = output<T>();

  readonly columns = computed<readonly ResolvedField[]>(() => this.config().listFields);
  readonly rows = computed<readonly T[]>(() => this.controller().rows());
  readonly total = computed(() => this.controller().total());
  readonly page = computed(() => this.controller().state.page());
  readonly pageSize = computed(() => this.controller().state.pageSize());
  readonly isLoading = computed(() => this.controller().status() === 'loading');
  readonly canUpdate = computed(() => this.controller().can('update'));
  readonly canDelete = computed(() => this.controller().can('delete'));
  readonly hasActions = computed(() => this.canUpdate() || this.canDelete());

  sortOrder(field: ResolvedField): NzTableSortOrder {
    const sort = this.controller().state.sort();
    if (!sort || sort.field !== field.name) return null;
    return sort.direction === 'asc' ? 'ascend' : 'descend';
  }

  /**
   * zorro already decided the next order, so the state is set to that order.
   * `CrudListState.setSort` cycles on its own, which is why clearing first is
   * required when the same column must land on a direction it would not cycle
   * into — otherwise the two cycle models disagree.
   */
  onSort(field: ResolvedField, order: NzTableSortOrder): void {
    if (!field.sortable) return;
    const state = this.controller().state;
    if (order === null) {
      state.clearSort();
      void this.controller().load();
      return;
    }
    const target: SortDirection = order === 'ascend' ? 'asc' : 'desc';
    const current = state.sort();
    if (current?.field === field.name && current.direction === target) return;
    if (current?.field === field.name) state.clearSort();
    state.setSort(field.name, target);
    void this.controller().load();
  }

  onPage(page: number): void {
    if (!Number.isInteger(page) || page < 1) return;
    this.controller().state.setPage(page);
    void this.controller().load();
  }

  rowKey(row: T): unknown {
    return (row as Record<string, unknown>)[this.config().primaryKey];
  }

  cellValue(row: T, field: ResolvedField): unknown {
    return (row as Record<string, unknown>)[field.name];
  }
}
