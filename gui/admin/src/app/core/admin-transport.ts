import { HttpClient, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { firstValueFrom } from 'rxjs';
import {
  CrudTransportError,
  normalizeListResult,
  type CrudTransport,
  type EntityId,
  type ListQuery,
  type ListResult,
} from 'crud-maker';
import { API_URL } from './api-url.token';

/**
 * `CrudTransport` over this API's dialect, for the screens the engine drives.
 *
 * `crud-maker`'s own `HttpTransport` speaks `perPage` and a `field_kind` filter
 * grammar; this API speaks `page`/`limit`/`q` and a closed set of named filters
 * (`api/openapi.yaml`). The difference lives here instead of in the package, so the
 * adapter stays generic and the API stays the source of truth.
 *
 * Only read endpoints exist for these entities: `get` reaches a detail route the API
 * does not have, and the mutations are domain calls (`PATCH .../roles`), not a
 * generic write. Both fail loudly rather than pretending.
 */

/**
 * Per endpoint: filter field → the query parameter the API actually reads. A filter
 * with no entry is dropped: sending a parameter the API ignores hides the mistake.
 */
const FILTER_PARAMS: Readonly<Record<string, Readonly<Record<string, string>>>> = {
  'admin/users': { status: 'status' },
  'admin/audit-events': {
    event: 'event',
    user_id: 'userId',
    email_hash: 'emailHash',
  },
};

/**
 * Endpoints whose date field travels as two parameters. The engine's `between`
 * arrives as `[from, to]`, and the API wants RFC 3339 while a date input yields a
 * local `YYYY-MM-DD` or `YYYY-MM-DDTHH:mm`.
 */
const RANGE_PARAMS: Readonly<Record<string, { field: string; from: string; to: string }>> = {
  'admin/audit-events': { field: 'created_at', from: 'from', to: 'to' },
};

/** Endpoints that accept the global search box, mapped to its parameter. */
const SEARCH_PARAMS: Readonly<Record<string, string>> = {
  'admin/users': 'q',
};

@Injectable({ providedIn: 'root' })
export class AdminTransport implements CrudTransport {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = inject(API_URL).replace(/\/+$/, '');

  async list(endpoint: string, query: ListQuery): Promise<ListResult> {
    const params = this.listParams(endpoint, query);
    const body = await this.request(() =>
      firstValueFrom(this.http.get<unknown>(this.url(endpoint), { params })),
    );
    // The envelope is `{data,page,limit,total}`; the engine wants `{rows,total}`.
    return normalizeListResult(body);
  }

  get(_endpoint: string, _id: EntityId): Promise<Record<string, unknown>> {
    return Promise.reject(this.unsupported('get'));
  }

  create(_endpoint: string, _payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return Promise.reject(this.unsupported('create'));
  }

  update(
    _endpoint: string,
    _id: EntityId,
    _payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    return Promise.reject(this.unsupported('update'));
  }

  remove(_endpoint: string, _id: EntityId): Promise<void> {
    return Promise.reject(this.unsupported('remove'));
  }

  private listParams(endpoint: string, query: ListQuery): HttpParams {
    const filters = FILTER_PARAMS[endpoint];
    const searchParam = SEARCH_PARAMS[endpoint];
    if (!filters) {
      throw this.unsupported(`list on "${endpoint}"`, endpoint);
    }

    let params = new HttpParams()
      .set('page', String(query.page))
      .set('limit', String(query.pageSize));

    if (searchParam && query.search) {
      params = params.set(searchParam, query.search);
    }

    const range = RANGE_PARAMS[endpoint];

    for (const [rawKey, value] of Object.entries(query.filters)) {
      // The engine keys filters as `field_kind`; the API wants the bare field name.
      const field = rawKey.slice(0, Math.max(rawKey.lastIndexOf('_'), 0)) || rawKey;

      if (range && field === range.field && Array.isArray(value)) {
        const [from, to] = value as [unknown, unknown];
        if (from) params = params.set(range.from, toRfc3339(String(from), 'from'));
        if (to) params = params.set(range.to, toRfc3339(String(to), 'to'));
        continue;
      }

      const param = filters[field];
      if (!param) continue;
      const text = String(value ?? '');
      if (text.trim() === '') continue;
      params = params.set(param, text);
    }

    return params;
  }

  private async request<T>(run: () => Promise<T>): Promise<T> {
    try {
      return await run();
    } catch (error) {
      if (error instanceof CrudTransportError) throw error;
      const status = (error as { status?: unknown })?.status;
      const body = (error as { error?: { error?: { code?: unknown } } })?.error;
      const code = body?.error?.code;
      throw new CrudTransportError(
        typeof code === 'string' ? code : error instanceof Error ? error.message : String(error),
        typeof status === 'number' ? status : null,
      );
    }
  }

  private unsupported(operation: string, endpoint?: string): CrudTransportError {
    const where = endpoint ? ` for "${endpoint}"` : '';
    return new CrudTransportError(`this API has no ${operation}${where}`, null);
  }

  private url(endpoint: string): string {
    return `${this.baseUrl}/${endpoint.replace(/^\/+|\/+$/g, '')}`;
  }
}

/**
 * Normalises a native date input to the RFC 3339 the API parses (`api/API.md` §7).
 * A date-only value becomes the start or the end of that day in UTC; a
 * `datetime-local` value (no zone) is read as UTC too, which is the honest simple
 * reading — the alternative would silently shift the range per browser.
 */
export function toRfc3339(value: string, edge: 'from' | 'to'): string {
  const trimmed = value.trim();
  if (/^\d{4}-\d{2}-\d{2}$/.test(trimmed)) {
    return `${trimmed}T${edge === 'from' ? '00:00:00' : '23:59:59'}Z`;
  }
  if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(trimmed)) {
    return `${trimmed}:00Z`;
  }
  return trimmed;
}
