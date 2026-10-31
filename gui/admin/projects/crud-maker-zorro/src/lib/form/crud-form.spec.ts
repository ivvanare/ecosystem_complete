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
import { CrudForm } from './crud-form';

const config: ResolvedCrudConfig = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  fields: [
    { name: 'id', type: 'number', in: { form: false, list: true } },
    {
      name: 'question',
      required: true,
      validators: [{ name: 'minLength', params: 5 }],
      in: { form: true, list: true },
    },
    { name: 'notes', type: 'textarea', in: { form: true, list: false } },
    { name: 'published', type: 'boolean', in: { form: true, list: true } },
    {
      name: 'category',
      type: 'select',
      options: [
        { label: 'General', value: 'general' },
        { label: 'Billing', value: 'billing' },
      ],
      in: { form: true, list: true },
    },
    { name: 'secret', type: 'hidden', in: { form: true, list: false } },
  ],
});

class FakeTransport implements CrudTransport {
  readonly created: Record<string, unknown>[] = [];
  readonly updated: { id: EntityId; payload: Record<string, unknown> }[] = [];

  list(_endpoint: string, _query: ListQuery): Promise<ListResult> {
    return Promise.resolve({ rows: [], total: 0 });
  }

  get(): Promise<Record<string, unknown>> {
    return Promise.resolve({});
  }

  create(_endpoint: string, payload: Record<string, unknown>): Promise<Record<string, unknown>> {
    this.created.push(payload);
    return Promise.resolve({ ...payload, id: 7 });
  }

  update(
    _endpoint: string,
    id: EntityId,
    payload: Record<string, unknown>,
  ): Promise<Record<string, unknown>> {
    this.updated.push({ id, payload });
    return Promise.resolve({ ...payload, id });
  }

  remove(): Promise<void> {
    return Promise.resolve();
  }
}

async function setup(seed: Record<string, unknown> | null = null) {
  const transport = new FakeTransport();
  const controller = new CrudController(config, transport);
  const fixture = TestBed.createComponent(CrudForm);
  fixture.componentRef.setInput('config', config);
  fixture.componentRef.setInput('controller', controller);
  fixture.componentRef.setInput('value', seed);
  await fixture.whenStable();
  return { transport, controller, fixture, host: fixture.nativeElement as HTMLElement };
}

function type(host: HTMLElement, selector: string, value: string): void {
  const input = host.querySelector(selector) as HTMLInputElement | HTMLTextAreaElement | null;
  if (!input) throw new Error(`control "${selector}" was not rendered`);
  input.value = value;
  input.dispatchEvent(new Event('input'));
}

describe('CrudForm', () => {
  it('renders a control per form field, skipping list-only and hidden fields', async () => {
    const { host } = await setup();
    const labels = [...host.querySelectorAll('.crud-form-label')].map((el) => el.textContent?.trim());
    expect(labels).toEqual(['question', 'notes', 'published', 'category']);
    expect(host.querySelector('textarea')).not.toBeNull();
    expect(host.querySelector('input[type="checkbox"]')).not.toBeNull();
    expect(host.querySelector('nz-select')).not.toBeNull();
  });

  it('prefills from the seed row and labels the action as an update', async () => {
    const { host } = await setup({ id: 3, question: 'How do signals work?', published: true });
    const firstInput = host.querySelector('input[type="text"]') as HTMLInputElement;
    expect(firstInput.value).toBe('How do signals work?');
    expect((host.querySelector('input[type="checkbox"]') as HTMLInputElement).checked).toBe(true);
    expect(host.textContent).toContain('Save changes');
  });

  it('refuses to save an empty required field and explains why', async () => {
    const { host, transport, fixture } = await setup();
    const form = host.querySelector('form') as HTMLFormElement;
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(transport.created).toHaveLength(0);
    expect(host.textContent).toContain('question is required');
  });

  it('applies a registry validator through Signal Forms', async () => {
    const { host, fixture } = await setup();
    type(host, 'input[type="text"]', 'abc');
    await fixture.whenStable();
    const form = host.querySelector('form') as HTMLFormElement;
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();
    expect(host.textContent).toContain('must be at least 5 characters');
  });

  it('saves a valid create through the controller and emits the stored row', async () => {
    const { host, transport, fixture } = await setup();
    const saved: unknown[] = [];
    fixture.componentInstance.saved.subscribe((row) => saved.push(row));
    type(host, 'input[type="text"]', 'How do signals work?');
    const form = host.querySelector('form') as HTMLFormElement;
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(transport.created).toHaveLength(1);
    expect(transport.created[0]).toMatchObject({ question: 'How do signals work?', published: false });
    expect(saved).toHaveLength(1);
  });

  it('sends the primary key when editing', async () => {
    const { host, transport, fixture } = await setup({ id: 3, question: 'Existing question' });
    const form = host.querySelector('form') as HTMLFormElement;
    form.dispatchEvent(new Event('submit'));
    await fixture.whenStable();

    expect(transport.updated).toHaveLength(1);
    expect(transport.updated[0]).toMatchObject({ id: 3, payload: { id: 3 } });
  });

  it('maps declared select options into zorro options', async () => {
    const { fixture } = await setup();
    const category = config.fieldMap.get('category');
    if (!category) throw new Error('field "category" was not resolved');
    expect(fixture.componentInstance.optionsFor(category)).toEqual([
      { label: 'General', value: 'general' },
      { label: 'Billing', value: 'billing' },
    ]);
  });
});
