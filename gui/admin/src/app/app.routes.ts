import { Routes } from '@angular/router';
import { adminAccessGuard } from './core/admin-access.guard';
import { authGuard } from './core/auth.guard';

/**
 * One SPA, two surfaces (S2): a public front door at `/` with the sign-in, and
 * the panel under `/panel`, which needs a session *and* `admin.access`.
 *
 * Screens load lazily so the landing stays a small first paint. Everything under
 * `/panel` shares `PanelLayout` (sidebar built from `permissions` + outlet); the
 * "coming soon" modules reuse one data-driven component instead of four
 * near-identical ones.
 */
export const routes: Routes = [
  {
    path: '',
    title: 'melZone — places, menus and routes',
    loadComponent: () => import('./landing/landing').then((m) => m.Landing),
  },
  {
    path: 'login',
    title: 'Sign in — melZone panel',
    loadComponent: () => import('./auth/login').then((m) => m.Login),
  },
  {
    // Authenticated but without `admin.access`: outside the panel chrome on
    // purpose, so the sidebar never appears for a session that cannot use it.
    path: 'no-access',
    title: 'No access — melZone panel',
    loadComponent: () => import('./access-denied/access-denied').then((m) => m.AccessDenied),
  },
  {
    path: 'panel',
    canActivate: [authGuard, adminAccessGuard],
    loadComponent: () => import('./panel/panel-layout').then((m) => m.PanelLayout),
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'overview' },
      {
        path: 'overview',
        title: 'Overview — melZone panel',
        loadComponent: () => import('./panel/overview/overview').then((m) => m.Overview),
      },
      {
        path: 'users',
        title: 'Users — melZone panel',
        loadComponent: () => import('./panel/users/users').then((m) => m.Users),
      },
      {
        path: 'roles',
        title: 'Roles and permissions — melZone panel',
        loadComponent: () => import('./panel/roles/roles').then((m) => m.Roles),
      },
      {
        path: 'audit',
        title: 'Audit log — melZone panel',
        loadComponent: () => import('./panel/audit/audit').then((m) => m.Audit),
      },
      {
        path: 'account',
        title: 'My account — melZone panel',
        loadComponent: () => import('./account/account').then((m) => m.Account),
      },
      {
        // The existing crud-maker demo, now inside the panel's navigation.
        path: 'faqs',
        title: 'FAQs demo — melZone panel',
        loadComponent: () => import('./demo/faqs-demo').then((m) => m.FaqsDemo),
      },
      {
        path: 'places',
        title: 'Places — melZone panel',
        loadComponent: () => import('./panel/soon/soon').then((m) => m.Soon),
        data: {
          title: 'Places',
          description: 'Venue records with schedules, photos and their social links.',
          permission: 'place.create',
          bullets: [
            'Create and edit a venue, with its category and geo coordinates',
            'Per-day schedules, including venues that cross midnight',
            'One cover photo plus a gallery per venue',
          ],
        },
      },
      {
        path: 'menus',
        title: 'Menus — melZone panel',
        loadComponent: () => import('./panel/soon/soon').then((m) => m.Soon),
        data: {
          title: 'Menus',
          description: 'Menus hang from a venue, and dishes hang from a menu.',
          permission: 'menu.manage',
          bullets: [
            'Several menus per venue: breakfast, lunch, dinner, wine',
            'Dishes with price and description, ordered inside their menu',
            'Publish or hide a menu without touching the venue',
          ],
        },
      },
      {
        path: 'reviews',
        title: 'Reviews — melZone panel',
        loadComponent: () => import('./panel/soon/soon').then((m) => m.Soon),
        data: {
          title: 'Reviews',
          description: 'One review per person and venue, moderated from here.',
          permission: 'review.moderate',
          bullets: [
            'Moderate a review without deleting the author record',
            'Rating history per venue, so a pattern is visible',
            'Every moderation lands in the audit log',
          ],
        },
      },
      {
        path: 'routes',
        title: 'Routes — melZone panel',
        loadComponent: () => import('./panel/soon/soon').then((m) => m.Soon),
        data: {
          title: 'Routes',
          description: 'Ordered visit lists built by the app’s users.',
          permission: 'route.manage',
          bullets: [
            'Read a route with its ordered stops',
            'Spot routes that point at a closed or suspended venue',
            'No editing: the route belongs to its author',
          ],
        },
      },
    ],
  },
  // The panel absorbed the old top-level URLs; keep them working as redirects.
  { path: 'account', pathMatch: 'full', redirectTo: 'panel/account' },
  { path: 'faqs', pathMatch: 'full', redirectTo: 'panel/faqs' },
  { path: '**', redirectTo: '' },
];
