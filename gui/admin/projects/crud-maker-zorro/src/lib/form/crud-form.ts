/**
 * `crud-form` — the create/edit screen of the ng-zorro adapter (T7.2).
 *
 * The config decides everything the form shows: `formFields` is the field list,
 * the registry's named validators become Signal Forms rules, and a `select`
 * whose options come from a referenced resource resolves through the same
 * `OptionResolver` the core uses. The adapter adds controls and zorro chrome;
 * it never invents a field, a rule or an option.
 *
 * The form does not own persistence either: `save()` goes through the
 * controller, which enforces the action matrix and refreshes the list.
 */
import {
  Component,
  computed,
  inject,
  Injector,
  input,
  linkedSignal,
  output,
  runInInjectionContext,
  signal,
  untracked,
  type Signal,
} from '@angular/core';
import { FormField, email, form, max, maxLength, min, minLength, pattern, required, validate, type Field, type FieldTree, type SchemaPath } from '@angular/forms/signals';
import {
  compileValidator,
  type BehaviorRef,
  type CrudController,
  type OptionResolver,
  type ResolvedCrudConfig,
  type ResolvedField,
} from 'crud-maker';
import { NzInputModule } from 'ng-zorro-antd/input';
import { NzSelectModule, type NzSelectOptionInterface } from 'ng-zorro-antd/select';

export type CrudFormValues = Record<string, unknown>;

function defaultValueFor(field: ResolvedField): unknown {
  if (field.type === 'boolean') return false;
  if (field.type === 'number') return null;
  return '';
}

function schemaPath(schema: unknown, name: string): SchemaPath<unknown> {
  return (schema as Record<string, SchemaPath<unknown>>)[name];
}

function applySchema(schema: unknown, config: ResolvedCrudConfig): void {
  for (const field of config.formFields) {
    const path = schemaPath(schema, field.name);
    if (field.required) required(path, { message: `${field.label} is required` });
    for (const ref of field.validators) applyValidator(path, ref, field);
  }
}

/**
 * Registry names map onto Signal Forms' own rules, so the framework produces
 * the error and its message. Anything a project registered itself falls back to
 * `validate()`, seeded with the compiled registry predicate.
 */
function applyValidator(path: SchemaPath<unknown>, ref: BehaviorRef, field: ResolvedField): void {
  const numeric = Number(ref.params);
  switch (ref.name) {
    case 'required':
      required(path, { message: `${field.label} is required` });
      return;
    case 'min':
      min(path as SchemaPath<number | null>, numeric, {
        message: `${field.label} must be at least ${numeric}`,
      });
      return;
    case 'max':
      max(path as SchemaPath<number | null>, numeric, {
        message: `${field.label} must be at most ${numeric}`,
      });
      return;
    case 'minLength':
      minLength(path as SchemaPath<string>, numeric, {
        message: `${field.label} must be at least ${numeric} characters`,
      });
      return;
    case 'maxLength':
      maxLength(path as SchemaPath<string>, numeric, {
        message: `${field.label} must be at most ${numeric} characters`,
      });
      return;
    case 'pattern':
      pattern(path as SchemaPath<string>, new RegExp(String(ref.params)), {
        message: `${field.label} has an invalid format`,
      });
      return;
    case 'email':
      email(path as SchemaPath<string>, { message: `${field.label} must be a valid email` });
      return;
    default: {
      const predicate = compileValidator(ref);
      validate(path, (ctx) => {
        const problem = predicate(ctx.value());
        return problem ? { kind: problem.code, message: `${field.label} is invalid` } : null;
      });
    }
  }
}

