/**
 * The panel's navigation catalogue, in one place: the sidebar, the overview's
 * module grid and the router all read it, so a module can never appear in one
 * and not the other.
 *
 * `status` is honest about what exists today: `live` screens call the API,
 * `soon` screens render a structure with no backend behind them (S4). `permission`
 * is the code the screen needs; the panel is driven by `permissions`, never by
 * role names (`api/API.md` §6).
 */

export type ModuleStatus = 'live' | 'soon';

/** Keys of the single-path icons rendered by the panel templates. */
export type ModuleIcon =
  | 'home'
  | 'places'
  | 'menus'
  | 'reviews'
  | 'routes'
  | 'users'
  | 'roles'
  | 'audit'
  | 'account'
  | 'demo';

export interface PanelModule {
  path: string;
  label: string;
  description: string;
  icon: ModuleIcon;
  status: ModuleStatus;
  permission: string;
}

export interface PanelGroup {
  title: string;
  modules: PanelModule[];
}

export const PANEL_GROUPS: readonly PanelGroup[] = [
  {
    title: 'General',
    modules: [
      {
        path: 'overview',
        label: 'Overview',
        description: 'Recent activity and module status',
        icon: 'home',
        status: 'live',
        permission: 'admin.access',
      },
      {
        path: 'places',
        label: 'Places',
        description: 'Venue records, schedules and photos',
        icon: 'places',
        status: 'soon',
        permission: 'place.create',
      },
      {
        path: 'menus',
        label: 'Menus',
        description: 'Menus and their dishes',
        icon: 'menus',
        status: 'soon',
        permission: 'menu.manage',
      },
      {
        path: 'reviews',
        label: 'Reviews',
        description: 'Moderation and history',
        icon: 'reviews',
        status: 'soon',
        permission: 'review.moderate',
      },
      {
        path: 'routes',
        label: 'Routes',
        description: 'Routes built by users',
        icon: 'routes',
        status: 'soon',
        permission: 'route.manage',
      },
    ],
  },
  {
    title: 'Administration',
    modules: [
      {
        path: 'users',
        label: 'Users',
        description: 'Search users and assign their roles',
        icon: 'users',
        status: 'live',
        permission: 'user.read',
      },
      {
        path: 'roles',
        label: 'Roles & permissions',
        description: 'Which permission each role carries',
        icon: 'roles',
        status: 'live',
        permission: 'role.manage',
      },
      {
        path: 'audit',
        label: 'Audit log',
        description: 'Who changed what, newest first',
        icon: 'audit',
        status: 'live',
        permission: 'admin.access',
      },
      {
        path: 'account',
        label: 'My account',
        description: 'Profile and session',
        icon: 'account',
        status: 'live',
        permission: 'admin.access',
      },
      {
        path: 'faqs',
        label: 'FAQs demo',
        description: 'crud-maker over a memory transport',
        icon: 'demo',
        status: 'live',
        permission: 'admin.access',
      },
    ],
  },
];

export const PANEL_MODULES: readonly PanelModule[] = PANEL_GROUPS.flatMap((g) => g.modules);

/** Every module the session may see, in catalogue order. */
export function visibleModules(
  permissions: readonly string[],
): readonly PanelModule[] {
  return PANEL_MODULES.filter((m) => permissions.includes(m.permission));
}

/**
 * SVG path data for each icon. One path per icon keeps the templates uniform:
 * `<svg><path [attr.d]="ICON_PATHS[module.icon]"/></svg>`.
 */
export const ICON_PATHS: Record<ModuleIcon, string> = {
  home: 'M4 11.5 12 4l8 7.5V20H4z',
  places:
    'M12 21s7-5.4 7-11a7 7 0 1 0-14 0c0 5.6 7 11 7 11Z M12 12.4a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8Z',
  menus: 'M4 6h16M4 12h11M4 18h7',
  reviews: 'm12 4 2.4 5 5.6.8-4 3.9 1 5.5-5-2.7-5 2.7 1-5.5-4-3.9 5.6-.8Z',
  routes:
    'M6 18.2a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8Z M18 8.6a2.4 2.4 0 1 0 0-4.8 2.4 2.4 0 0 0 0 4.8Z M8.2 15.4 15.8 8.6',
  users:
    'M9 11.7a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4Z M3.5 19.5c.6-3 2.8-4.6 5.5-4.6s4.9 1.6 5.5 4.6 M16 5.4a3 3 0 0 1 0 5.7 M17.5 15.2c1.8.6 3 2.1 3.3 4.3',
  roles:
    'M7 10V7.6a5 5 0 0 1 10 0V10 M4.5 10h15v10h-15z',
  audit: 'M6 4h9l4 4v12H6z M9.5 12h6M9.5 15.5h4',
  account:
    'M12 20.4a8.4 8.4 0 1 0 0-16.8 8.4 8.4 0 0 0 0 16.8Z M12 12.6a2.6 2.6 0 1 0 0-5.2 2.6 2.6 0 0 0 0 5.2Z M7.4 18.4c.8-2.2 2.5-3.4 4.6-3.4s3.8 1.2 4.6 3.4',
  demo: 'M4 7h16v10H4z M8 20h8',
};
