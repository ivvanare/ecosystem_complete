/**
 * In-memory `CrudTransport` for demos and local development: implements the
 * same server-side semantics the engine expects (filter kinds, sort,
 * pagination, search over declared fields) so screens work end-to-end
 * before a real API exists. Swap for `HttpTransport` by changing one
 * constructor argument.
 *
 * Collections are keyed by endpoint so several entities (e.g. a resource and
 * the category list a select points at) share one transport instance.
 */
import type { CrudTransport, EntityId, ListQuery, ListResult } from 'crud-maker';

type Row = Record<string, unknown>;

export interface MemoryTransportOptions {
  /** Fields searched (case-insensitive) when `query.search` is set. */
  searchFields?: string[];
  /** Simulated latency in ms. Defaults to 120. */
  delayMs?: number;
}

interface Bucket {
  rows: Row[];
  nextId: number;
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
  private readonly buckets = new Map<string, Bucket>();
  private readonly options: Required<Pick<MemoryTransportOptions, 'searchFields' | 'delayMs'>>;

  constructor(collections: Record<string, Row[]>, options: MemoryTransportOptions = {}) {
    for (const [endpoint, rows] of Object.entries(collections)) {
      const copy = rows.map((row) => ({ ...row }));
      const maxId = copy.reduce((max, row) => Math.max(max, Number(row['id']) || 0), 0);
      this.buckets.set(endpoint, { rows: copy, nextId: maxId + 1 });
    }
    this.options = { searchFields: options.searchFields ?? [], delayMs: options.delayMs ?? 120 };
  }

  async list(endpoint: string, query: ListQuery): Promise<ListResult> {
    await delay(this.options.delayMs);
    const bucket = this.bucket(endpoint);

    let filtered = bucket.rows.filter((row) =>
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
    const found = this.bucket(endpoint).rows.find((row) => String(row['id']) === String(id));
    if (!found) throw new Error(`MemoryTransport: entity ${String(id)} not found`);
    return { ...found };
  }

  async create(endpoint: string, payload: Row): Promise<Row> {
    await delay(this.options.delayMs);
    const bucket = this.bucket(endpoint);
    const created: Row = {
      id: bucket.nextId++,
      createdAt: new Date().toISOString().slice(0, 19),
      ...payload,
    };
    bucket.rows = [...bucket.rows, created];
    return { ...created };
  }

  async update(endpoint: string, id: EntityId, payload: Row): Promise<Row> {
    await delay(this.options.delayMs);
    const bucket = this.bucket(endpoint);
    const index = bucket.rows.findIndex((row) => String(row['id']) === String(id));
    if (index === -1) throw new Error(`MemoryTransport: entity ${String(id)} not found`);
    const updated: Row = { ...bucket.rows[index], ...payload, id: bucket.rows[index]!['id'] };
    bucket.rows = bucket.rows.map((row, i) => (i === index ? updated : row));
    return { ...updated };
  }

  async remove(endpoint: string, id: EntityId): Promise<void> {
    await delay(this.options.delayMs);
    const bucket = this.bucket(endpoint);
    bucket.rows = bucket.rows.filter((row) => String(row['id']) !== String(id));
  }

  private bucket(endpoint: string): Bucket {
    const bucket = this.buckets.get(endpoint);
    if (!bucket) throw new Error(`MemoryTransport: unknown endpoint "${endpoint}"`);
    return bucket;
  }
}