@Component({
  selector: 'crud-form',
  imports: [FormField, NzInputModule, NzSelectModule],
  template: `
    <form class="crud-form" (submit)="onSubmit($event)">
      @for (field of fields(); track field.name) {
        @if (field.type !== 'hidden') {
          <label class="crud-form-field">
            <span class="crud-form-label">{{ field.label }}</span>

            @switch (field.type) {
              @case ('boolean') {
                <input
                  type="checkbox"
                  [formField]="$any(bound(field))"
                  [readOnly]="field.readonly"
                />
              }
              @case ('textarea') {
                <textarea nz-input rows="4" [formField]="$any(bound(field))" [readOnly]="field.readonly"></textarea>
              }
              @case ('select') {
                <nz-select
                  [formField]="$any(bound(field))"
                  [nzOptions]="optionsFor(field)"
                  [nzDisabled]="field.readonly"
                />
              }
              @default {
                <input
                  nz-input
                  [type]="inputType(field)"
                  [formField]="$any(bound(field))"
                  [readOnly]="field.readonly"
                />
              }
            }

            @if (showError(field)) {
              <small class="crud-form-error">{{ firstError(field) }}</small>
            }
          </label>
        }
      }

      @if (controller().error(); as failure) {
        <p class="crud-form-error">{{ failure }}</p>
      }

      <div class="crud-form-actions">
        <button type="submit" [disabled]="!canSave()">{{ submitting() ? 'Saving…' : submitLabel() }}</button>
        <button type="button" (click)="cancelled.emit()">Cancel</button>
      </div>
    </form>
  `,
})
export class CrudForm<T extends object = Record<string, unknown>> {
  readonly config = input.required<ResolvedCrudConfig>();
  readonly controller = input.required<CrudController<T>>();
  /** Seed row; null or omitted means "create". */
  readonly value = input<CrudFormValues | null>(null);
  /** Required only when a field's options come from a referenced resource. */
  readonly options = input<OptionResolver | null>(null);

  readonly saved = output<T>();
  readonly cancelled = output<void>();

  readonly fields = computed<readonly ResolvedField[]>(() => this.config().formFields);

  /** Source of truth for the field tree, reseeded whenever the seed row changes. */
  readonly formModel = linkedSignal<CrudFormValues | null, CrudFormValues>({
    source: () => this.value(),
    computation: (seed) => this.seedModel(seed),
  });

  readonly formTree = computed<FieldTree<CrudFormValues>>(() => {
    const config = this.config();
    if (!this.cachedTree || this.cachedConfig !== config) {
      this.cachedConfig = config;
      // `form()` injects, and a computed is not an injection context, so the
      // tree is built inside the component's own injector.
      this.cachedTree = untracked(() =>
        runInInjectionContext(this.injector, () =>
          form(this.formModel, (schema) => applySchema(schema, config)),
        ),
      );
    }
    return this.cachedTree;
  });

  readonly canSave = computed(() => this.controller().can(this.value() ? 'update' : 'create'));
  readonly submitLabel = computed(() => (this.value() ? 'Save changes' : 'Create'));
  readonly submitting = signal(false);

  private readonly injector = inject(Injector);
  private cachedConfig: ResolvedCrudConfig | null = null;
  private cachedTree: FieldTree<CrudFormValues> | null = null;

  private seedModel(seed: CrudFormValues | null): CrudFormValues {
    const values: CrudFormValues = {};
    for (const field of this.fields()) {
      const raw = seed ? seed[field.name] : undefined;
      values[field.name] = raw ?? defaultValueFor(field);
    }
    return values;
  }

  bound(field: ResolvedField): Field<unknown> {
    return (this.formTree() as unknown as Record<string, Field<unknown>>)[field.name];
  }

  showError(field: ResolvedField): boolean {
    const state = this.bound(field)();
    return state.touched() && state.errors().length > 0;
  }

  firstError(field: ResolvedField): string | undefined {
    return this.bound(field)().errors()[0]?.message;
  }

  inputType(field: ResolvedField): string {
    switch (field.type) {
      case 'number':
        return 'number';
      case 'email':
        return 'email';
      case 'password':
        return 'password';
      case 'date':
        return 'date';
      case 'datetime':
        return 'datetime-local';
      default:
        return 'text';
    }
  }

  optionsFor(field: ResolvedField): NzSelectOptionInterface[] {
    const declared = field.options;
    if (Array.isArray(declared)) {
      return declared.map((option) => ({ label: option.label, value: option.value }));
    }
    const resolver = this.options();
    if (!declared || !resolver) return [];
    const resolved: Signal<readonly { label: string; value: string | number }[]> = resolver.forField(field);
    return resolved().map((option) => ({ label: option.label, value: option.value }));
  }

  async onSubmit(event: Event): Promise<void> {
    event.preventDefault();
    if (!this.canSave()) return;
    const tree = this.formTree();
    if (!tree().valid()) {
      tree().markAsTouched();
      return;
    }
    this.submitting.set(true);
    try {
      const values = { ...this.formModel() };
      if (this.value()) {
        const id = this.value()?.[this.config().primaryKey];
        values[this.config().primaryKey] = id;
      }
      const stored = await this.controller().save(values as Partial<T>);
      this.saved.emit(stored as T);
    } finally {
      this.submitting.set(false);
    }
  }
}
