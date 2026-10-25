/**
 * Public config vocabulary for crud-maker.
 *
 * Everything declared here must stay JSON-serializable: no functions, no
 * classes, no prototypes. Custom behavior (cell formatters, validators) is
 * referenced by name and resolved through the registry, so a plain JSON
 * document is a valid, complete CRUD config.
 */

export const FIELD_TYPES = [
  'text',
  'textarea',
  'number',
  'date',
  'datetime',
  'boolean',
  'select',
  'password',
  'email',
  'hidden',
] as const;
export type FieldType = (typeof FIELD_TYPES)[number];

export const FILTER_KINDS = ['eq', 'like', 'in', 'between', 'gte', 'lte'] as const;
export type FilterKind = (typeof FILTER_KINDS)[number];

export const SORT_DIRECTIONS = ['asc', 'desc'] as const;
export type SortDirection = (typeof SORT_DIRECTIONS)[number];

export type FieldWidth = 'narrow' | 'default' | 'wide';

/** Reference to a registered formatter/validator with optional params. */
export interface BehaviorRef {
  name: string;
  params?: unknown;
}
/** Terse JSON form allowed: `"formatter": "date"`. */
export type BehaviorSpec = string | BehaviorRef;

export interface FieldOption {
  label: string;
  value: string | number;
}

/** Options loaded from another CRUD resource (resolved by the engine, not in JSON). */
export interface OptionResource {
  resource: string;
  labelField: string;
  valueField?: string;
}

export interface ViewFlags {
  list?: boolean;
  form?: boolean;
  detail?: boolean;
}

export interface FieldConfig {
  /** Entity property name (flat; unique within the config). */
  name: string;
  /** Display label. Defaults to `name`. */
  label?: string;
  /** Defaults to `text`. */
  type?: FieldType;
  /** Which views show this field. Sensible per-type defaults apply. */
  in?: ViewFlags;
  /** Sugar for the `required` validator. */
  required?: boolean;
  /** Defaults to true for scalar types, false for textarea/password/hidden. */
  sortable?: boolean;
  /** Exactly one field may carry a default sort; it must be sortable. */
  defaultSort?: SortDirection;
  /** Server-side filter kinds exposed for this field. */
  filters?: FilterKind[];
  /** Named cell formatter used when rendering the value as text. */
  formatter?: BehaviorSpec;
  /** Named validators. */
  validators?: BehaviorSpec[];
  /** Required when `type` is `select`. */
  options?: FieldOption[] | OptionResource;
  width?: FieldWidth;
  readonly?: boolean;
}

export interface CrudActionsConfig {
  create?: boolean;
  read?: boolean;
  update?: boolean;
  delete?: boolean;
}

export interface CrudConfig {
  /** Stable unique key for the entity (state/route/i18n prefix). */
  key: string;
  /** REST resource, e.g. `faqs`. */
  endpoint: string;
  /** Defaults to `id`; must exist among the fields. */
  primaryKey?: string;
  /** Field used to name an instance (confirm dialogs, breadcrumbs). */
  titleField?: string;
  fields: FieldConfig[];
  /** All actions enabled by default. */
  actions?: CrudActionsConfig;
  /** Initial page size. Defaults to 20, max 1000. */
  pageSize?: number;
  /** Global-search field names. */
  searchFields?: string[];
}

/** Fully defaulted, validated field produced by `defineCrudConfig`. */
export interface ResolvedField {
  name: string;
  label: string;
  type: FieldType;
  in: { list: boolean; form: boolean; detail: boolean };
  sortable: boolean;
  filters: readonly FilterKind[];
  formatter: BehaviorRef | null;
  validators: readonly BehaviorRef[];
  required: boolean;
  options: FieldOption[] | OptionResource | null;
  width: FieldWidth;
  readonly: boolean;
}

/** Fully defaulted, validated config produced by `defineCrudConfig`. */
export interface ResolvedCrudConfig {
  key: string;
  /** Normalized without surrounding slashes. */
  endpoint: string;
  primaryKey: string;
  titleField: string | null;
  pageSize: number;
  searchFields: readonly string[];
  actions: { create: boolean; read: boolean; update: boolean; delete: boolean };
  fields: readonly ResolvedField[];
  fieldMap: ReadonlyMap<string, ResolvedField>;
  listFields: readonly ResolvedField[];
  formFields: readonly ResolvedField[];
  detailFields: readonly ResolvedField[];
  defaultSort: { field: string; direction: SortDirection } | null;
}
