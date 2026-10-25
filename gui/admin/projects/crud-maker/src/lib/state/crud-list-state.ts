/**
 * Signal-backed list state for a CRUD resource: page, pageSize, sort,
 * filters and global search live as signals; the transport-ready
 * `ListQuery` is a pure `computed` derivation. Every setter enforces the
 * config contract (sortable fields, exposed filter kinds, numeric/boolean
 * coercion) so malformed values can never reach the network.
 */
import { computed, signal, type Signal, type WritableSignal } from '@angular/core';
import type { FilterKind, ResolvedCrudConfig, SortDirection } from '../config/crud-config.types';

export interface SortState {
  field: string;
  direction: SortDirection;
}

/** Filter key convention: `${fieldName}_${kind}` (mirrors common REST conventions). */
export type FilterKey = string;

export interface ListQuery {
  page: number;
  pageSize: number;
  sort: SortState | null;
  filters: Readonly<Record<FilterKey, unknown>>;
  search: string | null;
}

export interface ListResult<T = Record<string, unknown>> {
  rows: T[];
  total: number;
}

const MAX_PAGE_SIZE = 1000;

function requirePositiveInt(value: number, label: string): void {
  if (!Number.isInteger(value) || value < 1) {
    throw new Error(`${label} must be an integer >= 1, got ${String(value)}`);
  }
}

/** Coerce a filter value for the field's declared type. `undefined` means "drop the filter". */
function coerceFilterValue(rawField: unknown, typeName: string, kind: FilterKind, label: string): unknown {
  if (rawField === null || rawField === undefined) return undefined;
  if (typeof rawField === 'string') {
    const trimmed = rawField.trim();
    if (trimmed === '') return undefined;
    if (typeName === 'number') {
      const numeric = Number(trimmed);
      if (Number.isNaN(numeric)) throw new Error(`${label}: "${rawField}" is not numeric`);
      return numeric;
    }
    if (typeName === 'boolean') {
      if (trimmed === 'true') return true;
      if (trimmed === 'false') return false;
      throw new Error(`${label}: "${rawField}" is not boolean`);
    }
    return trimmed;
  }
  if (typeName === 'number') {
    if (typeof rawField !== 'number' || Number.isNaN(rawField)) {
      throw new Error(`${label}: ${String(rawField)} is not numeric`);
    }
    return rawField;
  }
  if (typeName === 'boolean' && typeof rawField !== 'boolean') {
    throw new Error(`${label}: ${String(rawField)} is not boolean`);
  }
  return rawField;
}

export class CrudListState {
  readonly page: WritableSignal<number>;
  readonly pageSize: WritableSignal<number>;
  readonly sort: WritableSignal<SortState | null>;
  readonly filters: WritableSignal<Readonly<Record<FilterKey, unknown>>>;
  readonly search: WritableSignal<string | null>;

  /** The transport-ready query, recomputed only when its inputs change. */
  readonly query: Signal<ListQuery>;

  constructor(readonly config: ResolvedCrudConfig) {
    this.page = signal(1);
    this.pageSize = signal(config.pageSize);
    this.sort = signal(config.defaultSort ? { ...config.defaultSort } : null);
    this.filters = signal({});
    this.search = signal(null);
    this.query = computed(() => ({
      page: this.page(),
      pageSize: this.pageSize(),
      sort: this.sort(),
      filters: this.filters(),
      search: this.search(),
    }));
  }

  setPage(page: number): void {
    requirePositiveInt(page, 'page');
    this.page.set(page);
  }

  setPageSize(size: number): void {
    requirePositiveInt(size, 'pageSize');
    if (size > MAX_PAGE_SIZE) throw new Error(`pageSize must be <= ${MAX_PAGE_SIZE}`);
    this.pageSize.set(size);
    this.page.set(1);
  }

