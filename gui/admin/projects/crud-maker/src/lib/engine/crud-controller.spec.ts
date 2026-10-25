import { describe, expect, it } from 'vitest';
import { defineCrudConfig } from '../config/define-config';
import { encodeListQuery } from './params';
import { CrudController, CrudPermissionError } from './crud-controller';
import type { CrudTransport } from './transport';
import type { ListQuery, ListResult } from '../state/crud-list-state';

const config = defineCrudConfig({
  key: 'faqs',
  endpoint: '/faqs/',
  primaryKey: 'id',
  fields: [
    { name: 'id', sortable: false },
    { name: 'question', filters: ['like'] },
    { name: 'rating', type: 'number', filters: ['between'] },
    { name: 'tags', type: 'select', options: [{ label: 'a', value: 'a' }], filters: ['in'] },
    { name: 'archived', type: 'boolean', filters: ['eq'] },
  ],
  pageSize: 10,
});

const tick = () => new Promise((r) => setTimeout(r, 0));

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
}

class FakeTransport implements CrudTransport {
  readonly listCalls: { endpoint: string; query: ListQuery; defer: Deferred<ListResult> }[] = [];
  readonly writes: { method: string; endpoint: string; args: unknown[] }[] = [];

  list(endpoint: string, query: ListQuery): Promise<ListResult> {
    let resolve!: (value: ListResult) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<ListResult>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    this.listCalls.push({ endpoint, query, defer: { promise, resolve, reject } });
    return promise;
  }

  get(endpoint: string, id: unknown): Promise<Record<string, unknown>> {
    this.writes.push({ method: 'get', endpoint, args: [id] });
    return Promise.resolve({ id, question: 'detail' });
  }

  create(endpoint: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    this.writes.push({ method: 'create', endpoint, args: [payload] });
    return Promise.resolve({ id: 99, ...payload });
  }

  update(
    endpoint: string,
    id: unknown,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    this.writes.push({ method: 'update', endpoint, args: [id, payload] });
    return Promise.resolve({ id, ...payload });
  }

  remove(endpoint: string, id: unknown): Promise<void> {
    this.writes.push({ method: 'remove', endpoint, args: [id] });
    return Promise.resolve();
  }
}

function makeController(overrides?: {
  config?: typeof config;
  hooks?: { can?: (action: 'create' | 'read' | 'update' | 'delete') => boolean };
}) {
  const transport = new FakeTransport();
  const controller = new CrudController(overrides?.config ?? config, transport, overrides?.hooks);
  return { transport, controller };
}

describe('encodeListQuery', () => {
  it('encodes pagination, sort, filters and search into REST params', () => {
    const params = encodeListQuery({
      page: 2,
      pageSize: 50,
      sort: { field: 'createdAt', direction: 'desc' },
      filters: {
        'question_like': 'hello',
        'rating_between': [5, null],
        'tags_in': ['a', 'b'],
        'archived_eq': true,
      },
      search: 'text',
    });
    expect(params).toEqual({
      page: '2',
      perPage: '50',
      sort: 'createdAt:DESC',
      question_like: 'hello',
      rating_between: '5,',
      tags_in: 'a,b',
      archived_eq: 'true',
      q: 'text',
    });
  });

  it('omits absent sort, search and empty filter slots', () => {
    const params = encodeListQuery({
      page: 1,
      pageSize: 10,
      sort: null,
      filters: { 'rating_between': [null, 10] },
      search: null,
    });
    expect(params).toEqual({ page: '1', perPage: '10', rating_between: ',10' });
  });
});

