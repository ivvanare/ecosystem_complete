# crud-maker

CRUD screens from one JSON-shaped config. You declare an entity — fields, views, filters, sorting, validation, actions — and `crud-maker` gives you a validated schema, a signal-based list state, and a CRUD engine that talks to any backend through a small transport interface. No UI library is coupled to it: today's demo screens are plain HTML, ng-zorro components come later.

Audience: developers adding an entity to the melia admin panel. One config object + one screen is the whole cost.

## Quick path

```ts
// 1. Describe the entity (a plain object with exactly the shape of JSON). Do it
//    once, at startup: defineCrudConfig throws on any problem, so a bad config
//    fails fast instead of misbehaving at runtime.
import { CrudController, defineCrudConfig, HttpTransport } from 'crud-maker';

export const faqsConfig = defineCrudConfig({
  key: 'faqs',
  endpoint: 'faqs',
  titleField: 'question',
  pageSize: 10,
  searchFields: ['question'],
  fields: [
    { name: 'question', required: true, filters: ['like'] },
    { name: 'answer', type: 'textarea' },
    { name: 'views', type: 'number', filters: ['gte'] },
    { name: 'createdAt', type: 'datetime', formatter: 'date', defaultSort: 'desc', in: { form: false } },
  ],
});
```

```ts
// 2. Build the controller per screen (inject() must run in an injection context).
import { HttpClient } from '@angular/common/http';
import { inject } from '@angular/core';

const crud = new CrudController(faqsConfig, new HttpTransport(inject(HttpClient), API_URL));
```

```html
<!-- 3. Bind signals directly in the template. -->
@for (row of crud.rows(); track row['id']) {
  <tr>
    <td>{{ row['question'] }}</td>
    <td><button (click)="crud.state.setSort('views'); crud.load()">Views</button></td>
  </tr>
}
```

```ts
// 4. Mutations persist and reload the list automatically.
await crud.save({ question: 'New?', answer: 'Yes.', views: 0 }); // no id → create
await crud.removeOne(4); // delete, then reload
```

```ts
// 5. Queries: setters validate against the config, then reload.
crud.state.setFilter('question', 'like', 'signal');
crud.state.setPage(2);
await crud.load();
```

A complete working screen lives at `src/app/demo/` (FAQ vertical). It uses an in-memory transport so it runs without a backend.

## Consume from the admin app

- Import **only from `crud-maker`** (the public API is `src/public-api.ts`; deep imports are not supported).
- The workspace maps `crud-maker` → `dist/crud-maker`: after editing the library, run `ng build crud-maker` before app tests or `ng serve`.
- Tests: `ng test crud-maker` (vitest). The library has no runtime dependency beyond `@angular/core` and `@angular/common`.

## Config reference — `defineCrudConfig(raw): ResolvedCrudConfig`

`raw` is `unknown` on purpose: anything (JSON, an object literal, a fetched file) is validated and fully defaulted. The resolved object is what you hand to `CrudController`.

**Document**

| Key | Default | Notes |
|-----|---------|-------|
| `key` | required | Unique entity id (state/routing/permission prefix). |
| `endpoint` | required | REST resource; surrounding slashes are trimmed. |
| `fields` | required, ≥1 | Unique non-empty names. |
| `primaryKey` | `'id'` | If declared explicitly, must exist among fields. |
| `titleField` / `searchFields` | — | Must reference declared fields. |
| `pageSize` | `20` | 1–1000. |
| `actions` | all `true` | `create` / `read` / `update` / `delete` matrix. |

**Field**

| Key | Default | Notes |
|-----|---------|-------|
| `name` | required | Entity property (flat). |
| `label` | `name` | Display text. |
| `type` | `'text'` | `text textarea number date datetime boolean select password email hidden`. |
| `in` | per type (below) | `{list, form, detail}` view membership. |
| `required` | `false` | Sugar: prepends the `required` validator. |
| `sortable` | scalar types → `true` | `false` for `textarea/password/hidden`. |
| `defaultSort` | — | `'asc'`/`'desc'`; exactly one field, must be sortable. |
| `filters` | `[]` | Exposed server-side kinds: `eq like in between gte lte`. |
| `formatter` | — | Named display formatter (see Registry). |
| `validators` | `[]` | Named validators, `'min'` or `{name, params}`. |
| `options` | required for `select` | Static `[{label, value}]` or `{resource, labelField, valueField?}`. |
| `width` / `readonly` | `'default'` / `false` | UI hints; serializable. |

**View defaults by type** (explicit `in` overrides):

| Type | list | form | detail |
|------|------|------|--------|
| most types | ✅ | ✅ | ✅ |
| `textarea` | ❌ | ✅ | ✅ |
| `password`, `hidden` | ❌ | ✅ | ❌ |

**Validation is one-pass.** Every problem is collected and thrown together:

```ts
try {
  defineCrudConfig(bad);
} catch (e) {
  if (e instanceof CrudConfigError) console.table(e.issues);
  // [{ path: 'fields[1].name', code: 'duplicateField', message: ... }, ...]
}
```

Issue codes you will see: `notAnObject`, `required`, `fieldsEmpty`, `duplicateField`, `invalidFieldType`, `invalidType`, `invalidFilterKind`, `invalidWidth`, `invalidPageSize`, `invalidSortDirection`, `sortOnUnsortableField`, `multipleDefaultSort`, `unknownField`, `optionsRequired`, `invalidOptions`, `invalidBehaviorRef`, `unknownFormatter`, `unknownValidator`, `invalidValidatorParams`.

