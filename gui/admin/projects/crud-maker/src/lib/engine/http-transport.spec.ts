import { HttpParams } from '@angular/common/http';
import { of, throwError } from 'rxjs';
import { describe, expect, it } from 'vitest';
import { HttpTransport } from './http-transport';
import { CrudTransportError } from './transport';

interface CallLog {
  method: string;
  url: string;
  options: unknown;
  body?: unknown;
}

function httpClientStub(responses: Record<string, unknown>) {
  const calls: CallLog[] = [];
  const record =
    (method: string) =>
    (url: string, a: unknown, b?: unknown) => {
      const body = method === 'get' || method === 'delete' ? undefined : (a as unknown);
      const options = method === 'get' || method === 'delete' ? (a as unknown) : b;
      calls.push({ method, url, options, body });
      const key = `${method} ${url}`;
      const value = responses[key] ?? responses[method];
      return value instanceof Error ? throwError(() => value) : of(value);
    };
  const http = {
    get: record('get'),
    post: record('post'),
    patch: record('patch'),
    delete: record('delete'),
  };
  return { http: http as never, calls };
}

const baseQuery = {
  page: 1,
  pageSize: 10,
  sort: { field: 'createdAt', direction: 'desc' as const },
  filters: { 'question_like': 'hi' },
  search: null,
};

describe('HttpTransport', () => {
  it('GETs the collection URL with encoded params and normalizes the envelope', async () => {
    const { http, calls } = httpClientStub({ get: { items: [{ id: 1 }], total: 7 } });
    const transport = new HttpTransport(http, 'https://api.test/v1/');
    const result = await transport.list('faqs', baseQuery);
    expect(calls[0]!.method).toBe('get');
    expect(calls[0]!.url).toBe('https://api.test/v1/faqs');
    const params = calls[0]!.options as { params: HttpParams };
    expect(params.params.toString()).toContain('page=1');
    expect(params.params.get('sort')).toBe('createdAt:DESC');
    expect(params.params.toString()).toContain('question_like=hi');
    expect(result).toEqual({ rows: [{ id: 1 }], total: 7 });
  });

  it('POSTs creates, PATCHes updates and DELETEs removals', async () => {
    const { http, calls } = httpClientStub({ post: { id: 1 }, patch: { id: 2 }, delete: null });
    const transport = new HttpTransport(http, 'https://api.test');
    await transport.create('faqs', { question: 'a' });
    await transport.update('faqs', 2, { question: 'b' });
    await transport.remove('faqs', 3);
    expect(calls.map((c) => [c.method, c.url])).toEqual([
      ['post', 'https://api.test/faqs'],
      ['patch', 'https://api.test/faqs/2'],
      ['delete', 'https://api.test/faqs/3'],
    ]);
    expect(calls[0]!.body).toEqual({ question: 'a' });
    expect(calls[1]!.body).toEqual({ question: 'b' });
  });

  it('wraps HTTP failures into CrudTransportError with the status code', async () => {
    const httpError = Object.assign(new Error('Http failure'), { status: 404, statusText: 'Not Found' });
    const { http } = httpClientStub({ get: httpError });
    const transport = new HttpTransport(http, 'https://api.test');
    await expect(transport.list('faqs', baseQuery)).rejects.toThrowError(CrudTransportError);
    await transport.list('faqs', baseQuery).catch((e: CrudTransportError) => {
      expect(e.status).toBe(404);
      expect(e.message).toContain('404');
    });
  });
});
