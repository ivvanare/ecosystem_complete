/**
 * Resolves select option references — `{ resource, labelField, valueField? }`
 * declared in JSON — into reactive options, using the SAME transport the
 * engine already talks to. No new HTTP stack, no hardcoded endpoints: the
 * resource key is looked up in the configs map the consumer registers, so a
 * select's data source stays pure config.
 *
 * Signals per resource are cached and shared: several fields pointing at one
 * resource fetch once. Failures land in a visible error signal instead of a
 * silent empty dropdown. `refresh()` re-fetches with stale-response
 * protection (latest-wins per resource).
 */
import { signal, type Signal, type WritableSignal } from '@angular/core';
import type {
  FieldOption,
  OptionResource,
  ResolvedCrudConfig,
  ResolvedField,
} from '../config/crud-config.types';
import type { CrudTransport } from '../engine/transport';

interface Entry {
  resource: OptionResource;
  options: WritableSignal<FieldOption[]>;
  error: WritableSignal<string | null>;
  generation: number;
  started: boolean;
}

export class OptionResolver {
  private readonly entries = new Map<string, Entry>();
  private readonly staticSignals = new WeakMap<ResolvedField, Signal<FieldOption[]>>();

  /**
   * @param configs Known option resources, keyed by the `resource` string used in JSON.
   * @param fetchPageSize Rows requested from the collection endpoint. Defaults to 1000.
   */
  constructor(
    private readonly transport: CrudTransport,
    private readonly configs: Readonly<Record<string, ResolvedCrudConfig>>,
    private readonly fetchPageSize: number = 1000,
  ) {}

  /** Reactive options for a field. Never calls the transport for static/no options. */
  forField(field: ResolvedField): Signal<FieldOption[]> {
    const options = field.options;
    if (!options || Array.isArray(options)) {
      let cached = this.staticSignals.get(field);
      if (!cached) {
        cached = signal<FieldOption[]>((options ?? []).map((option) => ({ ...option })));
        this.staticSignals.set(field, cached);
      }
      return cached;
    }
    return this.forResource(options);
  }

  /** Fetches on first use (unless the resource is unknown) and caches per resource key. */
  forResource(resource: OptionResource): Signal<FieldOption[]> {
    const entry = this.ensure(resource);
    if (!entry.started) {
      entry.started = true;
      void this.fetch(entry);
    }
    return entry.options;
  }

  /** Last failure for the resource, or null. Stable signal, safe before any fetch. */
  error(resourceKey: string): Signal<string | null> {
    return this.ensure({ resource: resourceKey, labelField: 'name' }).error;
  }

  /** Force a re-fetch (e.g. after editing the referenced entity). */
  refresh(resourceKey: string): void {
    const entry = this.ensure({ resource: resourceKey, labelField: 'name' });
    entry.started = true;
    entry.generation += 1;
    void this.fetch(entry);
  }

  private ensure(resource: OptionResource): Entry {
    const existing = this.entries.get(resource.resource);
    if (existing) return existing;
    const created: Entry = {
      resource,
      options: signal<FieldOption[]>([]),
      error: signal<string | null>(null),
      generation: 0,
      started: false,
    };
    this.entries.set(resource.resource, created);
    return created;
  }

  private async fetch(entry: Entry): Promise<void> {
    const config = this.configs[entry.resource.resource];
    if (!config) {
      entry.error.set(`unknown option resource "${entry.resource.resource}"`);
      return;
    }
    const generation = entry.generation;
    try {
      const result = await this.transport.list(config.endpoint, {
        page: 1,
        pageSize: this.fetchPageSize,
        sort: config.defaultSort ? { ...config.defaultSort } : null,
        filters: {},
        search: null,
      });
      if (generation !== entry.generation) return;
      const valueField = entry.resource.valueField ?? config.primaryKey;
      entry.options.set(
        result.rows.map((row) => ({
          label: String(row[entry.resource.labelField] ?? ''),
          value: (row[valueField] as string | number) ?? '',
        })),
      );
      entry.error.set(null);
    } catch (e) {
      if (generation !== entry.generation) return;
      entry.error.set(e instanceof Error ? e.message : String(e));
    }
  }
}
