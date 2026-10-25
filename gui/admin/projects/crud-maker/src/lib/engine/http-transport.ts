/**
 * Default `CrudTransport` over `HttpClient`. A thin adapter: REST conventions
 * live here, not in the controller, so an API that speaks another dialect
 * gets a different transport instead of a forked engine.
 */
import { HttpClient, HttpParams } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';
import { normalizeListResult, type ListQuery, type ListResult } from '../state/crud-list-state';
import { encodeListQuery } from './params';
import { CrudTransport, CrudTransportError, type EntityId } from './transport';

export class HttpTransport implements CrudTransport {
  /**
   * @param baseUrl API root, e.g. `https://api.example.com/v1` (no trailing slash needed).
   */
  constructor(
    private readonly http: HttpClient,
    private readonly baseUrl: string,
  ) {}

  async list(endpoint: string, query: ListQuery): Promise<ListResult> {
    const body = await this.request(() =>
      firstValueFrom(
        this.http.get<unknown>(this.url(endpoint), {
          params: toHttpParams(encodeListQuery(query)),
        }),
      ),
    );
    return normalizeListResult(body);
  }

  async get(endpoint: string, id: EntityId): Promise<Record<string, unknown>> {
    return this.request(() => firstValueFrom(this.http.get<Record<string, unknown>>(this.url(endpoint, id))));
  }

  async create(
    endpoint: string,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    return this.request(() => firstValueFrom(this.http.post<Record<string, unknown>>(this.url(endpoint), payload)));
  }

  async update(
    endpoint: string,
    id: EntityId,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    return this.request(() => firstValueFrom(this.http.patch<Record<string, unknown>>(this.url(endpoint, id), payload)));
  }

  async remove(endpoint: string, id: EntityId): Promise<void> {
    await this.request(() => firstValueFrom(this.http.delete<unknown>(this.url(endpoint, id))));
  }

  private async request<T>(run: () => Promise<T>): Promise<T> {
    try {
      return await run();
    } catch (e) {
      if (e instanceof CrudTransportError) throw e;
      const status = (e as { status?: unknown })?.status;
      const statusText = (e as { statusText?: unknown })?.statusText;
      if (typeof status === 'number' && status > 0) {
        throw new CrudTransportError(
          `HTTP ${status}${typeof statusText === 'string' && statusText ? ` ${statusText}` : ''}`,
          status,
        );
      }
      throw new CrudTransportError(e instanceof Error ? e.message : String(e));
    }
  }

  private url(endpoint: string, id?: EntityId): string {
    const base = this.baseUrl.replace(/\/+$/, '');
    const path = endpoint.replace(/^\/+|\/+$/g, '');
    if (id === undefined || id === null) return `${base}/${path}`;
    return `${base}/${path}/${encodeURIComponent(String(id))}`;
  }
}

function toHttpParams(encoded: Record<string, string>): HttpParams {
  let params = new HttpParams();
  for (const [key, value] of Object.entries(encoded)) {
    params = params.set(key, value);
  }
  return params;
}