describe('CrudController: load', () => {
  it('starts idle and turns the resolved list into rows/total/ready', async () => {
    const { transport, controller } = makeController();
    expect(controller.status()).toBe('idle');
    const pending = controller.load();
    expect(controller.status()).toBe('loading');
    const call = transport.listCalls[0]!;
    expect(call.endpoint).toBe('faqs');
    expect(call.query.page).toBe(1);
    call.defer.resolve({ rows: [{ id: 1, question: 'q' }], total: 42 });
    await pending;
    expect(controller.rows()).toEqual([{ id: 1, question: 'q' }]);
    expect(controller.total()).toBe(42);
    expect(controller.status()).toBe('ready');
  });

  it('surfaces failures without clearing the last good rows', async () => {
    const { transport, controller } = makeController();
    const first = controller.load();
    transport.listCalls[0]!.defer.resolve({ rows: [{ id: 1 }], total: 1 });
    await first;

    const second = controller.load();
    transport.listCalls[1]!.defer.reject(new Error('boom'));
    await second;
    expect(controller.status()).toBe('error');
    expect(controller.error()).toBe('boom');
    expect(controller.rows()).toEqual([{ id: 1 }]);
  });

  it('discards stale responses when a newer load was started (latest wins)', async () => {
    const { transport, controller } = makeController();
    const slow = controller.load();
    const fast = controller.load();
    transport.listCalls[1]!.defer.resolve({ rows: [{ id: 'B' }], total: 1 });
    await fast;
    transport.listCalls[0]!.defer.resolve({ rows: [{ id: 'A' }], total: 1 });
    await slow;
    expect(controller.rows()).toEqual([{ id: 'B' }]);
  });

  it('refuses to read without permission and never touches the transport', async () => {
    const restricted = defineCrudConfig({
      key: 'secret',
      endpoint: 'secret',
      fields: [{ name: 'id' }],
      actions: { read: false },
    });
    const { transport, controller } = makeController({ config: restricted });
    await expect(controller.load()).rejects.toThrow(CrudPermissionError);
    expect(transport.listCalls).toHaveLength(0);
  });
});

describe('CrudController: mutations', () => {
  it('create sends POST-shaped payload without the primary key and reloads', async () => {
    const { transport, controller } = makeController();
    const saved = controller.save({ question: 'new one' });
    await tick();
    expect(transport.writes).toEqual([
      { method: 'create', endpoint: 'faqs', args: [{ question: 'new one' }] },
    ]);
    transport.listCalls.at(-1)!.defer.resolve({ rows: [], total: 0 });
    await expect(saved).resolves.toMatchObject({ id: 99, question: 'new one' });
  });

  it('update targets the primary key', async () => {
    const { transport, controller } = makeController();
    const saved = controller.save({ id: 7, question: 'edited' });
    await tick();
    expect(transport.writes[0]).toEqual({
      method: 'update',
      endpoint: 'faqs',
      args: [7, { id: 7, question: 'edited' }],
    });
    transport.listCalls.at(-1)!.defer.resolve({ rows: [], total: 0 });
    await saved;
  });

  it('removeOne deletes then reloads through the current query', async () => {
    const { transport, controller } = makeController();
    controller.state.setFilter('question', 'like', 'keep');
    const done = controller.removeOne(3);
    await tick();
    expect(transport.writes[0]).toEqual({ method: 'remove', endpoint: 'faqs', args: [3] });
    transport.listCalls.at(-1)!.defer.resolve({ rows: [], total: 0 });
    await done;
    expect(transport.listCalls.at(-1)!.query.filters).toEqual({ question_like: 'keep' });
  });

  it('detail reads one entity through transport.get', async () => {
    const { transport, controller } = makeController();
    await expect(controller.detail(5)).resolves.toEqual({ id: 5, question: 'detail' });
    expect(transport.writes[0]).toEqual({ method: 'get', endpoint: 'faqs', args: [5] });
  });

  it('hooks.can overwrites the config actions matrix', async () => {
    const { transport, controller } = makeController({ hooks: { can: (a) => a !== 'delete' } });
    expect(controller.can('delete')).toBe(false);
    expect(controller.can('create')).toBe(true);
    await expect(controller.removeOne(1)).rejects.toThrow(CrudPermissionError);
    expect(transport.writes).toHaveLength(0);
  });
});
