/**
 * `crud-cell` — the smallest real adapter (S9): it renders a core `CellView`
 * with ng-zorro primitives and decides nothing else.
 *
 * The core already resolved the text and the variant, so this component never
 * formats or inspects the raw value. What it does own is the mapping from the
 * declarative hint to its own vocabulary, and that mapping is not symmetric:
 * `nz-tag` accepts any color name, so the variant passes through untouched,
 * while `nz-badge` takes a closed status union. An unknown variant therefore
 * degrades to `default` instead of pushing a free-form string into a typed API.
 */
import { Component, computed, input } from '@angular/core';
import { resolveCellView, type CellView, type ResolvedField } from 'crud-maker';
import { NzBadgeModule, type NzBadgeStatusType } from 'ng-zorro-antd/badge';
import { NzTagModule, type NzTagColor } from 'ng-zorro-antd/tag';

const NZ_STATUSES: readonly NzBadgeStatusType[] = [
  'success',
  'processing',
  'default',
  'error',
  'warning',
];

@Component({
  selector: 'crud-cell',
  imports: [NzBadgeModule, NzTagModule],
  template: `
    @switch (view().kind) {
      @case ('badge') {
        <nz-badge [nzStatus]="status()" [nzText]="view().text" />
      }
      @case ('tag') {
        <nz-tag [nzColor]="color()">{{ view().text }}</nz-tag>
      }
      @case ('dot') {
        <nz-badge [nzStatus]="status()" />
      }
      @default {
        {{ view().text }}
      }
    }
  `,
})
export class CrudCell {
  readonly field = input.required<ResolvedField>();
  readonly value = input<unknown>();

  readonly view = computed<CellView>(() => resolveCellView(this.field(), this.value()));

  /** Variant mapped onto the closed zorro status union; never the raw hint. */
  readonly status = computed<NzBadgeStatusType>(() => toStatus(this.view().variant));

  /** Variant as a zorro tag color, or undefined when the value declares none. */
  readonly color = computed<NzTagColor | undefined>(() => this.view().variant ?? undefined);
}

function toStatus(variant: string | null): NzBadgeStatusType {
  const safe = variant ?? '';
  return NZ_STATUSES.find((status) => status === safe) ?? 'default';
}
