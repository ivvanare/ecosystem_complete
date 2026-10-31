import { beforeEach, describe, expect, it } from 'vitest';
import type { ResolvedField } from '../config/crud-config.types';
import { defineCrudConfig } from '../config/define-config';
import { registerFormatter, resetRegistry } from '../config/registry';
import { resolveCellView } from './cell-view';

function resolveField(extra: Record<string, unknown> = {}): ResolvedField {
  const config = defineCrudConfig({
    key: 'faqs',
    endpoint: 'faqs',
    fields: [{ name: 'status', ...extra }],
  });
  const field = config.fieldMap.get('status');
  if (!field) throw new Error('field "status" was not resolved');
  return field;
}

describe('resolveCellView', () => {
  beforeEach(() => {
    resetRegistry();
  });

  it('renders a plain value as text with no variant', () => {
    expect(resolveCellView(resolveField(), 'draft')).toEqual({
      kind: 'text',
      text: 'draft',
      variant: null,
    });
  });

  it('applies the formatter named by the config', () => {
    registerFormatter('shout', (value) => `${String(value)}!`);
    expect(resolveCellView(resolveField({ formatter: 'shout' }), 'done').text).toBe('done!');
  });

  it('renders null and undefined as empty text without touching the variant map', () => {
    const field = resolveField({ cell: { render: 'badge', variantMap: { null: 'ghost' } } });
    expect(resolveCellView(field, null)).toEqual({ kind: 'badge', text: '', variant: null });
    expect(resolveCellView(field, undefined)).toEqual({ kind: 'badge', text: '', variant: null });
  });

  it('maps the raw value through variantMap and keeps the declared render kind', () => {
    const field = resolveField({ cell: { render: 'badge', variantMap: { published: 'success' } } });
    expect(resolveCellView(field, 'published')).toEqual({
      kind: 'badge',
      text: 'published',
      variant: 'success',
    });
  });

  it('returns a null variant for a value the map does not declare', () => {
    const field = resolveField({ cell: { render: 'tag', variantMap: { published: 'success' } } });
    expect(resolveCellView(field, 'archived').variant).toBeNull();
  });

  it('keys the variant map by the stringified value', () => {
    const field = resolveField({ cell: { render: 'dot', variantMap: { 3: 'info' } } });
    expect(resolveCellView(field, 3)).toEqual({ kind: 'dot', text: '3', variant: 'info' });
  });

  it('never resolves inherited object properties as variants', () => {
    const field = resolveField({ cell: { render: 'badge', variantMap: { published: 'success' } } });
    expect(resolveCellView(field, 'toString').variant).toBeNull();
    expect(resolveCellView(field, '__proto__').variant).toBeNull();
  });
});
