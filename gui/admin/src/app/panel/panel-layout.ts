import { DOCUMENT } from '@angular/common';
import { Component, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router, RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { filter, map, startWith } from 'rxjs';
import { AuthService } from '../core/auth.service';
import {
  CaretDownOutline,
  CaretUpOutline,
  DownOutline,
  FilterOutline,
} from '@ant-design/icons-angular/icons';
import { NzI18nService, en_US } from 'ng-zorro-antd/i18n';
import { NzIconService } from 'ng-zorro-antd/icon';
import { ICON_PATHS, PANEL_GROUPS, PANEL_MODULES } from './panel-nav';

/**
 * Chrome for every `/panel` route: the sidebar built from the module catalogue
 * filtered by `permissions`, the top bar, and the outlet the screens render in.
 *
 * The navigation is data-driven for one reason: a module can never be listed
 * without its route, or shown to a session that cannot open it (S8).
 */
@Component({
  selector: 'app-panel-layout',
  imports: [RouterOutlet, RouterLink, RouterLinkActive],
  templateUrl: './panel-layout.html',
  styleUrl: './panel-layout.css',
})
export class PanelLayout {
  protected readonly auth = inject(AuthService);
  private readonly router = inject(Router);

  protected readonly icons = ICON_PATHS;

  /** Catalogue groups with every module this session may not open removed. */
  protected readonly groups = computed(() => {
    const permissions = this.auth.permissions();
    return PANEL_GROUPS.map((group) => ({
      title: group.title,
      modules: group.modules.filter((module) => permissions.includes(module.permission)),
    })).filter((group) => group.modules.length > 0);
  });

  private readonly url = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map((event) => event.urlAfterRedirects),
      startWith(this.router.url),
    ),
    { initialValue: this.router.url },
  );

  /** Label of the module currently open, for the breadcrumb. */
  protected readonly currentLabel = computed(() => {
    const url = this.url();
    return PANEL_MODULES.find((module) => url.startsWith(`/panel/${module.path}`))?.label ?? 'Overview';
  });

  private readonly document = inject(DOCUMENT);
  private readonly iconService = inject(NzIconService);
  private readonly i18n = inject(NzI18nService);

  constructor() {
    // ng-zorro lives only inside the panel: its stylesheet is an asset fetched on
    // entry (not a global import, which cost the landing 570 kB of CSS) and its
    // services are injected here rather than provided in `app.config`, which kept
    // 90 kB of zorro core in the initial bundle.
    this.loadZorro();
  }

  protected readonly initials = computed(() => {
    const user = this.auth.user();
    const source = user?.name?.trim() || user?.email?.trim() || '?';
    const parts = source.split(/[\s@.]+/).filter(Boolean);
    return parts
      .slice(0, 2)
      .map((part) => part[0]!.toUpperCase())
      .join('');
  });

  protected readonly roleLabel = computed(() => this.auth.user()?.roles.join(', ') ?? 'no role');

  private loadZorro(): void {
    if (!this.document.querySelector('link[data-zorro-styles]')) {
      const link = this.document.createElement('link');
      link.rel = 'stylesheet';
      link.href = 'ng-zorro-antd.min.css';
      link.setAttribute('data-zorro-styles', '');
      this.document.head.appendChild(link);
    }
    // Only the glyphs the table draws: registering the whole set would pull
    // thousands of paths into the bundle for four carets.
    for (const icon of [CaretDownOutline, CaretUpOutline, DownOutline, FilterOutline]) {
      this.iconService.addIcon(icon);
    }
    // ng-zorro falls back to zh_CN; the panel is English.
    this.i18n.setLocale(en_US);
  }

  protected async signOut(): Promise<void> {
    await this.auth.logout();
    await this.router.navigateByUrl('/login');
  }
}
