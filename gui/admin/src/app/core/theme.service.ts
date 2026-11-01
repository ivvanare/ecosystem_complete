import { DOCUMENT } from '@angular/common';
import { computed, inject, Injectable, signal } from '@angular/core';

/** The two themes the SPA ships. Anything else in storage is ignored. */
export type Theme = 'light' | 'dark';

/**
 * Storage key for the explicit choice. `index.html` reads the same key before the
 * bundle boots, so the first paint never flashes the wrong theme.
 */
export const THEME_STORAGE_KEY = 'melia.theme';

/**
 * Light/dark theme for the whole SPA. The choice is persisted; with none stored the
 * app follows `prefers-color-scheme`, and the toggle then overrides that until the
 * user switches back by hand.
 *
 * Theming is one attribute on `<html>`: every colour in the app is a CSS variable,
 * so a component never learns which theme is active.
 */
@Injectable({ providedIn: 'root' })
export class ThemeService {
  private readonly document = inject(DOCUMENT);
  private readonly state = signal<Theme>(readInitialTheme());

  readonly theme = this.state.asReadonly();
  readonly isDark = computed(() => this.state() === 'dark');

  constructor() {
    this.apply(this.state());
  }

  toggle(): void {
    this.set(this.state() === 'dark' ? 'light' : 'dark');
  }

  set(theme: Theme): void {
    this.state.set(theme);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch {
      // Private mode with storage disabled: the theme still applies for this page.
    }
    this.apply(theme);
  }

  private apply(theme: Theme): void {
    this.document.documentElement.dataset['theme'] = theme;
  }
}

/** Stored choice first, then the operating system's preference. */
function readInitialTheme(): Theme {
  try {
    const stored = localStorage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark') return stored;
  } catch {
    // Storage unavailable: fall through to the system preference.
  }
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}
