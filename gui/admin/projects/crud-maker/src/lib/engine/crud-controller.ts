/**
 * Per-instance CRUD controller: owns the list state, reactive collection
 * signals and the async actions against a pluggable transport. Deliberately
 * NOT a `providedIn:'root'` singleton — each screen builds its own instance,
 * so two lists never share mutable state. Concurrency is latest-wins via a
 * monotonic request id.
 */
import { signal, type Signal, type WritableSignal } from '@angular/core';
import type { ResolvedCrudConfig } from '../config/crud-config.types';
import { CrudListState } from '../state/crud-list-state';
import type { CrudTransport, EntityId } from './transport';

export type CrudAction = 'create' | 'read' | 'update' | 'delete';
export type CrudStatus = 'idle' | 'loading' | 'ready' | 'error';

export class CrudPermissionError extends Error {
  readonly action: CrudAction;
  readonly entityKey: string;

  constructor(action: CrudAction, entityKey: string) {
    super(`action "${action}" not permitted for "${entityKey}"`);
    this.name = 'CrudPermissionError';
    this.action = action;
    this.entityKey = entityKey;
  }
}

export interface ControllerHooks {
  /** Layer above the config matrix (e.g. real user permissions). */
  can?(action: CrudAction): boolean;
}

export class CrudController<T extends object = Record<string, unknown>> {
  readonly state: CrudListState;

  readonly rows: Signal<readonly T[]>;
  readonly total: Signal<number>;
  readonly status: Signal<CrudStatus>;
  readonly error: Signal<string | null>;

  private readonly rowsSig: WritableSignal<T[]> = signal([]);
  private readonly totalSig: WritableSignal<number> = signal(0);
  private readonly statusSig: WritableSignal<CrudStatus> = signal('idle');
  private readonly errorSig: WritableSignal<string | null> = signal(null);
  private requestId = 0;

  constructor(
    readonly config: ResolvedCrudConfig,
    private readonly transport: CrudTransport,
    private readonly hooks: ControllerHooks = {},
  ) {
    this.state = new CrudListState(config);
    this.rows = this.rowsSig.asReadonly();
    this.total = this.totalSig.asReadonly();
    this.status = this.statusSig.asReadonly();
    this.error = this.errorSig.asReadonly();
  }

  can(action: CrudAction): boolean {
    return this.config.actions[action] && (this.hooks.can?.(action) ?? true);
  }

  /** Fetch the current query. Stale in-flight responses are discarded. */
  async load(): Promise<void> {
    this.assertAllowed('read');
    const id = ++this.requestId;
    this.statusSig.set('loading');
    try {
      const result = await this.transport.list(this.config.endpoint, this.state.query());
      if (id !== this.requestId) return;
      this.rowsSig.set(result.rows as T[]);
      this.totalSig.set(result.total);
      this.errorSig.set(null);
      this.statusSig.set('ready');
    } catch (e) {
      if (id !== this.requestId) return;
      this.errorSig.set(e instanceof Error ? e.message : String(e));
      this.statusSig.set('error');
    }
  }

  async detail(id: EntityId): Promise<Record<string, unknown>> {
    this.assertAllowed('read');
    return this.transport.get(this.config.endpoint, id);
  }

  /**
   * Create when the primary key is absent, update when present; then reload
   * the list so the mutation is reflected without extra wiring.
   */
  async save(values: Partial<T>): Promise<Record<string, unknown>> {
    const id = (values as Record<string, unknown>)[this.config.primaryKey];
    const isUpdate = id !== undefined && id !== null;
    this.assertAllowed(isUpdate ? 'update' : 'create');
    const payload = values as Record<string, unknown>;
    const saved = isUpdate
      ? await this.transport.update(this.config.endpoint, id as EntityId, payload)
      : await this.transport.create(this.config.endpoint, payload);
    await this.load();
    return saved;
  }

  async removeOne(id: EntityId): Promise<void> {
    this.assertAllowed('delete');
    await this.transport.remove(this.config.endpoint, id);
    await this.load();
  }

  private assertAllowed(action: CrudAction): void {
    if (!this.can(action)) throw new CrudPermissionError(action, this.config.key);
  }
}
