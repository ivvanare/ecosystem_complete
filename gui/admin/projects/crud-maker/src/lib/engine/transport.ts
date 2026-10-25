/**
 * Transport contract for the CRUD engine. The core never talks to a
 * concrete HTTP client: it works against this interface so engines can be
 * tested with fakes and swapped for REST, GraphQL or mock backends.
 */
import type { ListQuery, ListResult } from '../state/crud-list-state';

export type EntityId = string | number;

export interface CrudTransport {
  list(endpoint: string, query: ListQuery): Promise<ListResult>;
  get(endpoint: string, id: EntityId): Promise<Record<string, unknown>>;
  create(endpoint: string, payload: Record<string, unknown>): Promise<Record<string, unknown>>;
  update(
    endpoint: string,
    id: EntityId,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>>;
  remove(endpoint: string, id: EntityId): Promise<void>;
}

/** Transport-level failure (network, HTTP status, envelope). */
export class CrudTransportError extends Error {
  readonly status: number | null;

  constructor(message: string, status: number | null = null) {
    super(message);
    this.name = 'CrudTransportError';
    this.status = status;
  }
}
