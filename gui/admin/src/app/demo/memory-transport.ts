/**
 * In-memory `CrudTransport` for demos and local development: implements the
 * same server-side semantics the engine expects (filter kinds, sort,
 * pagination, search over declared fields) so screens work end-to-end
 * before a real API exists. Swap for `HttpTransport` by changing one
 * constructor argument.
 */
import type { CrudTransport, EntityId, ListQuery, ListResult } from 'crud-maker';

type Row = Record<string, unknown>;

export interface MemoryTransportOptions {
  /** Fields searched (case-insensitive) when `query.search` is set. */
  searchFields?: string[];
  /** Simulated latency in ms. Defaults to 120. */
  delayMs?: number;
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function matches(row: Row, rawKey: string, expected: unknown): boolean {
  const split = rawKey.lastIndexOf('_');
  if (split <= 0) return true;
  const field = rawKey.slice(0, split);
  const kind = rawKey.slice(split + 1);
  const actual = row[field];

  switch (kind) {
    case 'like':
      return String(actual ?? '')
        .toLowerCase()
        .includes(String(expected ?? '').toLowerCase());
    case 'eq':
      if (typeof expected === 'boolean') return Boolean(actual) === expected;
      return String(actual) === String(expected);
    case 'gte':
      return Number(actual) >= Number(expected);
    case 'lte':
      return Number(actual) <= Number(expected);
    case 'between': {
      if (!Array.isArray(expected)) return true;
      const [from, to] = expected as [unknown, unknown];
      const value = Number(actual);
      if (from !== null && from !== undefined && value < Number(from)) return false;
      if (to !== null && to !== undefined && value > Number(to)) return false;
      return true;
    }
    case 'in':
      return Array.isArray(expected) && expected.some((option) => String(option) === String(actual));
    default:
      return true;
  }
}

function compare(a: unknown, b: unknown): number {
  if (typeof a === 'number' || typeof b === 'number') {
    return Number(a) - Number(b);
  }
  return String(a ?? '').localeCompare(String(b ?? ''));
}

export class MemoryTransport implements CrudTransport {
  private rows: Row[];
  private nextId: number;
  private readonly options: Required<Pick<MemoryTransportOptions, 'searchFields' | 'delayMs'>>;

  constructor(initial: Row[], options: MemoryTransportOptions = {}) {
    this.rows = initial.map((row) => ({ ...row }));
    const maxId = this.rows.reduce((max, row) => Math.max(max, Number(row['id']) || 0), 0);
    this.nextId = maxId + 1;
    this.options = { searchFields: options.searchFields ?? [], delayMs: options.delayMs ?? 120 };
  }

  async list(endpoint: string, query: ListQuery): Promise<ListResult> {
    await delay(this.options.delayMs);
    void endpoint;

    let filtered = this.rows.filter((row) =>
      Object.entries(query.filters).every(([key, expected]) => matches(row, key, expected)),
    );

    if (query.search) {
      const term = query.search.toLowerCase();
      filtered = filtered.filter((row) =>
        this.options.searchFields.some((field) =>
          String(row[field] ?? '').toLowerCase().includes(term),
        ),
      );
    }

    if (query.sort) {
      const { field, direction } = query.sort;
      filtered = [...filtered].sort((a, b) => {
        const result = compare(a[field], b[field]);
        return direction === 'desc' ? -result : result;
      });
    }

    const start = (query.page - 1) * query.pageSize;
    return {
      rows: filtered.slice(start, start + query.pageSize).map((row) => ({ ...row })),
      total: filtered.length,
    };
  }

  async get(endpoint: string, id: EntityId): Promise<Row> {
    await delay(this.options.delayMs);
    void endpoint;
    const found = this.rows.find((row) => String(row['id']) === String(id));
    if (!found) throw new Error(`MemoryTransport: entity ${String(id)} not found`);
    return { ...found };
  }

  async create(endpoint: string, payload: Row): Promise<Row> {
    await delay(this.options.delayMs);
    void endpoint;
    const created: Row = {
      id: this.nextId++,
      createdAt: new Date().toISOString().slice(0, 19),
      ...payload,
    };
    this.rows = [...this.rows, created];
    return { ...created };
  }

  async update(endpoint: string, id: EntityId, payload: Row): Promise<Row> {
    await delay(this.options.delayMs);
    void endpoint;
    const index = this.rows.findIndex((row) => String(row['id']) === String(id));
    if (index === -1) throw new Error(`MemoryTransport: entity ${String(id)} not found`);
    const updated: Row = { ...this.rows[index], ...payload, id: this.rows[index]!['id'] };
    this.rows = this.rows.map((row, i) => (i === index ? updated : row));
    return { ...updated };
  }

  async remove(endpoint: string, id: EntityId): Promise<void> {
    await delay(this.options.delayMs);
    void endpoint;
    this.rows = this.rows.filter((row) => String(row['id']) !== String(id));
  }
}
