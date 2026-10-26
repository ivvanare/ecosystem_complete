import { describe, expect, it } from 'vitest';
import { MemoryTransport } from './memory-transport';
import type { ListQuery } from 'crud-maker';

const row = (id: number, question: string, views: number, published: boolean) => ({
  id,
  question,
  views,
  published,
  createdAt: `2026-01-0${id}T10:00:00`,
});

const seed = [
  row(1, 'How do signals work?', 100, true),
  row(2, 'How do forms work?', 50, false),
  row(3, 'What about http?', 300, true),
  row(4, 'Zoneless meaning?', 25, false),
  row(5, 'Signal forms guide?', 175, true),
];

function query(overrides: Partial<ListQuery> = {}): ListQuery {
  return { page: 1, pageSize: 10, sort: null, filters: {}, search: null, ...overrides };
}

function transport(): MemoryTransport {
  return new MemoryTransport(seed, { searchFields: ['question'] });
}

describe('MemoryTransport', () => {
  it('lists a page and reports the total', async () => {
    const result = await transport().list('faqs', query({ page: 2, pageSize: 2 }));
    expect(result.total).toBe(5);
    expect(result.rows.map((r) => r['id'])).toEqual([3, 4]);
  });

  it('applies like, eq, gte and between filters', async () => {
    const t = transport();
    const total = async (overrides: Partial<ListQuery>) =>
      (await t.list('faqs', query(overrides))).total;
    expect(await total({ filters: { question_like: 'SIGNAL' } })).toBe(2);
    expect(await total({ filters: { published_eq: true } })).toBe(3);
    expect(await total({ filters: { views_gte: 100 } })).toBe(3);
    expect(await total({ filters: { views_between: [40, 200] } })).toBe(3);
    expect(await total({ filters: { views_between: [null, 60] } })).toBe(2);
  });

  it('applies in filters and global search across declared fields', async () => {
    const t = transport();
    expect((await t.list('faqs', query({ filters: { id_in: [1, 5] } }))).total).toBe(2);
    expect((await t.list('faqs', query({ search: 'zoneless' }))).total).toBe(1);
  });

  it('sorts numerically and by text, asc and desc', async () => {
    const t = transport();
    const asc = await t.list('faqs', query({ sort: { field: 'views', direction: 'asc' } }));
    expect(asc.rows.map((r) => r['views'])).toEqual([25, 50, 100, 175, 300]);
    const desc = await t.list('faqs', query({ sort: { field: 'question', direction: 'desc' } }));
    expect(desc.rows[0]!['question']).toBe('Zoneless meaning?');
  });

  it('creates with the next id and updates by primary key', async () => {
    const t = transport();
    const created = await t.create('faqs', { question: 'new', views: 1, published: false });
    expect(created['id']).toBe(6);
    expect((await t.list('faqs', query())).total).toBe(6);

    const updated = await t.update('faqs', 6, { views: 42 });
    expect(updated['views']).toBe(42);
    expect((updated['question'] as string)).toBe('new');
    await expect(t.update('faqs', 999, { views: 1 })).rejects.toThrow(/not found/);
  });

  it('gets and removes by id', async () => {
    const t = transport();
    expect((await t.get('faqs', 3))['question']).toBe('What about http?');
    await expect(t.get('faqs', 99)).rejects.toThrow(/not found/);
    await t.remove('faqs', 3);
    expect((await t.list('faqs', query())).total).toBe(4);
  });

  it('does not leak internal state through responses', async () => {
    const t = transport();
    const first = await t.get('faqs', 1);
    first['question'] = 'MUTATED';
    expect((await t.get('faqs', 1))['question']).toBe('How do signals work?');
  });

  it('paginates past the end with an empty page but the real total', async () => {
    const result = await transport().list('faqs', query({ page: 99, pageSize: 10 }));
    expect(result.rows).toEqual([]);
    expect(result.total).toBe(5);
  });
});
