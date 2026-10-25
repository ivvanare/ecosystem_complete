import { beforeEach, describe, expect, it } from 'vitest';
import { registerFormatter, registerValidator, resetRegistry } from './registry';
import { CrudConfigError, defineCrudConfig } from './define-config';

function validBase(): Record<string, unknown> {
  return {
    key: 'faqs',
    endpoint: 'faqs',
    fields: [
      { name: 'question', label: 'Question', filters: ['like'], required: true },
      { name: 'createdAt', type: 'datetime', formatter: 'date', defaultSort: 'desc' },
    ],
  };
}

function expectIssues(config: unknown, codes: string[]): CrudConfigError {
  let error: CrudConfigError | undefined;
  try {
    defineCrudConfig(config);
  } catch (e) {
    error = e as CrudConfigError;
  }
  expect(error, 'defineCrudConfig should have thrown').toBeInstanceOf(CrudConfigError);
  const found = error!.issues.map((i) => i.code);
  for (const code of codes) expect(found).toContain(code);
  return error!;
}

describe('defineCrudConfig: normalization', () => {
  beforeEach(() => resetRegistry());

  it('applies documented defaults to a minimal valid config', () => {
    const resolved = defineCrudConfig(validBase());
    expect(resolved.primaryKey).toBe('id');
    expect(resolved.pageSize).toBe(20);
    expect(resolved.titleField).toBeNull();
    expect(resolved.defaultSort).toEqual({ field: 'createdAt', direction: 'desc' });
    expect(resolved.actions).toEqual({ create: true, read: true, update: true, delete: true });

    const question = resolved.fieldMap.get('question')!;
    expect(question.type).toBe('text');
    expect(question.label).toBe('Question');
    expect(question.in).toEqual({ list: true, form: true, detail: true });
    expect(question.sortable).toBe(true);
    expect(question.validators).toEqual([{ name: 'required' }]);
    expect(question.formatter).toBeNull();

    const createdAt = resolved.fieldMap.get('createdAt')!;
    expect(createdAt.formatter).toEqual({ name: 'date' });
  });

  it('normalizes terse string specs to objects and endpoint slashes', () => {
    const resolved = defineCrudConfig({
      key: 'faqs',
      endpoint: '/faqs/',
      fields: [{ name: 'id', validators: ['required', { name: 'min', params: 1 }] }],
    });
    expect(resolved.endpoint).toBe('faqs');
    expect(resolved.fieldMap.get('id')!.validators).toEqual([
      { name: 'required' },
      { name: 'min', params: 1 },
    ]);
  });

  it('derives per-view field lists and the field map from the flags', () => {
    const resolved = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [
        { name: 'a' },
        { name: 'b', type: 'textarea' },
        { name: 'c', type: 'password' },
        { name: 'd', in: { list: false, form: false, detail: false } },
      ],
    });
    expect(resolved.listFields.map((f) => f.name)).toEqual(['a']);
    expect(resolved.formFields.map((f) => f.name)).toEqual(['a', 'b', 'c']);
    expect(resolved.detailFields.map((f) => f.name)).toEqual(['a', 'b']);
    expect(resolved.fieldMap.size).toBe(4);
  });

  it('hidden fields default to form-only presence', () => {
    const resolved = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [{ name: 'internalId', type: 'hidden' }],
    });
    expect(resolved.fieldMap.get('internalId')!.in).toEqual({
      list: false,
      form: true,
      detail: false,
    });
  });

  it('explicit in-flags override type defaults', () => {
    const resolved = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [{ name: 'note', type: 'textarea', in: { list: true, detail: false } }],
    });
    expect(resolved.fieldMap.get('note')!.in).toEqual({ list: true, form: true, detail: false });
  });

  it('required sugar does not duplicate an explicit required validator', () => {
    const resolved = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [{ name: 'a', required: true, validators: ['required'] }],
    });
    expect(resolved.fieldMap.get('a')!.validators).toEqual([{ name: 'required' }]);
  });

  it('keeps extra unknown properties out of the resolved object', () => {
    const resolved = defineCrudConfig({
      key: 'x',
      endpoint: 'x',
      fields: [{ name: 'a', someday: 'maybe' }],
    });
    expect('someday' in resolved.fieldMap.get('a')!).toBe(false);
  });
});

