import { describe, expect, it } from 'vitest';
import { defineCrudConfig } from '../config/define-config';
import { CrudListState, normalizeListResult } from './crud-list-state';

const config = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  primaryKey: 'id',
  fields: [
    { name: 'id', sortable: false },
    { name: 'question', filters: ['like', 'eq'] },
    { name: 'views', type: 'number', filters: ['gte', 'lte'] },
    { name: 'createdAt', type: 'datetime', defaultSort: 'desc' },
    { name: 'tags', type: 'select', options: [{ label: 'a', value: 'a' }], filters: ['in', 'eq'] },
    { name: 'archived', type: 'boolean', filters: ['eq'] },
    { name: 'rating', type: 'number', filters: ['between'] },
  ],
  pageSize: 10,
  searchFields: ['question'],
});

describe('CrudListState: defaults', () => {
  it('seeds the query from the resolved config', () => {
    const state = new CrudListState(config);
    expect(state.query()).toEqual({
      page: 1,
      pageSize: 10,
      sort: { field: 'createdAt', direction: 'desc' },
      filters: {},
      search: null,
    });
  });

  it('falls back to null sort when the config has no defaultSort', () => {
    const plain = defineCrudConfig({ key: 'x', endpoint: 'x', fields: [{ name: 'a' }] });
    expect(new CrudListState(plain).query().sort).toBeNull();
  });
});

describe('CrudListState: pagination', () => {
  it('accepts integer pages >= 1 and rejects everything else (no NaN leaks)', () => {
    const state = new CrudListState(config);
    state.setPage(3);
    expect(state.query().page).toBe(3);
    expect(() => state.setPage(0)).toThrow();
    expect(() => state.setPage(-2)).toThrow();
    expect(() => state.setPage(1.5)).toThrow();
    expect(() => state.setPage(Number.NaN)).toThrow();
  });

  it('setPageSize keeps bounds and resets to page 1', () => {
    const state = new CrudListState(config);
    state.setPage(4);
    state.setPageSize(50);
    expect(state.query()).toMatchObject({ page: 1, pageSize: 50 });
    expect(() => state.setPageSize(0)).toThrow();
    expect(() => state.setPageSize(1001)).toThrow();
  });
});

describe('CrudListState: sorting', () => {
  it('cycles asc → desc → none on the same field and resets the page', () => {
    const state = new CrudListState(config);
    state.setPage(5);
    state.setSort('views');
    expect(state.query().sort).toEqual({ field: 'views', direction: 'asc' });
    expect(state.query().page).toBe(1);
    state.setSort('views');
    expect(state.query().sort).toEqual({ field: 'views', direction: 'desc' });
    state.setSort('views');
    expect(state.query().sort).toBeNull();
  });

  it('switching fields restarts at asc', () => {
    const state = new CrudListState(config);
    state.setSort('views', 'desc');
    state.setSort('question');
    expect(state.query().sort).toEqual({ field: 'question', direction: 'asc' });
  });

  it('rejects unsortable and unknown fields', () => {
    const state = new CrudListState(config);
    expect(() => state.setSort('id')).toThrow(/not sortable/);
    expect(() => state.setSort('ghost')).toThrow(/unknown field/);
  });
});

