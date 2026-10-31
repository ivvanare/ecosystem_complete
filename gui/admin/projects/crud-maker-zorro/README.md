# crud-maker-zorro

ng-zorro-antd adapter for `crud-maker`. The core resolves *what* to show; this package
decides *how* it looks with ng-zorro primitives.

## Install

```bash
pnpm add crud-maker crud-maker-zorro ng-zorro-antd
```

The adapter is optional by design: a project that wants its own components installs
`crud-maker` alone, and one that wants PrimeNG installs `crud-maker-primeng` instead. The
choice is the dependency list, not a runtime flag.

## What it owns

Only the mapping from the core's declarative hints to ng-zorro's vocabulary:

| Core says | Adapter renders |
|-----------|-----------------|
| `cell.render: 'text'` | the formatted text alone |
| `cell.render: 'tag'` | `<nz-tag>` with the variant as its color |
| `cell.render: 'badge'` | `<nz-badge>` with the variant as its status |
| `cell.render: 'dot'` | a status dot, no text |

`variantMap` values pass straight through to a tag color, because `nz-tag` accepts any color
name. Badge statuses are a closed union, so an unknown variant degrades to `default`
instead of pushing a free-form string into a typed API.

Nothing here formats values: `resolveCellView` in `crud-maker` already returned
`{ kind, text, variant }`, so every adapter shows the same text for the same config.

## Components

### `crud-table`

```html
<crud-table [config]="config" [controller]="controller" (edit)="openForm($event)" (remove)="confirmDelete($event)" />
```

Columns come from `config.listFields`; each cell is a `crud-cell`, so cell hints and formatters behave
exactly as configured. Sorting and paging are translated into `CrudListState` and the table refetches
itself on every change — call `controller.load()` once yourself for the first page. The actions column
appears only when the config (or `ControllerHooks.can`) allows update/delete, and it never mutates: it
emits the row so the consumer decides the dialog, the route and the confirmation.

## Status

Early. `crud-cell` and `crud-table` are implemented and tested; the form and detail screens are the
next deliverables.