  /**
   * Click-cycle semantics: same field flips asc → desc → none; a new field
   * starts at asc. Changing sort resets to page 1.
   */
  setSort(field: string, direction?: SortDirection): void {
    const target = this.config.fieldMap.get(field);
    if (!target) throw new Error(`setSort: unknown field "${field}"`);
    if (!target.sortable) throw new Error(`setSort: field "${field}" is not sortable`);
    const current = this.sort();
    let next: SortState | null;
    if (current && current.field === field) {
      next = current.direction === 'asc' ? { field, direction: 'desc' } : null;
    } else {
      next = { field, direction: direction ?? 'asc' };
    }
    this.sort.set(next);
    this.page.set(1);
  }

  /** `setSort(null)` equivalent for headers that clear sorting entirely. */
  clearSort(): void {
    this.sort.set(null);
    this.page.set(1);
  }

  setFilter(field: string, kind: FilterKind, value: unknown): void {
    const target = this.config.fieldMap.get(field);
    if (!target) throw new Error(`setFilter: unknown field "${field}"`);
    if (!target.filters.includes(kind)) {
      throw new Error(`setFilter: kind "${kind}" not exposed for field "${field}"`);
    }
    const key = `${field}_${kind}`;
    const normalized = this.normalizeFilterValue(target.type, kind, value, key);
    const current = this.filters();
    if (normalized === undefined) {
      if (!(key in current)) return;
      const next = { ...current };
      delete next[key];
      this.filters.set(next);
    } else {
      if (current[key] === normalized && key in current) return;
      this.filters.set({ ...current, [key]: normalized });
    }
    this.page.set(1);
  }

  getFilter(field: string, kind: FilterKind): unknown {
    return this.filters()[`${field}_${kind}`];
  }

  clearFilters(): void {
    this.filters.set({});
    this.page.set(1);
  }

  setSearch(term: string | null): void {
    const trimmed = typeof term === 'string' ? term.trim() : null;
    this.search.set(trimmed === '' ? null : trimmed);
    this.page.set(1);
  }

  /** Restore every signal to the config-derived defaults. */
  reset(): void {
    this.page.set(1);
    this.pageSize.set(this.config.pageSize);
    this.sort.set(this.config.defaultSort ? { ...this.config.defaultSort } : null);
    this.filters.set({});
    this.search.set(null);
  }

  private normalizeFilterValue(typeName: string, kind: FilterKind, value: unknown, label: string): unknown {
    if (kind === 'between') {
      if (!Array.isArray(value) || value.length !== 2) {
        throw new Error(`${label}: between requires a [from, to] array`);
      }
      const from = coerceFilterValue(value[0], typeName, kind, `${label}[0]`);
      const to = coerceFilterValue(value[1], typeName, kind, `${label}[1]`);
      if (from === undefined && to === undefined) return undefined;
      return [from ?? null, to ?? null];
    }
    if (kind === 'in') {
      if (!Array.isArray(value)) throw new Error(`${label}: in requires an array`);
      return value.length === 0 ? undefined : [...value];
    }
    if (Array.isArray(value)) throw new Error(`${label}: "${kind}" does not accept arrays`);
    return coerceFilterValue(value, typeName, kind, label);
  }
}

/**
 * Accept the list envelope conventions seen across backends and return a
 * normalized `{rows, total}`. Unknown shapes throw instead of degrading to
 * an empty page (which would hide integration bugs).
 */
export function normalizeListResult<T = Record<string, unknown>>(payload: unknown): ListResult<T> {
  if (Array.isArray(payload)) {
    return { rows: payload as T[], total: payload.length };
  }
  if (typeof payload === 'object' && payload !== null) {
    const record = payload as Record<string, unknown>;
    for (const rowsKey of ['items', 'data', 'results'] as const) {
      const rows = record[rowsKey];
      if (Array.isArray(rows)) {
        const meta = record['meta'];
        const metaTotal = typeof meta === 'object' && meta !== null
          ? (meta as Record<string, unknown>)['total'] ?? (meta as Record<string, unknown>)['count']
          : undefined;
        const total = record['total'] ?? record['count'] ?? metaTotal;
        return {
          rows: rows as T[],
          total: typeof total === 'number' && Number.isFinite(total) ? total : rows.length,
        };
      }
    }
  }
  throw new Error(`unrecognized list payload: ${JSON.stringify(payload)}`);
}
