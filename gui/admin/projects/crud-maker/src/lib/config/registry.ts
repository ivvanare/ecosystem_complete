/**
 * Name-based registry for cell formatters and field validators.
 *
 * JSON configs reference behavior by name (`"date"`, `"min"`), never by
 * closure, so a config document stays serializable. Apps register custom
 * behavior at startup — before `defineCrudConfig`, which resolves names
 * against this registry and fails fast on unknown ones.
 */
import { formatDate } from '@angular/common';
import type { BehaviorRef } from './crud-config.types';

export type { BehaviorRef };

export type CellFormatter = (value: unknown, params: unknown) => string;
export type ValidationProblem = { code: string; params?: unknown };
export type CompiledValidator = (value: unknown) => ValidationProblem | null;
export type ValidatorFactory = (params: unknown) => CompiledValidator;

export class RegistryError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'RegistryError';
  }
}

const formatters = new Map<string, CellFormatter>();
const validatorFactories = new Map<string, ValidatorFactory>();

export function registerFormatter(name: string, formatter: CellFormatter): void {
  formatters.set(name, formatter);
}

export function registerValidator(name: string, factory: ValidatorFactory): void {
  validatorFactories.set(name, factory);
}

export function hasFormatter(name: string): boolean {
  return formatters.has(name);
}

export function hasValidator(name: string): boolean {
  return validatorFactories.has(name);
}

/** Restore the built-in entries, discarding custom registrations. Intended for tests. */
export function resetRegistry(): void {
  formatters.clear();
  validatorFactories.clear();
  installBuiltIns();
}

/**
 * Render a cell value through the named formatter. `null`/`undefined` always
 * produce `''`; with no formatter the value degrades to `String(value)`.
 */
export function formatValue(ref: BehaviorRef | null | undefined, value: unknown): string {
  if (value === null || value === undefined) return '';
  if (!ref || !ref.name) return String(value);
  const formatter = formatters.get(ref.name);
  if (!formatter) throw new RegistryError(`Unknown formatter "${ref.name}"`);
  return formatter(value, ref.params);
}

/** Resolve a validator ref into a pure predicate. Throws when the name is unknown. */
export function compileValidator(ref: BehaviorRef): CompiledValidator {
  const factory = validatorFactories.get(ref.name);
  if (!factory) throw new RegistryError(`Unknown validator "${ref.name}"`);
  return factory(ref.params);
}

// ---------------------------------------------------------------------------
// Built-ins
// ---------------------------------------------------------------------------

function isEmpty(value: unknown): boolean {
  if (value === null || value === undefined) return true;
  if (typeof value === 'string') return value.trim() === '';
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

function toDateOrText(value: unknown, pattern: string): string {
  try {
    return formatDate(value as Parameters<typeof formatDate>[0], pattern, 'en-US');
  } catch {
    return String(value);
  }
}

function installBuiltIns(): void {
  registerFormatter('date', (value) => toDateOrText(value, 'yyyy-MM-dd'));
  registerFormatter('datetime', (value) => toDateOrText(value, 'yyyy-MM-dd HH:mm'));
  registerFormatter('boolean', (value) => (value ? 'yes' : 'no'));

  registerFormatter('truncate', (value, params) => {
    const opts = (params ?? {}) as { length?: number; suffix?: string };
    const length = opts.length && opts.length > 0 ? opts.length : 80;
    const suffix = opts.suffix ?? '…';
    const text = String(value);
    return text.length <= length ? text : text.slice(0, length) + suffix;
  });

  registerFormatter('number', (value, params) => {
    const opts = (params ?? {}) as {
      locale?: string;
      minimumFractionDigits?: number;
      maximumFractionDigits?: number;
    };
    const numeric = typeof value === 'number' ? value : Number(value);
    if (!Number.isFinite(numeric)) return String(value);
    return new Intl.NumberFormat(opts.locale ?? 'en-US', {
      minimumFractionDigits: opts.minimumFractionDigits,
      maximumFractionDigits: opts.maximumFractionDigits,
    }).format(numeric);
  });

  registerValidator('required', () => (value) => (isEmpty(value) ? { code: 'required' } : null));

  registerValidator('min', (params) => {
    const bound = Number(params);
    if (Number.isNaN(bound)) return () => null;
    return (value) => {
      if (isEmpty(value)) return null;
      const numeric = Number(value);
      if (Number.isNaN(numeric)) return null;
      return numeric < bound ? { code: 'min', params: { min: bound } } : null;
    };
  });

  registerValidator('max', (params) => {
    const bound = Number(params);
    if (Number.isNaN(bound)) return () => null;
    return (value) => {
      if (isEmpty(value)) return null;
      const numeric = Number(value);
      if (Number.isNaN(numeric)) return null;
      return numeric > bound ? { code: 'max', params: { max: bound } } : null;
    };
  });

  registerValidator('minLength', (params) => {
    const bound = Number(params);
    if (Number.isNaN(bound)) return () => null;
    return (value) => {
      if (isEmpty(value)) return null;
      const length = Array.isArray(value) ? value.length : String(value).length;
      return length < bound ? { code: 'minLength', params: { minLength: bound } } : null;
    };
  });

  registerValidator('maxLength', (params) => {
    const bound = Number(params);
    if (Number.isNaN(bound)) return () => null;
    return (value) => {
      if (isEmpty(value)) return null;
      const length = Array.isArray(value) ? value.length : String(value).length;
      return length > bound ? { code: 'maxLength', params: { maxLength: bound } } : null;
    };
  });

  registerValidator('pattern', (params) => {
    if (typeof params !== 'string') {
      throw new RegistryError('pattern validator requires a regex source string');
    }
    let regex: RegExp;
    try {
      regex = new RegExp(params);
    } catch {
      throw new RegistryError(`Invalid pattern "${params}"`);
    }
    return (value) => {
      if (isEmpty(value)) return null;
      return regex.test(String(value)) ? null : { code: 'pattern', params: { pattern: params } };
    };
  });

  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  registerValidator('email', () => (value) => {
    if (isEmpty(value)) return null;
    return emailRegex.test(String(value)) ? null : { code: 'email' };
  });
}

installBuiltIns();