> Option `{resource: …}` is metadata only: the library does **not** fetch it. Resolve it in the consumer (typically with a second `CrudController` over that resource).

## Named behaviors — the registry

Configs reference behavior by name so they stay serializable. Built-ins are installed at import; register your own **before** `defineCrudConfig`, which resolves names against the registry and fails fast on unknown ones.

| Formatter | Output | Params |
|-----------|--------|--------|
| `date` / `datetime` | `yyyy-MM-dd` / `+ HH:mm` | — |
| `number` | grouped, `en-US` | `{locale?, minimumFractionDigits?, maximumFractionDigits?}` |
| `boolean` | `yes` / `no` | — |
| `truncate` | max 80 chars + `…` | `{length?, suffix?}` |

| Validator | Fires when | Params |
|-----------|------------|--------|
| `required` | empty string/null/undefined/`[]` (`0`, `false` pass) | — |
| `min` / `max` | numeric out of range | number |
| `minLength` / `maxLength` | string/array size out of range | number |
| `pattern` | regex source mismatch | string (invalid regex fails at define time) |
| `email` | malformed address | — |

```ts
import { registerFormatter, registerValidator } from 'crud-maker';

registerFormatter('currency', (value, params) =>
  new Intl.NumberFormat('en-US', {
    style: 'currency',
    currency: (params as { currency?: string })?.currency ?? 'USD',
  }).format(Number(value)));

registerValidator('even', () => (value) =>
  Number(value) % 2 === 0 ? null : { code: 'even' });
```

Use `formatValue(field.formatter, row[field.name])` to render a cell. `resetRegistry()` restores built-ins (tests).

## List state — `CrudController.state`

Signals: `page`, `pageSize`, `sort` (`{field, direction} | null`), `filters` (keys `` `${name}_${kind}` ``), `search`. The transport-ready `query` is a `computed`; every setter re-validates against the config and resets to page 1 (except `setPage`).

| Method | Contract |
|--------|----------|
| `setPage(n)` / `setPageSize(n)` | integers; page ≥ 1, size 1–1000; never NaN through to the network |
| `setSort(field, dir?)` / `clearSort()` | same field cycles asc → desc → none; unsortable/unknown throws |
| `setFilter(field, kind, value)` | kind must be exposed by the field; empty/whitespace drops the filter |
| `getFilter` / `clearFilters` / `reset` | `reset` restores config defaults |

Coercion: `number` fields parse (NaN throws), `boolean` parses `'true'/'false'`, `between` requires a two-element array (`null` = open bound; both null drops it), `in` requires a non-empty array.

`normalizeListResult(payload)` accepts the usual REST envelopes — bare array, `{items|data|results, total|count}` and `meta.total` — and throws on anything else instead of silently returning an empty page.

## Engine — `CrudController`

Per-instance (never a root singleton): each screen owns its controller, so two lists never share state.

| Member | Behavior |
|--------|----------|
| `rows`, `total`, `status`, `error` | readonly signals; `status: 'idle' \| 'loading' \| 'ready' \| 'error'` |
| `load()` | fetch `state.query()`; **latest-wins** — stale in-flight responses are discarded |
| `detail(id)` | single-entity read |
| `save(values)` | create when `primaryKey` absent, update when present; reloads after success; returns saved entity |
| `removeOne(id)` | delete + reload |
| `can(action)` | `config.actions[action] && hooks.can?.(action) ?? true` |
| `hooks` | `new CrudController(config, transport, { can: a => permissions.has(a) })` |

Guarded calls reject with `CrudPermissionError` before touching the transport.

## Transport

Implement one interface to swap backends (REST, GraphQL, mocks):

```ts
interface CrudTransport {
  list(endpoint, query: ListQuery): Promise<ListResult>; // {rows, total}
  get(endpoint, id): Promise<Record<string, unknown>>;
  create(endpoint, payload): Promise<Record<string, unknown>>;
  update(endpoint, id, payload): Promise<Record<string, unknown>>;
  remove(endpoint, id): Promise<void>;
}
```

`HttpTransport(http, baseUrl)` speaks this REST dialect:

| Call | Request |
|------|---------|
| list | `GET {base}/{endpoint}?page=2&perPage=10&sort=views:DESC&question_like=x&views_gte=5&createdAt_between=from,to&tags_in=a,b&q=text` |
| get | `GET {base}/{endpoint}/{id}` |
| create | `POST {base}/{endpoint}` |
| update | `PATCH {base}/{endpoint}/{id}` |
| remove | `DELETE {base}/{endpoint}/{id}` |

HTTP failures surface as `CrudTransportError` (carries `status`); anything else from the transport becomes the controller's `error()` string. Different backend dialect? Write another transport — never fork the engine. `encodeListQuery(query)` is exported if you only need to reuse the parameter encoding.

The demo's `MemoryTransport` (`src/app/demo/memory-transport.ts`) is the canonical small implementation: same semantics, in-memory, no network.

## Out of scope (by design)

- **UI**: no components ship with the library. Generic ng-zorro screens are planned as T4 in `odd/tasks/crud-maker.md`.
- **Auth / i18n / notifications**: injected via `hooks`/registry, never imported.
- **Option resource fetching** (see Config reference).

## Consumer checklist

- [ ] You added an entity by writing one config (no engine changes).
- [ ] `defineCrudConfig` runs at startup for your config; CI covers it.
- [ ] Your screen binds `crud.rows() / status() / error()` and never mutates signals from outside.
- [ ] Filter kinds you use are declared in the config (unknown kinds throw).
- [ ] `ng build crud-maker` re-ran after touching the library.
