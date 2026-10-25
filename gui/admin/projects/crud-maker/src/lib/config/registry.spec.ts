import { beforeEach, describe, expect, it } from 'vitest';
import {
  compileValidator,
  formatValue,
  hasFormatter,
  hasValidator,
  registerFormatter,
  registerValidator,
  resetRegistry,
  RegistryError,
  type BehaviorRef,
} from './registry';

const ref = (name: string, params?: unknown): BehaviorRef => ({ name, params });

describe('registry: formatters', () => {
  beforeEach(() => resetRegistry());

  it('formats null-ish values as empty string for any formatter', () => {
    expect(formatValue(ref('date'), null)).toBe('');
    expect(formatValue(ref('truncate'), undefined)).toBe('');
  });

  it('falls back to String(value) when no formatter is given', () => {
    expect(formatValue(null, 42)).toBe('42');
    expect(formatValue(undefined, 'x')).toBe('x');
  });

  it('date renders yyyy-MM-dd and datetime renders yyyy-MM-dd HH:mm (en-US)', () => {
    expect(formatValue(ref('date'), '2026-10-24T18:30:00')).toBe('2026-10-24');
    expect(formatValue(ref('datetime'), '2026-10-24T18:30:00')).toBe('2026-10-24 18:30');
  });

  it('date passes through unparseable values as text', () => {
    expect(formatValue(ref('date'), 'not-a-date')).toBe('not-a-date');
  });

  it('truncate defaults to 80 chars with ellipsis and honors params', () => {
    const long = 'a'.repeat(100);
    expect(formatValue(ref('truncate'), long)).toBe('a'.repeat(80) + '…');
    expect(formatValue(ref('truncate', { length: 5 }), 'abcdefg')).toBe('abcde…');
    expect(formatValue(ref('truncate', { length: 10, suffix: '...' }), 'short')).toBe('short');
  });

  it('number uses en-US grouping and honors fraction params', () => {
    expect(formatValue(ref('number'), 1234.5)).toBe('1,234.5');
    expect(formatValue(ref('number', { minimumFractionDigits: 2 }), 1234)).toBe('1,234.00');
    expect(formatValue(ref('number'), 'abc')).toBe('abc');
  });

  it('boolean maps to yes/no', () => {
    expect(formatValue(ref('boolean'), true)).toBe('yes');
    expect(formatValue(ref('boolean'), 0)).toBe('no');
  });

  it('throws RegistryError on unknown formatter name', () => {
    expect(() => formatValue(ref('nope'), 1)).toThrowError(RegistryError);
  });

  it('allows registering and overriding custom formatters', () => {
    registerFormatter('test.upper', (v) => String(v).toUpperCase());
    expect(formatValue(ref('test.upper'), 'hi')).toBe('HI');
    registerFormatter('date', () => 'stubbed');
    expect(formatValue(ref('date'), '2026-01-01')).toBe('stubbed');
  });
});

describe('registry: validators', () => {
  beforeEach(() => resetRegistry());

  it('required rejects empty strings, null, undefined and empty arrays but accepts 0 and false', () => {
    const required = compileValidator(ref('required'));
    expect(required('')?.code).toBe('required');
    expect(required('   ')?.code).toBe('required');
    expect(required(null)?.code).toBe('required');
    expect(required(undefined)?.code).toBe('required');
    expect(required([])?.code).toBe('required');
    expect(required(0)).toBeNull();
    expect(required(false)).toBeNull();
    expect(required('x')).toBeNull();
  });

  it('skips optional validators on empty values (required handles emptiness)', () => {
    const min = compileValidator(ref('min', 5));
    expect(min(null)).toBeNull();
    expect(min('')).toBeNull();
  });

  it('min/max compare numerically and pass through non-numeric values', () => {
    const min = compileValidator(ref('min', 5));
    const max = compileValidator(ref('max', 10));
    expect(min(3)?.code).toBe('min');
    expect(min(5)).toBeNull();
    expect(max(11)?.code).toBe('max');
    expect(max('abc')).toBeNull();
  });

  it('minLength/maxLength work on strings and arrays', () => {
    const minLen = compileValidator(ref('minLength', 3));
    const maxLen = compileValidator(ref('maxLength', 4));
    expect(minLen('ab')?.code).toBe('minLength');
    expect(minLen([1, 2])?.code).toBe('minLength');
    expect(minLen('abc')).toBeNull();
    expect(maxLen('abcde')?.code).toBe('maxLength');
    expect(maxLen([1, 2, 3, 4, 5])?.code).toBe('maxLength');
  });

  it('pattern tests the value against a regex source string', () => {
    const digits = compileValidator(ref('pattern', '^\\d+$'));
    expect(digits('123')).toBeNull();
    expect(digits('12a')?.code).toBe('pattern');
  });

  it('pattern with invalid regex throws RegistryError at compile time', () => {
    expect(() => compileValidator(ref('pattern', '(['))).toThrowError(RegistryError);
  });

  it('email rejects malformed addresses and accepts valid ones', () => {
    const email = compileValidator(ref('email'));
    expect(email('a@b.co')).toBeNull();
    expect(email('a@b')?.code).toBe('email');
    expect(email(null)).toBeNull();
  });

  it('throws RegistryError on unknown validator name', () => {
    expect(() => compileValidator(ref('nope'))).toThrowError(RegistryError);
  });

  it('allows registering custom validators and overriding built-ins', () => {
    registerValidator('test.even', (params) => (value) => {
      if (value === null || value === undefined || value === '') return null;
      return Number(value) % 2 === 0 ? null : { code: 'even', params };
    });
    expect(hasValidator('test.even')).toBe(true);
    expect(compileValidator(ref('test.even'))(3)?.code).toBe('even');
    expect(compileValidator(ref('test.even'))(4)).toBeNull();
  });

  it('hasFormatter reports built-ins', () => {
    expect(hasFormatter('truncate')).toBe(true);
    expect(hasFormatter('missing.xyz')).toBe(false);
  });
});
