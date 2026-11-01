import { Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { ThemeService } from '../core/theme.service';
import { AppPreview } from './app-preview';
import { PanelPreview } from './panel-preview';

/**
 * Public landing at `/` (S2): the product page for melZone, with the only
 * call to action that goes anywhere today — the panel sign-in. It renders for
 * an anonymous visitor and issues no API request, so a dead API never blocks
 * the front door.
 *
 * The app preview inside the hero is illustrative content, not live data;
 * nothing on this page claims a number the API cannot prove.
 */
@Component({
  selector: 'app-landing',
  imports: [RouterLink, AppPreview, PanelPreview],
  templateUrl: './landing.html',
  styleUrl: './landing.css',
})
export class Landing {
  private readonly theme = inject(ThemeService);
  protected readonly isDark = this.theme.isDark;

  protected toggleTheme(): void {
    this.theme.toggle();
  }
}
