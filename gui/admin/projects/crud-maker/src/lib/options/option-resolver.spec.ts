import { describe, expect, it } from 'vitest';
import { defineCrudConfig } from '../config/define-config';
import type { CrudTransport, EntityId } from '../engine/transport';
import type { ListQuery, ListResult } from '../state/crud-list-state';
import { OptionResolver } from './option-resolver';

const tick = () => new Promise((r) => setTimeout(r, 0));

interface Deferred<T> {
  resolve: (value: T) => void;
  reject: (reason: unknown) => void;
}

class FakeTransport implements CrudTransport {
  readonly listCalls: { endpoint: string; query: ListQuery; defer: Deferred<ListResult> }[] = [];

  list(endpoint: string, query: ListQuery): Promise<ListResult> {
    let resolve!: (value: ListResult) => void;
    let reject!: (reason: unknown) => void;
    const promise = new Promise<ListResult>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    this.listCalls.push({ endpoint, query, defer: { resolve, reject } });
    return promise;
  }

  get(): Promise<Record<string, unknown>> {
    throw new Error('not used');
  }

  create(): Promise<Record<string, unknown>> {
    throw new Error('not used');
  }

  update(): Promise<Record<string, unknown>> {
    throw new Error('not used');
  }

  remove(): Promise<void> {
    throw new Error('not used');
  }
}

const categoriesConfig = defineCrudConfig({
  key: 'faqCategories',
  endpoint: 'faq_categories',
  fields: [
    { name: 'id', sortable: false },
    { name: 'name', defaultSort: 'asc' },
    { name: 'slug' },
  ],
});

const faqsConfig = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  fields: [
    {
      name: 'category',
      type: 'select',
      options: { resource: 'faqCategories', labelField: 'name' },
    },
    {
      name: 'kind',
      type: 'select',
      options: [
        { label: 'Soft', value: 'soft' },
        { label: 'Hard', value: 'hard' },
      ],
    },
    { name: 'title' },
  ],
});

function configs() {
  return { faqCategories: categoriesConfig };
}

describe('OptionResolver', () => {
  it('returns static options without touching the transport', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const options = resolver.forField(faqsConfig.fieldMap.get('kind')!);
    expect(options()).toEqual([
      { label: 'Soft', value: 'soft' },
      { label: 'Hard', value: 'hard' },
    ]);
    await tick();
    expect(transport.listCalls).toHaveLength(0);
  });

  it('returns a stable empty signal for fields without options', () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const first = resolver.forField(faqsConfig.fieldMap.get('title')!);
    const second = resolver.forField(faqsConfig.fieldMap.get('title')!);
    expect(first()).toEqual([]);
    expect(second).toBe(first);
  });

  it('fetches resource options with the referenced config endpoint, pageSize and default sort', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const options = resolver.forField(faqsConfig.fieldMap.get('category')!);
    expect(options()).toEqual([]);

    await tick();
    const call = transport.listCalls[0]!;
    expect(call.endpoint).toBe('faq_categories');
    expect(call.query).toEqual({
      page: 1,
      pageSize: 1000,
      sort: { field: 'name', direction: 'asc' },
      filters: {},
      search: null,
    });

    call.defer.resolve({
      rows: [
        { id: 1, name: 'Reactivity', slug: 'reactivity' },
        { id: 2, name: 'Forms', slug: 'forms' },
      ],
      total: 2,
    });
    await tick();
    expect(options()).toEqual([
      { label: 'Reactivity', value: 1 },
      { label: 'Forms', value: 2 },
    ]);
  });

  it('honors valueField when declared', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const field = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [
        { name: 'c', type: 'select', options: { resource: 'faqCategories', labelField: 'name', valueField: 'slug' } },
      ],
    }).fieldMap.get('c')!;
    const options = resolver.forField(field);
    await tick();
    transport.listCalls[0]!.defer.resolve({ rows: [{ id: 1, name: 'Reactivity', slug: 'reactivity' }], total: 1 });
    await tick();
    expect(options()).toEqual([{ label: 'Reactivity', value: 'reactivity' }]);
  });

  it('fetches once per resource even when several fields share it', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const other = defineCrudConfig({
      key: 'y',
      endpoint: 'y',
      fields: [{ name: 'c', type: 'select', options: { resource: 'faqCategories', labelField: 'slug' } }],
    }).fieldMap.get('c')!;
    const a = resolver.forField(faqsConfig.fieldMap.get('category')!);
    const b = resolver.forField(other);
    expect(b).toBe(a);
    await tick();
    expect(transport.listCalls).toHaveLength(1);
  });

  it('reports unknown resources through the error signal, not silently', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, {});
    const options = resolver.forField(faqsConfig.fieldMap.get('category')!);
    await tick();
    expect(options()).toEqual([]);
    expect(transport.listCalls).toHaveLength(0);
    expect(resolver.error('faqCategories')()).toContain('unknown option resource');
  });

  it('surfaces transport failures in the error signal and keeps options empty', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const options = resolver.forField(faqsConfig.fieldMap.get('category')!);
    await tick();
    transport.listCalls[0]!.defer.reject(new Error('offline'));
    await tick();
    expect(resolver.error('faqCategories')()).toBe('offline');
    expect(options()).toEqual([]);
  });

  it('refresh re-fetches and discards the stale in-flight response', async () => {
    const transport = new FakeTransport();
    const resolver = new OptionResolver(transport, configs());
    const options = resolver.forField(faqsConfig.fieldMap.get('category')!);
    await tick();

    resolver.refresh('faqCategories');
    await tick();
    expect(transport.listCalls).toHaveLength(2);

    // resolve the NEW call first, then let the stale one land
    transport.listCalls[1]!.defer.resolve({ rows: [{ id: 9, name: 'Fresh', slug: 'fresh' }], total: 1 });
    await tick();
    transport.listCalls[0]!.defer.resolve({ rows: [{ id: 1, name: 'Stale', slug: 'stale' }], total: 1 });
    await tick();
    expect(options()).toEqual([{ label: 'Fresh', value: 9 }]);
  });
});
