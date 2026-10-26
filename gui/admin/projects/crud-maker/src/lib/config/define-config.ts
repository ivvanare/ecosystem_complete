/**
 * `defineCrudConfig`: turn an unknown (JSON) document into a fully defaulted,
 * validated `ResolvedCrudConfig`. All problems are collected in one pass and
 * thrown together as `CrudConfigError.issues`, so a broken config reports
 * every mistake at once instead of one per reload.
 *
 * Raw input shapes declare property names with `unknown` values: nothing is
 * trusted until checked.
 */
import {
  CELL_RENDER_KINDS,
  FIELD_TYPES,
  FILTER_KINDS,
  SORT_DIRECTIONS,
  type BehaviorRef,
  type CellRenderKind,
  type FieldType,
  type FieldOption,
  type FieldWidth,
  type FilterKind,
  type OptionResource,
  type ResolvedCrudConfig,
  type ResolvedField,
  type SortDirection,
} from './crud-config.types';
import { compileValidator, hasFormatter, hasValidator } from './registry';

export interface ConfigIssue {
  path: string;
  code: string;
  message: string;
}

export class CrudConfigError extends Error {
  readonly issues: ConfigIssue[];

  constructor(issues: ConfigIssue[]) {
    const summary = issues.map((i) => `${i.path}: ${i.code}`).join('; ');
    super(`crud config invalid (${issues.length} issue(s)): ${summary}`);
    this.name = 'CrudConfigError';
    this.issues = issues;
  }
}

type UnknownRecord = Record<string, unknown>;

interface RawConfig {
  key?: unknown;
  endpoint?: unknown;
  primaryKey?: unknown;
  titleField?: unknown;
  fields?: unknown;
  actions?: unknown;
  pageSize?: unknown;
  searchFields?: unknown;
}

interface RawField {
  name?: unknown;
  label?: unknown;
  type?: unknown;
  in?: unknown;
  required?: unknown;
  sortable?: unknown;
  defaultSort?: unknown;
  filters?: unknown;
  formatter?: unknown;
  validators?: unknown;
  options?: unknown;
  width?: unknown;
  readonly?: unknown;
  cell?: unknown;
}

interface RawCellHint {
  render?: unknown;
  variantMap?: unknown;
}

interface RawViewFlags {
  list?: unknown;
  form?: unknown;
  detail?: unknown;
}

interface RawBehavior {
  name?: unknown;
  params?: unknown;
}

interface RawOption {
  label?: unknown;
  value?: unknown;
}

interface RawOptionResource {
  resource?: unknown;
  labelField?: unknown;
  valueField?: unknown;
}

const isRecord = (value: unknown): value is UnknownRecord =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const isNonEmptyString = (value: unknown): value is string =>
  typeof value === 'string' && value.trim() !== '';

const SORTABLE_TYPES: readonly FieldType[] = ['text', 'number', 'date', 'datetime', 'boolean', 'select', 'email'];
const WIDTHS: readonly string[] = ['narrow', 'default', 'wide'];

function defaultIn(type: FieldType): { list: boolean; form: boolean; detail: boolean } {
  switch (type) {
    case 'password':
    case 'hidden':
      return { list: false, form: true, detail: false };
    case 'textarea':
      return { list: false, form: true, detail: true };
    default:
      return { list: true, form: true, detail: true };
  }
}

class Collector {
  readonly issues: ConfigIssue[] = [];

  add(path: string, code: string, message: string): void {
    this.issues.push({ path, code, message });
  }

  needString(obj: UnknownRecord, key: string, path: string): string | null {
    const value = obj[key];
    if (!isNonEmptyString(value)) {
      this.add(`${path}.${key}`, 'required', `"${key}" must be a non-empty string`);
      return null;
    }
    return value.trim();
  }

  optionalBool(obj: UnknownRecord, key: string, path: string, fallback: boolean): boolean {
    const value = obj[key];
    if (value === undefined) return fallback;
    if (typeof value !== 'boolean') {
      this.add(`${path}.${key}`, 'invalidType', `"${key}" must be a boolean`);
      return fallback;
    }
    return value;
  }

