import { TestBed } from '@angular/core/testing';
import { describe, expect, it } from 'vitest';
import {
  CrudController,
  defineCrudConfig,
  type CrudTransport,
  type EntityId,
  type ListQuery,
  type ListResult,
  type ResolvedCrudConfig,
} from 'crud-maker';
import { CrudTable } from './crud-table';

const config: ResolvedCrudConfig = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  fields: [
    { name: 'id', type: 'number', in: { list: true, form: false, detail: true } },
    { name: 'question', in: { list: true, form: true, detail: true } },
    {
      name: 'status',
      sortable: false,
      in: { list: true, form: true },
      cell: { render: 'tag', variantMap: { published: 'green' } },
    },
    { name: 'notes', in: { list: false, form: true } },
  ],
});

const rows = [
  { id: 1, question: 'How do signals work?', status: 'published', notes: 'internal' },
  { id: 2, question: 'What about forms?', status: 'draft', notes: 'internal' },
];

class FakeTransport implements CrudTransport {
  readonly listCalls: ListQuery[] = [];

  list(_endpoint: string, query: ListQuery): Promise<ListResult> {
    this.listCalls.push(query);
    return Promise.resolve({ rows, total: 42 });
  }

  get(_endpoint: string, _id: EntityId): Promise<Record<string, unknown>> {
    return Promise.resolve({});
  }

  create(_endpoint: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    return Promise.resolve(payload);
  }

  update(
    _endpoint: string,
    _id: EntityId,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    return Promise.resolve(payload);
  }

  remove(): Promise<void> {
    return Promise.resolve();
  }
}

async function setup() {
  const transport = new FakeTransport();
  const controller = new CrudController(config, transport);
  await controller.load();

  const fixture = TestBed.createComponent(CrudTable);
  fixture.componentRef.setInput('config', config);
  fixture.componentRef.setInput('controller', controller);
  await fixture.whenStable();

  return { transport, controller, fixture, host: fixture.nativeElement as HTMLElement };
}

describe('CrudTable', () => {
  it('renders one row per controller row and a header per list field', async () => {
    const { host } = await setup();
    const headers = [...host.querySelectorAll('th')].map((th) => th.textContent?.trim());
    expect(headers).toEqual(['id', 'question', 'status', 'Actions']);
    expect(host.textContent).toContain('How do signals work?');
    expect(host.textContent).toContain('What about forms?');
    expect(host.textContent).not.toContain('internal');
  });

  it('renders cell hints through crud-cell', async () => {
    const { host } = await setup();
    expect(host.querySelectorAll('nz-tag')).toHaveLength(2);
    expect(host.querySelector('.ant-tag-green')).not.toBeNull();
  });

  it('shows the transport total, not the page length', async () => {
    const { host } = await setup();
    expect(host.querySelector('.ant-pagination')?.textContent).toContain('42');
  });

  it('applies a zorro sort order to the list state and refetches', async () => {
    const { controller, transport, fixture } = await setup();
    const table = fixture.componentInstance;
    const field = config.fieldMap.get('question');
    if (!field) throw new Error('field "question" was not resolved');

    table.onSort(field, 'ascend');
    await fixture.whenStable();
    expect(controller.state.sort()).toEqual({ field: 'question', direction: 'asc' });
    expect(transport.listCalls.at(-1)?.sort).toEqual({ field: 'question', direction: 'asc' });

    table.onSort(field, 'descend');
    await fixture.whenStable();
    expect(controller.state.sort()).toEqual({ field: 'question', direction: 'desc' });

    table.onSort(field, null);
    await fixture.whenStable();
    expect(controller.state.sort()).toBeNull();
  });

  it('sorts when the zorro header trigger is clicked', async () => {
    const { controller, transport, host, fixture } = await setup();
    const before = transport.listCalls.length;
    const headers = [...host.querySelectorAll('th')];
    const questionHeader = headers.find((th) => th.textContent?.trim() === 'question');
    if (!questionHeader) throw new Error('header "question" was not rendered');
    const trigger = questionHeader.querySelector('.ant-table-column-sorters') ?? questionHeader;
    (trigger as HTMLElement).click();
    await fixture.whenStable();

    expect(controller.state.sort()).toEqual({ field: 'question', direction: 'asc' });
    expect(transport.listCalls.length).toBeGreaterThan(before);
  });

  it('ignores sort requests for a field the config does not allow', async () => {
    const { controller, transport, fixture } = await setup();
    const field = config.fieldMap.get('status');
    if (!field) throw new Error('field "status" was not resolved');
    const before = transport.listCalls.length;

    fixture.componentInstance.onSort(field, 'ascend');
    await fixture.whenStable();
    expect(controller.state.sort()).toBeNull();
    expect(transport.listCalls).toHaveLength(before);
  });

  it('moves to the requested page and ignores an invalid one', async () => {
    const { controller, fixture } = await setup();
    const table = fixture.componentInstance;

    table.onPage(2);
    await fixture.whenStable();
    expect(controller.state.page()).toBe(2);

    table.onPage(0);
    await fixture.whenStable();
    expect(controller.state.page()).toBe(2);
  });

  it('emits the row for edit and remove, and hides actions when not permitted', async () => {
    const { fixture } = await setup();
    const table = fixture.componentInstance;
    const edited: unknown[] = [];
    const removed: unknown[] = [];
    table.edit.subscribe((row) => edited.push(row));
    table.remove.subscribe((row) => removed.push(row));

    const buttons = [...fixture.nativeElement.querySelectorAll('button')] as HTMLButtonElement[];
    buttons[0].click();
    buttons[1].click();
    await fixture.whenStable();

    expect(edited).toEqual([rows[0]]);
    expect(removed).toEqual([rows[0]]);

    const readOnly = defineCrudConfig({
      key: 'faqs',
      endpoint: 'faqs',
      fields: [{ name: 'id', type: 'number' }],
      actions: { update: false, delete: false },
    });
    const restricted = TestBed.createComponent(CrudTable);
    restricted.componentRef.setInput('config', readOnly);
    restricted.componentRef.setInput('controller', new CrudController(readOnly, new FakeTransport()));
    await restricted.whenStable();
    expect(restricted.componentInstance.hasActions()).toBe(false);
    expect(restricted.nativeElement.querySelectorAll('button')).toHaveLength(0);
  });
});
