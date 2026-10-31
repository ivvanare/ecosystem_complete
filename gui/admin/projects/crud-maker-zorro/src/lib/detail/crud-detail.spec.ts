import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import { defineCrudConfig, type ResolvedCrudConfig } from 'crud-maker';
import { CrudDetail } from './crud-detail';

const config: ResolvedCrudConfig = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  fields: [
    { name: 'id', type: 'number', in: { list: true, form: false, detail: true } },
    { name: 'question', in: { list: true, form: true, detail: true } },
    { name: 'notes', type: 'textarea', in: { list: false, form: true, detail: false } },
    {
      name: 'status',
      in: { list: true, form: true, detail: true },
      cell: { render: 'tag', variantMap: { published: 'green' } },
    },
    {
      name: 'createdAt',
      in: { list: true, form: false, detail: true },
      formatter: { name: 'date' },
    },
  ],
});

async function setup(row: Record<string, unknown> | null) {
  const fixture = TestBed.createComponent(CrudDetail);
  fixture.componentRef.setInput('config', config);
  fixture.componentRef.setInput('value', row);
  await fixture.whenStable();
  return { fixture, host: fixture.nativeElement as HTMLElement };
}

describe('CrudDetail', () => {
  it('renders one label per detail field, skipping fields the config keeps out', async () => {
    const { host } = await setup({ id: 1, question: 'Q', notes: 'internal', status: 'published' });
    const labels = [...host.querySelectorAll('dt')].map((el) => el.textContent?.trim());
    expect(labels).toEqual(['id', 'question', 'status', 'createdAt']);
    expect(host.textContent).not.toContain('internal');
  });

  it('renders values through crud-cell, so cell hints apply', async () => {
    const { host } = await setup({ id: 1, question: 'Q', status: 'published' });
    expect(host.querySelector('nz-tag')?.textContent?.trim()).toBe('published');
    expect(host.querySelector('.ant-tag-green')).not.toBeNull();
  });

  it('applies the configured formatter', async () => {
    const { host } = await setup({ id: 1, question: 'Q', createdAt: '2026-10-31T12:00:00Z' });
    expect(host.textContent).toContain('2026-10-31');
  });

  it('shows an empty state when there is no record', async () => {
    const { host } = await setup(null);
    expect(host.querySelectorAll('dt')).toHaveLength(0);
    expect(host.textContent).toContain('No record to show');
  });

  it('renders an absent value as empty text instead of the string "undefined"', async () => {
    const { host } = await setup({ id: 1, question: 'Q' });
    expect(host.textContent).not.toContain('undefined');
    expect(host.textContent).not.toContain('null');
  });
});