  /** Validate an optional/required field-name reference. */
  checkNameRef(value: unknown, path: string, required: boolean): string | null {
    if (value === undefined) {
      if (required) this.add(path, 'required', 'must be a non-empty string');
      return null;
    }
    if (!isNonEmptyString(value)) {
      this.add(path, 'required', 'must be a non-empty string');
      return null;
    }
    return value;
  }
}

function normalizeBehaviorSpec(
  spec: unknown,
  path: string,
  kind: 'formatter' | 'validator',
  c: Collector,
): BehaviorRef | null {
  if (isNonEmptyString(spec)) return { name: spec };
  if (isRecord(spec) && isNonEmptyString((spec as RawBehavior).name)) {
    const ref: BehaviorRef = { name: (spec as RawBehavior).name as string };
    if ('params' in spec) ref.params = spec['params'];
    return ref;
  }
  c.add(path, 'invalidBehaviorRef', `${kind} must be a name or {name, params}`);
  return null;
}

function resolveOptions(
  raw: RawField,
  type: FieldType,
  path: string,
  c: Collector,
): FieldOption[] | OptionResource | null {
  const value = raw.options;
  if (value === undefined || value === null) {
    if (type === 'select') c.add(`${path}.options`, 'optionsRequired', 'select fields must declare options');
    return null;
  }
  if (Array.isArray(value)) {
    const valid =
      value.length > 0 &&
      value.every(
        (o) => isRecord(o) && isNonEmptyString((o as RawOption).label) &&
          (typeof (o as RawOption).value === 'string' || typeof (o as RawOption).value === 'number'),
      );
    if (!valid) c.add(`${path}.options`, 'invalidOptions', 'options must be a non-empty [{label, value}]');
    return valid ? (value as FieldOption[]) : null;
  }
  if (isRecord(value)) {
    const rec = value as RawOptionResource;
    if (isNonEmptyString(rec.resource) && isNonEmptyString(rec.labelField)) {
      const optionResource: OptionResource = { resource: rec.resource, labelField: rec.labelField };
      if (rec.valueField !== undefined) {
        if (isNonEmptyString(rec.valueField)) optionResource.valueField = rec.valueField;
        else c.add(`${path}.options.valueField`, 'invalidOptions', 'valueField must be a non-empty string');
      }
      return optionResource;
    }
  }
  c.add(`${path}.options`, 'invalidOptions', 'options must be [{label, value}] or {resource, labelField}');
  return null;
}