describe('defineCrudConfig: validation', () => {
  beforeEach(() => resetRegistry());

  it('accepts the minimal valid config', () => {
    expect(() => defineCrudConfig(validBase())).not.toThrow();
  });

  it('rejects non-object input and missing key/endpoint/fields', () => {
    expectIssues(null, ['notAnObject']);
    expectIssues({ endpoint: 'x', fields: [] }, ['required']);
    expectIssues({ key: 'x', fields: [] }, ['required']);
    expectIssues({ key: 'x', endpoint: 'x', fields: [] }, ['fieldsEmpty']);
  });

  it('rejects missing/duplicate/invalid field names', () => {
    expectIssues({ key: 'x', endpoint: 'x', fields: [{}] }, ['required']);
    expectIssues(
      { key: 'x', endpoint: 'x', fields: [{ name: 'a' }, { name: 'a' }] },
      ['duplicateField'],
    );
  });

  it('collects every issue in one pass instead of failing on the first', () => {
    const error = expectIssues(
      { key: '', endpoint: 'x', fields: [{ name: 'a', type: 'rune' }, { name: 'a' }] },
      ['required', 'invalidFieldType', 'duplicateField'],
    );
    expect(error.issues).toHaveLength(3);
  });

  it('cross-validates references against field names', () => {
    expectIssues(
      {
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 'a' }],
        primaryKey: 'id',
        titleField: 'missing',
        searchFields: ['nope'],
      },
      ['unknownField'],
    );
    // valid references pass
    expect(() =>
      defineCrudConfig({
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 'id' }, { name: 'a' }],
        primaryKey: 'id',
        titleField: 'a',
        searchFields: ['a'],
      }),
    ).not.toThrow();
  });

  it('requires options on select fields and rejects malformed option shapes', () => {
    expectIssues({ key: 'x', endpoint: 'x', fields: [{ name: 's', type: 'select' }] }, [
      'optionsRequired',
    ]);
    expectIssues(
      {
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 's', type: 'select', options: [{ label: 'a' }] }],
      },
      ['invalidOptions'],
    );
    expect(() =>
      defineCrudConfig({
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 's', type: 'select', options: { resource: 'cats', labelField: 'name' } }],
      }),
    ).not.toThrow();
  });

  it('rejects defaultSort on non-sortable fields and more than one defaultSort', () => {
    expectIssues(
      { key: 'x', endpoint: 'x', fields: [{ name: 'a', type: 'textarea', defaultSort: 'asc' }] },
      ['sortOnUnsortableField'],
    );
    expectIssues(
      {
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 'a', defaultSort: 'asc' }, { name: 'b', defaultSort: 'desc' }],
      },
      ['multipleDefaultSort'],
    );
  });

  it('rejects invalid enums and page sizes', () => {
    expectIssues({ key: 'x', endpoint: 'x', fields: [{ name: 'a', filters: ['soundslike'] }] }, [
      'invalidFilterKind',
    ]);
    expectIssues({ key: 'x', endpoint: 'x', fields: [{ name: 'a', width: 'huge' }] }, [
      'invalidWidth',
    ]);
    expectIssues({ key: 'x', endpoint: 'x', fields: [{ name: 'a' }], pageSize: 0 }, [
      'invalidPageSize',
    ]);
    expectIssues({ key: 'x', endpoint: 'x', fields: [{ name: 'a' }], pageSize: 5000 }, [
      'invalidPageSize',
    ]);
  });

  it('rejects empty endpoint but accepts one with surrounding slashes', () => {
    expectIssues({ key: 'x', endpoint: '  /', fields: [{ name: 'a' }] }, ['required']);
    expect(() =>
      defineCrudConfig({ key: 'x', endpoint: '/x/', fields: [{ name: 'a' }] }),
    ).not.toThrow();
  });

  it('resolves formatter and validator names against the registry at define time', () => {
    expectIssues(
      { key: 'x', endpoint: 'x', fields: [{ name: 'a', formatter: 'nope.fmt' }] },
      ['unknownFormatter'],
    );
    expectIssues(
      { key: 'x', endpoint: 'x', fields: [{ name: 'a', validators: ['nope.val'] }] },
      ['unknownValidator'],
    );
    registerFormatter('custom.fmt', (v) => String(v));
    registerValidator('custom.val', () => () => null);
    expect(() =>
      defineCrudConfig({
        key: 'x',
        endpoint: 'x',
        fields: [{ name: 'a', formatter: 'custom.fmt', validators: ['custom.val'] }],
      }),
    ).not.toThrow();
  });
});
