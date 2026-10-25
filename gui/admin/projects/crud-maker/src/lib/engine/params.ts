/**
 * REST parameter encoding for a `ListQuery`: the canonical internal shape is
 * flattened into the string params a backend expects. Swap the transport
 * (not this file) when an API speaks a different query dialect.
 */
import type { ListQuery } from '../state/crud-list-state';

export function encodeListQuery(query: ListQuery): Record<string, string> {
  const params: Record<string, string> = {
    page: String(query.page),
    perPage: String(query.pageSize),
  };
  if (query.sort) {
    params['sort'] = `${query.sort.field}:${query.sort.direction.toUpperCase()}`;
  }
  for (const [key, value] of Object.entries(query.filters)) {
    params[key] = encodeFilterValue(value);
  }
  if (query.search) params['q'] = query.search;
  return params;
}

function encodeFilterValue(value: unknown): string {
  if (Array.isArray(value)) {
    return value.map((part) => (part === null || part === undefined ? '' : String(part))).join(',');
  }
  if (value === null || value === undefined) return '';
  return String(value);
}