function resolveField(
  raw: unknown,
  index: number,
  seen: Set<string>,
  sortSpecs: Map<string, SortDirection>,
  c: Collector,
): ResolvedField | null {
  const path = `fields[${index}]`;
  if (!isRecord(raw)) {
    c.add(path, 'required', 'field must be an object');
    return null;
  }
  const f = raw as RawField;
  const dyn = raw as UnknownRecord;

  const name = c.needString(dyn, 'name', path);
  if (name !== null) {
    if (seen.has(name)) c.add(`${path}.name`, 'duplicateField', `duplicate field name "${name}"`);
    else seen.add(name);
  }

  let type: FieldType = 'text';
  if (f.type !== undefined) {
    if (typeof f.type === 'string' && (FIELD_TYPES as readonly string[]).includes(f.type)) {
      type = f.type as FieldType;
    } else {
      c.add(`${path}.type`, 'invalidFieldType', `unknown field type "${String(f.type)}"`);
    }
  }

  let label = name ?? '';
  if (f.label !== undefined) {
    if (isNonEmptyString(f.label)) label = f.label.trim();
    else c.add(`${path}.label`, 'required', 'label must be a non-empty string');
  }

  const viewFlags = defaultIn(type);
  if (f.in !== undefined) {
    if (isRecord(f.in)) {
      const flags = f.in as RawViewFlags;
      for (const view of ['list', 'form', 'detail'] as const) {
        const flag = flags[view];
        if (flag === undefined) continue;
        if (typeof flag !== 'boolean') c.add(`${path}.in.${view}`, 'invalidType', 'view flag must be a boolean');
        else viewFlags[view] = flag;
      }
    } else {
      c.add(`${path}.in`, 'invalidType', 'in must be an object of view flags');
    }
  }

  const sortable = c.optionalBool(dyn, 'sortable', path, SORTABLE_TYPES.includes(type));

  if (f.defaultSort !== undefined && name !== null) {
    if (typeof f.defaultSort === 'string' && (SORT_DIRECTIONS as readonly string[]).includes(f.defaultSort)) {
      if (!sortable) {
        c.add(`${path}.defaultSort`, 'sortOnUnsortableField', `"${name}" is not sortable`);
      } else {
        sortSpecs.set(name, f.defaultSort as SortDirection);
      }
    } else {
      c.add(`${path}.defaultSort`, 'invalidSortDirection', 'defaultSort must be "asc" or "desc"');
    }
  }

  const filters: FilterKind[] = [];
  if (f.filters !== undefined) {
    if (Array.isArray(f.filters)) {
      f.filters.forEach((kind, k) => {
        if (typeof kind === 'string' && (FILTER_KINDS as readonly string[]).includes(kind)) {
          if (!filters.includes(kind as FilterKind)) filters.push(kind as FilterKind);
        } else {
          c.add(`${path}.filters[${k}]`, 'invalidFilterKind', `unknown filter kind "${String(kind)}"`);
        }
      });
    } else {
      c.add(`${path}.filters`, 'invalidType', 'filters must be an array of kinds');
    }
  }

  let formatter: BehaviorRef | null = null;
  if (f.formatter !== undefined) {
    formatter = normalizeBehaviorSpec(f.formatter, `${path}.formatter`, 'formatter', c);
    if (formatter && !hasFormatter(formatter.name)) {
      c.add(`${path}.formatter`, 'unknownFormatter', `no formatter registered as "${formatter.name}"`);
      formatter = null;
    }
  }

  const validators: BehaviorRef[] = [];
  if (f.validators !== undefined) {
    if (Array.isArray(f.validators)) {
      f.validators.forEach((spec, k) => {
        const ref = normalizeBehaviorSpec(spec, `${path}.validators[${k}]`, 'validator', c);
        if (!ref) return;
        if (!hasValidator(ref.name)) {
          c.add(`${path}.validators[${k}]`, 'unknownValidator', `no validator registered as "${ref.name}"`);
          return;
        }
        try {
          compileValidator(ref);
        } catch (e) {
          c.add(`${path}.validators[${k}]`, 'invalidValidatorParams', (e as Error).message);
          return;
        }
        validators.push(ref);
      });
    } else {
      c.add(`${path}.validators`, 'invalidType', 'validators must be an array');
    }
  }
  const required = c.optionalBool(dyn, 'required', path, false);
  if (required && !validators.some((v) => v.name === 'required')) validators.unshift({ name: 'required' });

  const options = resolveOptions(f, type, path, c);

  let width: FieldWidth = 'default';
  if (f.width !== undefined) {
    if (typeof f.width === 'string' && WIDTHS.includes(f.width)) width = f.width as FieldWidth;
    else c.add(`${path}.width`, 'invalidWidth', `unknown width "${String(f.width)}"`);
  }

  const readonly = c.optionalBool(dyn, 'readonly', path, false);

  let cell: { render: CellRenderKind; variantMap: Record<string, string> } = {
    render: 'text',
    variantMap: {},
  };
  if (f.cell !== undefined) {
    if (isRecord(f.cell)) {
      const hint = f.cell as RawCellHint;
      if (hint.render !== undefined) {
        if (
          typeof hint.render === 'string' &&
          (CELL_RENDER_KINDS as readonly string[]).includes(hint.render)
        ) {
          cell = { ...cell, render: hint.render as CellRenderKind };
        } else {
          c.add(`${path}.cell.render`, 'invalidCellRender', `unknown cell render "${String(hint.render)}"`);
        }
      }
      if (hint.variantMap !== undefined) {
        if (isRecord(hint.variantMap) && Object.values(hint.variantMap).every((v) => typeof v === 'string')) {
          cell = { ...cell, variantMap: { ...hint.variantMap } as Record<string, string> };
        } else {
          c.add(`${path}.cell.variantMap`, 'invalidCellVariantMap', 'variantMap must map value → variant strings');
        }
      }
    } else {
      c.add(`${path}.cell`, 'invalidType', 'cell must be an object of rendering hints');
    }
  }

  return {
    name: name ?? '',
    label,
    type,
    in: viewFlags,
    sortable,
    filters,
    formatter,
    validators,
    required,
    options,
    width,
    readonly,
    cell,
  };
}

