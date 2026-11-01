# melZone admin (SPA)

Angular 22 admin panel for melZone: the public landing, the panel sign-in, and the
backoffice screens. See `../../AGENTS.md` for the repository rules and the sibling
`crud-maker` checkout this workspace links.

## Routes

| Route | Screen | Access |
|-------|--------|--------|
| `/` | Public landing | anonymous |
| `/login` | Sign in (accounts are provisioned, never self-registered) | anonymous |
| `/no-access` | Signed in but without `admin.access` | session |
| `/panel` | Overview: live totals, recent audit events, module status | `admin.access` |
| `/panel/users` | Users, search, role assignment | `admin.access` |
| `/panel/roles` | Roles and the permission catalogue | `admin.access` |
| `/panel/audit` | Audit log, filter by exact event name | `admin.access` |
| `/panel/account` | Profile and sign out | `admin.access` |
| `/panel/faqs` | `crud-maker` demo over a memory transport | `admin.access` |
| `/panel/places`, `/panel/menus`, `/panel/reviews`, `/panel/routes` | "Coming soon" structure: no API yet | `admin.access` |
| `/account`, `/faqs` | Redirect to the panel routes above | — |

Navigation is data-driven from `src/app/panel/panel-nav.ts`: a module is listed only
when the session holds its `permission`, so the sidebar can never offer a screen the
API would answer with `403 permission_denied`.

## Session

The panel uses the browser transport (`../../api/API.md` §2): the refresh token lives
in an httpOnly cookie, the CSRF value in `sessionStorage`, and the access token in
memory only. `AuthService` rotates the session through one shared in-flight request,
because the API revokes the whole family when a rotated token is replayed.

## Design

Visual directions for this panel live in `design/admin-dashboard/` (static artboards,
`design.json` manifest). `design/system.md` does not exist yet; the implementation
tokens are the CSS variables at the top of `src/styles.css`.

## Development server

```bash
npx ng serve          # http://localhost:4200
npx ng test admin --watch=false
npx ng build
```

The dev build points at `http://localhost:9000/v1` through the `API_URL` injection
token; `docker compose up -d` in the repository root starts the API and its services.
