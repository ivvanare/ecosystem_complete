import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { defineCrudConfig, type ResolvedField } from 'crud-maker';
import { CrudCell } from './crud-cell';

function resolveField(cell?: Record<string, unknown>): ResolvedField {
  const config = defineCrudConfig({
    key: 'faqs',
    endpoint: 'faqs',
    fields: [{ name: 'status', ...(cell ? { cell } : {}) }],
  });
  const field = config.fieldMap.get('status');
  if (!field) throw new Error('field "status" was not resolved');
  return field;
}

async function render(field: ResolvedField, value: unknown) {
  const fixture = TestBed.createComponent(CrudCell);
  fixture.componentRef.setInput('field', field);
  fixture.componentRef.setInput('value', value);
  await fixture.whenStable();
  return fixture.nativeElement as HTMLElement;
}

describe('CrudCell', () => {
  it('renders a plain value as text, with no zorro primitive', async () => {
    const host = await render(resolveField(), 'draft');
    expect(host.textContent?.trim()).toBe('draft');
    expect(host.querySelector('nz-tag')).toBeNull();
    expect(host.querySelector('nz-badge')).toBeNull();
  });

  it('renders a tag cell with the variant as its color', async () => {
    const host = await render(resolveField({ render: 'tag', variantMap: { published: 'green' } }), 'published');
    expect(host.querySelector('nz-tag')?.textContent?.trim()).toBe('published');
    expect(host.querySelector('.ant-tag-green')).not.toBeNull();
  });

  it('renders a badge cell whose variant is a valid zorro status', async () => {
    const host = await render(resolveField({ render: 'badge', variantMap: { published: 'success' } }), 'published');
    expect(host.querySelector('nz-badge')?.textContent?.trim()).toBe('published');
    expect(host.querySelector('.ant-badge-status-success')).not.toBeNull();
  });

  it('degrades an unknown variant to the default badge status', async () => {
    const host = await render(resolveField({ render: 'badge', variantMap: { published: 'green' } }), 'published');
    expect(host.querySelector('.ant-badge-status-default')).not.toBeNull();
  });

  it('renders a dot cell without repeating the text', async () => {
    const host = await render(resolveField({ render: 'dot', variantMap: { published: 'error' } }), 'published');
    expect(host.querySelector('.ant-badge-status-error')).not.toBeNull();
    expect(host.textContent?.trim()).toBe('');
  });

  it('renders an absent value as empty text', async () => {
    const host = await render(resolveField({ render: 'tag', variantMap: { published: 'green' } }), null);
    expect(host.querySelector('nz-tag')?.textContent?.trim()).toBe('');
  });
});
