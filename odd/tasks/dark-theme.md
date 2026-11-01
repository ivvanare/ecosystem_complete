# Feature: dark-theme

Objective: give the SPA a persisted light/dark theme that starts from the system preference and reaches every surface — landing, login, panel and the ng-zorro components inside it.
Branch: `feat/dark-theme` (from `feat/admin-crud-screens`, which is what the panel currently is)
Delivery strategy: ask-on-risk (inherited); not published until the human says so (L19 of `admin-crud-screens`)
Test runners: `npx ng test admin --watch=false` and `npx ng build` inside `gui/admin`; compile gate `node node_modules/typescript/bin/tsc --noEmit -p tsconfig.app.json` / `-p tsconfig.spec.json` plus `ngc` with a throwaway strict config

## Specs

- **S1** (verbatim, user, L1): "primero agregemos un thme oscuro por que esto me frie lo ojos"
- **S2** (verbatim option chosen, L2): "Todo el SPA, con toggle", described as: "Toggle persistido; arranca siguiendo el sistema. Implica pasar literales de landing y login a tokens".
  - Acceptance: one control switches light/dark; the choice survives a reload; with no stored choice the app follows `prefers-color-scheme`; landing, login and panel all render legibly in both.
- **S3** (constraint, measured): the panel loads ng-zorro's stylesheet as an asset link (`zorro-styles` handling in `PanelLayout`). Dark mode must swap it for `ng-zorro-antd.dark.min.css` instead of layering both, so the vendor CSS stays one bundle at a time.
- **S4** (constraint): no literal colours left in component stylesheets that a theme must change; they become tokens in `src/styles.css` with a light and a dark value.

## Tasks

| ID | Spec | Status | Evidence |
|----|------|--------|----------|
| T1 | S2, S4 | in progress | token sets (light + dark) in `styles.css`; component literals converted |
| T2 | S2 | pending | `ThemeService` + init before first paint + persisted choice |
| T3 | S2 | pending | toggle control in the panel top bar and the public header |
| T4 | S3 | pending | zorro link swaps between light and dark bundles |
| T5 | S1, S2 | pending | suite + build green (human runner) |

## Log

- **L1** (verbatim, user, 2026-11-01): "primero agregemos un thme oscuro por que esto me frie lo ojos"
- **L2** (verbatim answer, user, 2026-11-01): "Todo el SPA, con toggle" — toggle persistido, arranca siguiendo el sistema.
- **L3** (baseline, parent, 2026-11-01): today the theme is implicit. `src/styles.css` holds ~20 CSS variables used by most panel screens, but `landing.css`, `login.css` and parts of the panel carry literal hex values (`#0b1220`, `#f8fafc`, `#f0f3f8`, tag backgrounds…) that a dark set cannot override. The panel's vendor stylesheet is a single injected `<link data-zorro-styles>` pointing at the light bundle, which makes S3 a one-line swap rather than a second stylesheet.