describe('CrudListState: filters', () => {
  it('keys filters as `${name}_${kind}` and trims strings', () => {
    const state = new CrudListState(config);
    state.setFilter('question', 'like', '  hello  ');
    expect(state.query().filters).toEqual({ 'question_like': 'hello' });
    expect(state.getFilter('question', 'like')).toBe('hello');
  });

  it('drops empty-ish values so they never reach the query', () => {
    const state = new CrudListState(config);
    state.setFilter('question', 'like', 'x');
    state.setFilter('question', 'like', '   ');
    expect(state.query().filters).toEqual({});
    state.setFilter('tags', 'in', []);
    expect(state.query().filters).toEqual({});
  });

  it('coerces numeric filters and rejects NaN', () => {
    const state = new CrudListState(config);
    state.setFilter('views', 'gte', '5');
    expect(state.query().filters).toEqual({ 'views_gte': 5 });
    expect(() => state.setFilter('views', 'gte', 'abc')).toThrow(/numeric/);
  });

  it('between requires a two-element range of numbers (null = open bound)', () => {
    const state = new CrudListState(config);
    state.setFilter('rating', 'between', ['5', 10]);
    expect(state.query().filters).toEqual({ 'rating_between': [5, 10] });
    state.setFilter('rating', 'between', ['5', null]);
    expect(state.query().filters).toEqual({ 'rating_between': [5, null] });
    expect(() => state.setFilter('rating', 'between', 5)).toThrow(/between/);
    expect(() => state.setFilter('rating', 'between', [1])).toThrow(/between/);
    state.setFilter('rating', 'between', [null, null]);
    expect(state.query().filters).toEqual({});
  });

  it('in requires a non-empty array', () => {
    const state = new CrudListState(config);
    state.setFilter('tags', 'in', ['a', 'b']);
    expect(state.query().filters).toEqual({ 'tags_in': ['a', 'b'] });
    expect(() => state.setFilter('tags', 'in', 'a')).toThrow(/in/);
  });

  it('rejects kinds the field does not expose', () => {
    const state = new CrudListState(config);
    expect(() => state.setFilter('createdAt', 'eq', 'x')).toThrow(/not exposed/);
    expect(() => state.setFilter('question', 'between', ['a', 'b'])).toThrow(/not exposed/);
  });

  it('clearFilters removes everything and resets the page', () => {
    const state = new CrudListState(config);
    state.setFilter('question', 'like', 'a');
    state.setFilter('tags', 'eq', 'x');
    state.setPage(2);
    state.clearFilters();
    expect(state.query().filters).toEqual({});
    expect(state.query().page).toBe(1);
  });

  it('multiple kinds on the same field coexist as separate keys', () => {
    const state = new CrudListState(config);
    state.setFilter('views', 'gte', 1);
    state.setFilter('views', 'lte', 10);
    expect(state.query().filters).toEqual({ 'views_gte': 1, 'views_lte': 10 });
  });

  it('boolean filters coerce "true"/"false" strings', () => {
    const state = new CrudListState(config);
    state.setFilter('archived', 'eq', 'true');
    expect(state.query().filters).toEqual({ 'archived_eq': true });
  });
});

describe('CrudListState: search and reset', () => {
  it('trims search and maps empty to null', () => {
    const state = new CrudListState(config);
    state.setSearch('  term ');
    expect(state.query().search).toBe('term');
    state.setSearch('  ');
    expect(state.query().search).toBeNull();
  });

  it('reset restores every default', () => {
    const state = new CrudListState(config);
    state.setPage(7);
    state.setSort('views', 'desc');
    state.setFilter('question', 'like', 'q');
    state.setSearch('s');
    state.reset();
    expect(state.query()).toEqual({
      page: 1,
      pageSize: 10,
      sort: { field: 'createdAt', direction: 'desc' },
      filters: {},
      search: null,
    });
  });
});

describe('normalizeListResult', () => {
  it('accepts bare arrays', () => {
    expect(normalizeListResult([{ a: 1 }, { a: 2 }])).toEqual({ rows: [{ a: 1 }, { a: 2 }], total: 2 });
  });

  it('accepts {items,total}, {data,meta.total}, {results,count}', () => {
    expect(normalizeListResult({ items: [{ a: 1 }], total: 9 })).toEqual({ rows: [{ a: 1 }], total: 9 });
    expect(normalizeListResult({ data: [{ b: 1 }], meta: { total: 3 } })).toEqual({ rows: [{ b: 1 }], total: 3 });
    expect(normalizeListResult({ results: [{ c: 1 }], count: 1 })).toEqual({ rows: [{ c: 1 }], total: 1 });
  });

  it('falls back to row count when the envelope has no total', () => {
    expect(normalizeListResult({ data: [1, 2, 3] })).toEqual({ rows: [1, 2, 3], total: 3 });
  });

  it('rejects payloads that carry no rows array', () => {
    expect(() => normalizeListResult(null)).toThrow(/unrecognized list payload/);
    expect(() => normalizeListResult({ nope: true })).toThrow(/unrecognized list payload/);
    expect(() => normalizeListResult('text')).toThrow(/unrecognized list payload/);
  });
});
