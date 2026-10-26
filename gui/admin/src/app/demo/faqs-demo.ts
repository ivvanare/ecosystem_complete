import { Component, computed, signal } from '@angular/core';
import { form, FormField, min, minLength, required } from '@angular/forms/signals';
import {
  CrudController,
  defineCrudConfig,
  formatValue,
  type ResolvedCrudConfig,
  type ResolvedField,
} from 'crud-maker';
import { FAQS_CONFIG, FAQS_SEED } from './faqs.config';
import { MemoryTransport } from './memory-transport';

interface FaqForm {
  question: string;
  answer: string;
  category: string;
  views: number;
  published: boolean;
}

const EMPTY_FORM: FaqForm = {
  question: '',
  answer: '',
  category: 'general',
  views: 0,
  published: false,
};

const CATEGORIES = [
  { label: 'General', value: 'general' },
  { label: 'Reactivity', value: 'reactivity' },
  { label: 'Forms', value: 'forms' },
  { label: 'HTTP', value: 'http' },
];

/**
 * Demo vertical proving the crud-maker base logic end-to-end without a
 * backend or any UI-library dependency: JSON config → validated schema →
 * signal state → in-memory transport → plain HTML screen.
 */
@Component({
  selector: 'app-faqs-demo',
  imports: [FormField],
  templateUrl: './faqs-demo.html',
  styleUrl: './faqs-demo.css',
})
export class FaqsDemo {
  protected readonly config: ResolvedCrudConfig = defineCrudConfig(FAQS_CONFIG);
  protected readonly crud = new CrudController(
    this.config,
    new MemoryTransport(FAQS_SEED, { searchFields: ['question'] }),
  );
  protected readonly listFields: readonly ResolvedField[] = this.config.listFields;
  protected readonly categories = CATEGORIES;

  protected readonly model = signal<FaqForm>({ ...EMPTY_FORM });
  protected readonly profile = form(this.model, (schema) => {
    required(schema.question);
    minLength(schema.question, 5);
    required(schema.answer);
    required(schema.category);
    min(schema.views, 0);
  });

  protected readonly editingId = signal<number | null>(null);
  protected readonly showForm = signal(false);
  protected readonly questionFilter = signal('');
  protected readonly viewsFrom = signal('');

  protected readonly totalPages = computed(() =>
    Math.max(1, Math.ceil(this.crud.total() / this.crud.state.pageSize())),
  );

  constructor() {
    void this.crud.load();
  }

  protected display(field: ResolvedField, row: Record<string, unknown>): string {
    return formatValue(field.formatter, row[field.name]);
  }

  protected sortGlyph(field: string): string {
    const sort = this.crud.state.sort();
    if (!sort || sort.field !== field) return '';
    return sort.direction === 'asc' ? ' ▲' : ' ▼';
  }

  protected toggleSort(field: string): void {
    this.crud.state.setSort(field);
    void this.crud.load();
  }

  protected onQuestionFilter(event: Event): void {
    this.questionFilter.set((event.target as HTMLInputElement).value);
  }

  protected onViewsFilter(event: Event): void {
    this.viewsFrom.set((event.target as HTMLInputElement).value);
  }

  protected applyFilters(): void {
    this.crud.state.setFilter('question', 'like', this.questionFilter());
    this.crud.state.setFilter('views', 'gte', this.viewsFrom() === '' ? null : this.viewsFrom());
    void this.crud.load();
  }

  protected clearFilters(): void {
    this.questionFilter.set('');
    this.viewsFrom.set('');
    this.crud.state.clearFilters();
    void this.crud.load();
  }

  protected goToPage(page: number): void {
    if (page < 1 || page > this.totalPages()) return;
    this.crud.state.setPage(page);
    void this.crud.load();
  }

  protected openCreate(): void {
    this.editingId.set(null);
    this.model.set({ ...EMPTY_FORM });
    this.showForm.set(true);
  }

  protected openEdit(row: Record<string, unknown>): void {
    this.editingId.set(Number(row[this.config.primaryKey]));
    this.model.set({
      question: String(row['question'] ?? ''),
      answer: String(row['answer'] ?? ''),
      category: String(row['category'] ?? 'general'),
      views: Number(row['views'] ?? 0),
      published: Boolean(row['published']),
    });
    this.showForm.set(true);
  }

  protected closeForm(): void {
    this.showForm.set(false);
  }

  protected save(event: Event): void {
    event.preventDefault();
    if (!this.profile().valid()) return;
    const values: Record<string, unknown> = { ...this.model() };
    const id = this.editingId();
    if (id !== null) values[this.config.primaryKey] = id;
    this.crud
      .save(values)
      .then(() => this.showForm.set(false))
      .catch(() => undefined);
  }

  protected remove(row: Record<string, unknown>): void {
    void this.crud.removeOne(Number(row[this.config.primaryKey]));
  }
}