export function defineCrudConfig(raw: unknown): ResolvedCrudConfig {
  if (!isRecord(raw)) {
    throw new CrudConfigError([
      { path: '$', code: 'notAnObject', message: 'config must be a plain object' },
    ]);
  }
  const c = new Collector();
  const cfg = raw as RawConfig;
  const dyn = raw as UnknownRecord;

  const key = c.needString(dyn, 'key', '$');
  const endpoint = c.needString(dyn, 'endpoint', '$');
  if (endpoint !== null && endpoint.replace(/^\/+|\/+$/g, '') === '') {
    c.add('$.endpoint', 'required', 'endpoint must contain a resource');
  }

  const seen = new Set<string>();
  const sortSpecs = new Map<string, SortDirection>();
  const fields: ResolvedField[] = [];
  if (!Array.isArray(cfg.fields) || cfg.fields.length === 0) {
    c.add('$.fields', 'fieldsEmpty', 'fields must be a non-empty array');
  } else {
    cfg.fields.forEach((f, i) => {
      const resolved = resolveField(f, i, seen, sortSpecs, c);
      if (resolved !== null && resolved.name) fields.push(resolved);
    });
  }
  if (sortSpecs.size > 1) {
    c.add('$.fields', 'multipleDefaultSort', 'only one field may declare defaultSort');
  }

  const fieldMap = new Map(fields.map((f) => [f.name, f]));

  let primaryKey = 'id';
  if (cfg.primaryKey !== undefined) {
    const pk = c.checkNameRef(cfg.primaryKey, '$.primaryKey', false);
    if (pk !== null) {
      primaryKey = pk;
      if (!fieldMap.has(pk)) c.add('$.primaryKey', 'unknownField', `"${pk}" is not a declared field`);
    }
  }

  let titleField: string | null = null;
  if (cfg.titleField !== undefined) {
    const tf = c.checkNameRef(cfg.titleField, '$.titleField', false);
    if (tf !== null) {
      titleField = tf;
      if (!fieldMap.has(tf)) c.add('$.titleField', 'unknownField', `"${tf}" is not a declared field`);
    }
  }

  const searchFields: string[] = [];
  if (cfg.searchFields !== undefined) {
    if (Array.isArray(cfg.searchFields)) {
      cfg.searchFields.forEach((sf, i) => {
        const name = c.checkNameRef(sf, `$.searchFields[${i}]`, false);
        if (name === null) return;
        if (fieldMap.has(name)) searchFields.push(name);
        else c.add(`$.searchFields[${i}]`, 'unknownField', `"${name}" is not a declared field`);
      });
    } else {
      c.add('$.searchFields', 'invalidType', 'searchFields must be an array of field names');
    }
  }

  const actions = { create: true, read: true, update: true, delete: true };
  if (cfg.actions !== undefined) {
    if (isRecord(cfg.actions)) {
      const actionRecord = cfg.actions as UnknownRecord;
      for (const action of ['create', 'read', 'update', 'delete'] as const) {
        actions[action] = c.optionalBool(actionRecord, action, '$.actions', true);
      }
    } else {
      c.add('$.actions', 'invalidType', 'actions must be an object');
    }
  }

  let pageSize = 20;
  if (cfg.pageSize !== undefined) {
    if (typeof cfg.pageSize === 'number' && Number.isInteger(cfg.pageSize) && cfg.pageSize >= 1 && cfg.pageSize <= 1000) {
      pageSize = cfg.pageSize;
    } else {
      c.add('$.pageSize', 'invalidPageSize', 'pageSize must be an integer between 1 and 1000');
    }
  }

  if (c.issues.length > 0) throw new CrudConfigError(c.issues);

  const sortEntry = [...sortSpecs.entries()][0];
  return {
    key: key!,
    endpoint: endpoint!.replace(/^\/+|\/+$/g, ''),
    primaryKey,
    titleField,
    pageSize,
    searchFields,
    actions,
    fields,
    fieldMap,
    listFields: fields.filter((f) => f.in.list),
    formFields: fields.filter((f) => f.in.form),
    detailFields: fields.filter((f) => f.in.detail),
    defaultSort: sortEntry ? { field: sortEntry[0], direction: sortEntry[1] } : null,
  };
}
