/**
 * The smallest unit of the adapter contract (S7/S8): a resolved field plus a
 * raw value become a presentation view.
 *
 * This is pure presentation *logic* with no UI dependency, so it belongs to the
 * core: every adapter — `crud-maker-zorro`, a future `crud-maker-primeng`, or a
 * screen a project writes by hand — renders the same `CellView` for the same
 * config, and none of them re-derives formatting or variants. An adapter maps
 * `kind` and `variant` to its own primitives and decides nothing else.
 */
import type { CellRenderKind, ResolvedField } from '../config/crud-config.types';
import { formatValue } from '../config/registry';

export interface CellView {
  /** Declared rendering hint; the adapter owns what a badge/tag/dot looks like. */
  readonly kind: CellRenderKind;
  /** Formatted, display-ready text. Never null: absent values render as `''`. */
  readonly text: string;
  /** Presentation variant from `variantMap`, or null when the value declares none. */
  readonly variant: string | null;
}

export function resolveCellView(field: ResolvedField, value: unknown): CellView {
  return {
    kind: field.cell.render,
    text: formatValue(field.formatter, value),
    variant: resolveVariant(field.cell.variantMap, value),
  };
}

/**
 * Keys are the raw stringified value, as documented on `CellHint`. Reading the
 * map directly (instead of `in` or `hasOwnProperty`) keeps inherited keys such
 * as `toString` or `__proto__` from ever matching: only string entries count.
 */
function resolveVariant(map: Readonly<Record<string, string>>, value: unknown): string | null {
  if (value === null || value === undefined) return null;
  const hit: unknown = map[String(value)];
  return typeof hit === 'string' ? hit : null;
}
