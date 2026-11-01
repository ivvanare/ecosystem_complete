import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { THEME_STORAGE_KEY, ThemeService } from './theme.service';

describe('ThemeService', () => {
  const root = () => document.documentElement.dataset['theme'];

  function stubSystemPreference(prefersDark: boolean): void {
    vi.stubGlobal(
      'matchMedia',
      vi.fn().mockReturnValue({ matches: prefersDark, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
    );
  }

  beforeEach(() => {
    TestBed.resetTestingModule();
    localStorage.clear();
    delete document.documentElement.dataset['theme'];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  function create(): ThemeService {
    TestBed.configureTestingModule({});
    return TestBed.inject(ThemeService);
  }

  it('follows the system preference when nothing is stored', () => {
    stubSystemPreference(true);
    const theme = create();

    expect(theme.theme()).toBe('dark');
    expect(root()).toBe('dark');
  });

  it('prefers the stored choice over the system', () => {
    stubSystemPreference(true);
    localStorage.setItem(THEME_STORAGE_KEY, 'light');
    const theme = create();

    expect(theme.theme()).toBe('light');
    expect(root()).toBe('light');
  });

  it('toggles, applies and persists the choice', () => {
    stubSystemPreference(false);
    const theme = create();
    expect(theme.isDark()).toBe(false);

    theme.toggle();

    expect(theme.theme()).toBe('dark');
    expect(root()).toBe('dark');
    expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
  });

  it('survives storage being unavailable', () => {
    stubSystemPreference(false);
    const failing = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });

    const theme = create();
    expect(() => theme.set('dark')).not.toThrow();
    expect(root()).toBe('dark');

    failing.mockRestore();
  });
});
