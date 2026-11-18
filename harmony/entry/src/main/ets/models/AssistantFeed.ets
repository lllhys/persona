import { receiptWireState } from './AssistantReceipt';
import { Wire, ResourceRow } from './Wire';
import type { ClientModel } from './ClientModel';
import type { HubRepository } from './HubRepository';

function segment(value: string): string { return encodeURIComponent(value); }

function textField(value: Object | undefined, key: string, fallback: string = ''): string {
  return Wire.text(Wire.record(value), key, fallback);
}

function hasId(values: string[], id: string): boolean { return values.includes(id); }

function updated(values: string[], id: string, add: boolean): string[] {
  const next = values.filter((value: string) => value !== id);
  if (add && id) next.push(id);
  return next;
}

function timestamp(value: string): number {
  const parsed = Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function draftPlaceholder(value: string): string {
  const match = /【[^】\n]*】|\{\{[^}\n]*\}\}/.exec(value);
  return match ? match[0] : '';
}

interface AssistantRouteSnapshot {
  revision: number;
  admit: (() => boolean) | undefined;
}

/** Editable values kept separately from the server's immutable plan until the owner presses Start. */
@ObservedV2
export class PlanEditTask {
  @Trace title: string = '';
  @Trace prompt: string = '';
  @Trace machine: string = '';
  @Trace cwd: string = '';
  @Trace permission: string = '';
  @Trace readonly: boolean = false;

  constructor(source: Record<string, Object> = {}) {
    this.title = Wire.text(source, 'title');
    this.prompt = Wire.text(source, 'prompt');
    this.machine = Wire.text(source, 'machine');
    this.cwd = Wire.text(source, 'cwd');
    this.permission = Wire.text(source, 'permission', 'default');
    this.readonly = Wire.bool(source, 'readonly');
    if (this.readonly) this.permission = 'plan';
  }

  value(key: string): string {
    if (key === 'title') return this.title;
    if (key === 'prompt') return this.prompt;
    if (key === 'machine') return this.machine;
    if (key === 'cwd') return this.cwd;
    if (key === 'permission') return this.permission;
    return '';
  }
}

/** Scoped, in-memory projection of one assistant conversation's real Hub feed. */
@ObservedV2
export class AssistantFeedModel {
  @Trace loading: boolean = false;
  @Trace loadingMore: boolean = false;
  @Trace error: string = '';
  @Trace todoError: string = '';
  @Trace todosReady: boolean = false;
  @Trace itemLookupStatus: string = '';
  @Trace conversationId: string = '';
  @Trace owner: string = '';
  @Trace items: Record<string, Object>[] = [];
  @Trace plans: Record<string, Object> = {};
  @Trace freshPlanIds: string[] = [];
  @Trace loadingPlanIds: string[] = [];
  @Trace planActionErrors: Record<string, Object> = {};
  @Trace taskCards: Record<string, Object>[] = [];
  @Trace todos: Record<string, Object>[] = [];
  @Trace views: Record<string, Object> = {};
  @Trace viewActions: Record<string, Object> = {};
  @Trace viewBlocks: string[] = [];
  @Trace nextCursor: string = '';
  @Trace hasLoaded: boolean = false;
  @Trace editing: boolean = false;
  @Trace editingPlanId: string = '';
  @Trace editingPlanVersion: string = '';
  @Trace editTasks: PlanEditTask[] = [];
  @Trace rememberPlans: string[] = [];
  @Trace busyKeys: string[] = [];
  @Trace uncertainKeys: string[] = [];
  @Trace pendingMessageIds: string[] = [];
  @Trace messageDrafts: Record<string, Object> = {};
  private generation: number = 0;
  private clientGeneration: number = -1;
  private scopeKey: string = '';
  private targetKey: string = '';
  private client: ClientModel | undefined;
  private repository: HubRepository | undefined;
  private routeAdmission: (() => boolean) | undefined;
  private routeAdmissionRevision: number = 0;
  private refreshPromise: Promise<void> | undefined;
  private refreshRun: number = -1;
  private offEvents: (() => void) | undefined;

  private current(run: number): boolean {
    const client = this.client;
    return !!client && run === this.generation && client.owns(this.clientGeneration, this.scopeKey) &&
      client.scope() === this.scopeKey && client.target() === this.targetKey;
  }

  /** The host supplies route visibility. Existing non-routed callers remain admitted by default. */
  setRouteAdmission(admit?: () => boolean): void {
    this.routeAdmission = admit;
    this.routeAdmissionRevision++;
  }

  private routeSnapshot(): AssistantRouteSnapshot {
    return { revision: this.routeAdmissionRevision, admit: this.routeAdmission };
  }

  private routeIsCurrent(snapshot: AssistantRouteSnapshot): boolean {
    try { return this.routeAdmissionRevision === snapshot.revision && (!snapshot.admit || snapshot.admit()); }
    catch (_) { return false; }
  }

  private routeAdmitted(): boolean {
    const snapshot: AssistantRouteSnapshot = this.routeSnapshot();
    return this.routeIsCurrent(snapshot);
  }

  private clearState(): void {
    this.loading = false; this.loadingMore = false; this.error = ''; this.todoError = ''; this.todosReady = false; this.itemLookupStatus = '';
    this.conversationId = ''; this.owner = ''; this.items = []; this.plans = {}; this.freshPlanIds = []; this.loadingPlanIds = []; this.planActionErrors = {}; this.taskCards = []; this.todos = [];
    this.views = {}; this.viewActions = {}; this.viewBlocks = []; this.nextCursor = ''; this.hasLoaded = false;
    this.editing = false; this.editingPlanId = ''; this.editingPlanVersion = ''; this.editTasks = []; this.rememberPlans = [];
    this.busyKeys = []; this.uncertainKeys = []; this.pendingMessageIds = []; this.messageDrafts = {};
  }

  /** Captures one conversation's generation, scope, selected identity and repository before starting any reads. */
  async open(model: ClientModel, row: ResourceRow, load: boolean = true, routeAdmission?: () => boolean): Promise<void> {
    this.close();
    if (routeAdmission) this.setRouteAdmission(routeAdmission);
    const run = this.generation;
    if (row.kind !== 'conversation' || !row.id || model.target() !== model.target(row)) {
      this.error = 'request_updated';
      return;
    }
    const owner = row.manager || model.profile.manager;
    if (owner !== model.profile.manager) {
      this.error = 'request_updated';
      return;
    }
    let repository: HubRepository;
    try { repository = model.repo(); } catch (_) { this.error = 'offline'; return; }
    this.client = model;
    this.repository = repository;
    this.clientGeneration = model.connectionGeneration();
    this.scopeKey = model.scope();
    this.targetKey = model.target(row);
    this.conversationId = row.id;
    this.owner = owner;
    if (!this.current(run)) { this.error = 'request_updated'; return; }
    this.offEvents = model.observeEvents((raw: string): void => this.receiveEvent(raw, run));
    if (load) await this.refresh();
  }

  close(): void {
    this.offEvents?.(); this.offEvents = undefined;
    this.setRouteAdmission(undefined);
    this.generation++;
    this.client = undefined; this.repository = undefined; this.refreshPromise = undefined; this.refreshRun = -1;
    this.clientGeneration = -1; this.scopeKey = ''; this.targetKey = '';
    this.clearState();
  }

  private mergeViews(views: Record<string, Object>): void {
    const next: Record<string, Object> = {};
    for (const key of Object.keys(this.views)) next[key] = this.views[key];
    for (const key of Object.keys(views)) {
      const incoming = Wire.record(views[key]), conversation = Wire.text(incoming, 'conversation'), version = incoming['version'];
      if (Wire.text(incoming, 'id') !== key || conversation && conversation !== this.conversationId ||
        typeof version !== 'number' || !Number.isSafeInteger(version) || version <= 0) continue;
      const previous = Wire.record(next[key]);
      if (!Object.keys(previous).length || Wire.number(previous, 'version') < version) next[key] = incoming;
    }
    this.views = next;
  }
  private mergeViewActions(actions: Record<string, Object>): void {
    const next: Record<string, Object> = {};
    for (const key of Object.keys(this.viewActions)) next[key] = this.viewActions[key];
    for (const key of Object.keys(actions)) {
      const incoming = Wire.record(actions[key]), previous = Wire.record(next[key]);
      if (Wire.text(incoming, 'key') !== key || !Wire.text(incoming, 'action') || !Wire.text(incoming, 'client_msg_id') || !Wire.text(incoming, 'status')) continue;
      if (Object.keys(previous).length) {
        const immutable = ['action', 'client_msg_id', 'page', 'version', 'text'];
        const previousValues = Wire.object(previous, 'values'), incomingValues = Wire.object(incoming, 'values');
        if (immutable.some((field: string) => JSON.stringify(previous[field] ?? '') !== JSON.stringify(incoming[field] ?? '')) ||
          (Wire.text(previous, 'kind') || 'action') !== (Wire.text(incoming, 'kind') || 'action') ||
          Object.keys(previousValues).length !== Object.keys(incomingValues).length || Object.keys(previousValues).some((name: string) => previousValues[name] !== incomingValues[name]) ||
          Wire.number(incoming, 'revision') <= Wire.number(previous, 'revision')) continue;
      }
      next[key] = incoming;
    }
    this.viewActions = next;
  }
  private mergeViewBlocks(keys: string[]): void {
    const next = this.viewBlocks.slice();
    for (const key of keys) if (key && key.length <= 512 && !/[\u0000-\u001f\u007f]/.test(key) && !next.includes(key)) next.push(key);
    this.viewBlocks = next;
  }
  private receiveEvent(raw: string, run: number): void {
    if (!this.current(run) || raw.length > 2 * 1024 * 1024) return;
    const frame = Wire.parse(raw);
    if (Wire.has(frame, 'manager') && Wire.text(frame, 'manager') !== this.owner || Wire.text(frame, 'conversation') !== this.conversationId) return;
    const type = Wire.text(frame, 'type');
    if (type === 'assistant_view') {
      const view = Wire.object(frame, 'view'), id = Wire.text(view, 'id'); if (id) this.mergeViews(Wire.payload([id], [view]));
    } else if (type === 'assistant_view_action') {
      const action = Wire.object(frame, 'action'), key = Wire.text(action, 'key'); if (key) this.mergeViewActions(Wire.payload([key], [action]));
    } else if (type === 'assistant_view_block') this.mergeViewBlocks([Wire.text(frame, 'key')]);
    else if (type === 'assistant_feed') {
      const item = Wire.object(frame, 'item');
      if (Wire.text(item, 'conversation', this.conversationId) !== this.conversationId || Wire.text(item, 'manager', this.owner) !== this.owner) return;
      this.mergeItems([item]);
      const view = Wire.object(Wire.object(item, 'body'), 'view'), id = Wire.text(view, 'id');
      if (id && Wire.text(item, 'ref') === id) this.mergeViews(Wire.payload([id], [view]));
      this.refresh().catch(() => undefined);
    }
  }

  private feedPath(cursor: string = '', since: string = ''): string {
    let path = '/api/v1/assistant/conversations/' + segment(this.conversationId) + '/feed?paginate=1';
    if (this.owner) path += '&manager=' + segment(this.owner);
    if (cursor) path += '&cursor=' + segment(cursor);
    else if (since) path += '&since=' + segment(since);
    return path;
  }

  private ownerPath(path: string): string { return this.owner ? path + '?manager=' + segment(this.owner) : path; }

  private mergeItems(incoming: Record<string, Object>[]): void {
    const byId: Record<string, Record<string, Object>> = {};
    this.items.forEach((item: Record<string, Object>) => {
      const id = Wire.text(item, 'id'); if (id) byId[id] = item;
    });
    incoming.forEach((item: Record<string, Object>) => {
      const id = Wire.text(item, 'id'); if (id) byId[id] = item;
    });
    this.items = Object.keys(byId).map((id: string) => byId[id]).sort((left: Record<string, Object>, right: Record<string, Object>) => {
      const order = timestamp(Wire.text(left, 'at')) - timestamp(Wire.text(right, 'at'));
      return order !== 0 ? order : Wire.text(left, 'id').localeCompare(Wire.text(right, 'id'));
    });
  }

  private mergePlans(incoming: Record<string, Object>): void {
    const next: Record<string, Object> = {};
    Object.keys(this.plans).forEach((id: string) => next[id] = this.plans[id]);
    Object.keys(incoming).forEach((id: string) => {
      const plan = Wire.record(incoming[id]);
      const conversation = Wire.text(plan, 'conversation');
      if (conversation && conversation !== this.conversationId) return;
      next[id] = plan;
    });
    this.plans = next;
  }

  private mergeTaskCards(incoming: Record<string, Object>[]): void {
    if (!incoming.length) return; // Empty/null projections are upsert pages, never a deletion snapshot.
    const byId: Record<string, Record<string, Object>> = {};
    this.taskCards.forEach((card: Record<string, Object>) => {
      const id = Wire.text(card, 'id'); if (id) byId[id] = card;
    });
    incoming.forEach((card: Record<string, Object>) => {
      const id = Wire.text(card, 'id');
      const planId = Wire.text(card, 'plan_id');
      if (planId) {
        const plan = Wire.record(this.plans[planId]);
        if (Wire.text(plan, 'conversation') && Wire.text(plan, 'conversation') !== this.conversationId) return;
      }
      if (id) byId[id] = card;
    });
    this.taskCards = Object.keys(byId).map((id: string) => byId[id]).sort((left: Record<string, Object>, right: Record<string, Object>) => {
      const order = timestamp(Wire.text(left, 'at')) - timestamp(Wire.text(right, 'at'));
      return order !== 0 ? order : Wire.text(left, 'id').localeCompare(Wire.text(right, 'id'));
    });
  }

  private mergePage(result: Record<string, Object>): void {
    const incomingItems = Wire.rows(result, 'items').filter((item: Record<string, Object>) => {
      const conversation = Wire.text(item, 'conversation');
      const manager = Wire.text(item, 'manager');
      return (!conversation || conversation === this.conversationId) && (!manager || manager === this.owner);
    });
    this.mergeItems(incomingItems);
    const plans = Wire.object(result, 'plans');
    const related: string[] = [];
    incomingItems.forEach((item: Record<string, Object>) => {
      const ref = Wire.text(item, 'ref');
      if (Wire.text(item, 'kind') === 'plan' && ref && !related.includes(ref)) related.push(ref);
    });
    const filteredPlans: Record<string, Object> = {};
    Object.keys(plans).forEach((id: string) => {
      if (hasId(related, id) || hasId(Object.keys(this.plans), id)) filteredPlans[id] = plans[id];
    });
    this.mergePlans(filteredPlans);
    if (Wire.has(result, 'task_cards') && Array.isArray(result['task_cards'])) this.mergeTaskCards(Wire.rows(result, 'task_cards'));
    this.mergeViews(Wire.object(result, 'views'));
    this.mergeViewActions(Wire.object(result, 'view_actions'));
    this.mergeViewBlocks(Wire.strings(result, 'view_blocks'));
    this.pendingMessageIds = this.pendingMessageIds.filter((id: string) => {
      const item = this.items.find((row: Record<string, Object>) => Wire.text(row, 'id') === id);
      const body = Wire.object(item || {}, 'body');
      return !(Wire.text(body, 'sent_at') || Wire.text(body, 'filed_at') || Wire.text(body, 'dismissed_at'));
    });
  }

  private async readPage(run: number, cursor: string = '', since: string = ''): Promise<Record<string, Object> | undefined> {
    if (!this.current(run) || !this.repository) return undefined;
    const path = this.feedPath(cursor, since);
    const result = await this.repository.call(path);
    if (!this.current(run)) return undefined;
    this.mergePage(result);
    return result;
  }

  private latestSeenAt(): string {
    let newest = '';
    let newestTime = 0;
    this.items.forEach((item: Record<string, Object>) => {
      const body = Wire.object(item, 'body');
      const candidates = [Wire.text(item, 'at'), Wire.text(body, 'sent_at'), Wire.text(body, 'filed_at'),
        Wire.text(body, 'dismissed_at'), Wire.text(body, 'dismissal_updated_at')];
      candidates.forEach((value: string) => {
        const at = timestamp(value);
        if (at > newestTime) { newestTime = at; newest = value; }
      });
    });
    return newest;
  }

  async refresh(): Promise<void> {
    const run = this.generation;
    if (!this.current(run)) return;
    if (this.refreshPromise && this.refreshRun === run) return this.refreshPromise;
    this.loading = true; this.error = '';
    const work = this.refreshCurrent(run);
    this.refreshPromise = work; this.refreshRun = run;
    try { await work; }
    finally {
      if (this.refreshPromise === work) this.refreshPromise = undefined;
      if (this.generation === run) this.loading = false;
    }
  }

  private async refreshCurrent(run: number): Promise<void> {
    try {
      const initial = !this.hasLoaded;
      const since = initial ? '' : this.latestSeenAt();
      let result = await this.readPage(run, '', since);
      if (!result || !this.current(run)) return;
      if (initial) this.nextCursor = Wire.text(result, 'next_cursor');
      this.hasLoaded = true;
      let cursor = Wire.text(result, 'next_cursor');
      // A refresh window can be large; bound it and continue from its newest timestamp on the next refresh.
      for (let page = 1; cursor && page < 4; page++) {
        const before = cursor;
        result = await this.readPage(run, cursor);
        if (!result || !this.current(run)) return;
        cursor = Wire.text(result, 'next_cursor');
        // The next cursor is the boundary after the newest page actually read.
        // Keep it only for the initial, full-history window: a later since
        // refresh has a separate cursor and must not replace the older-history
        // boundary exposed by Load more.
        if (initial) this.nextCursor = cursor;
        if (cursor === before) break;
      }
      if (!this.current(run)) return;
      await this.refreshTodos(run);
    } catch (error) {
      if (this.current(run)) this.error = this.errorKey(error as Error);
    }
  }

  async loadMore(): Promise<boolean> {
    const run = this.generation, cursor = this.nextCursor;
    if (!cursor || !this.current(run) || this.loadingMore) return false;
    this.loadingMore = true; this.todoError = ''; this.error = '';
    try {
      const result = await this.readPage(run, cursor);
      if (!result || !this.current(run)) return false;
      const next = Wire.text(result, 'next_cursor');
      if (next === cursor) { this.error = 'request_updated'; return false; }
      this.nextCursor = next;
      await this.refreshTodos(run);
      return this.current(run);
    } catch (error) { if (this.current(run)) this.error = this.errorKey(error as Error); return false; }
    finally { if (this.generation === run) this.loadingMore = false; }
  }

  /** Reads the source feed from its head until the exact item is found or the authoritative history ends. */
  async locateItem(itemId: string, kind: string): Promise<string> {
    const run = this.generation;
    if (!itemId || !kind || !this.current(run) || !this.repository || !this.routeAdmitted()) {
      this.itemLookupStatus = 'error';
      return this.itemLookupStatus;
    }
    this.itemLookupStatus = 'loading';
    this.error = '';
    const seen = new Set<string>();
    let cursor = '';
    try {
      while (this.current(run) && this.repository && this.routeAdmitted()) {
        const result = await this.readPage(run, cursor);
        if (!result || !this.current(run) || !this.routeAdmitted()) return this.itemLookupStatus;
        this.hasLoaded = true;
        const pageItems = Wire.rows(result, 'items').filter((item: Record<string, Object>) => {
          const conversation = Wire.text(item, 'conversation');
          const manager = Wire.text(item, 'manager');
          return (!conversation || conversation === this.conversationId) && (!manager || manager === this.owner);
        });
        const found = pageItems.find((item: Record<string, Object>) => Wire.text(item, 'id') === itemId);
        if (found) {
          this.nextCursor = Wire.text(result, 'next_cursor');
          this.itemLookupStatus = Wire.text(found, 'kind') === kind ? 'found' : 'mismatch';
          if (this.itemLookupStatus === 'found' && kind === 'plan') {
            const planId = Wire.text(found, 'ref');
            if (planId) await this.reconcilePlan(planId, run);
          }
          await this.refreshTodos(run);
          return this.itemLookupStatus;
        }

        const next = Wire.text(result, 'next_cursor');
        if (!next) {
          this.nextCursor = '';
          this.itemLookupStatus = 'missing';
          await this.refreshTodos(run);
          return this.itemLookupStatus;
        }
        if (next === cursor || seen.has(next)) {
          this.itemLookupStatus = 'error';
          this.error = 'failed';
          return this.itemLookupStatus;
        }
        if (cursor) seen.add(cursor);
        cursor = next;
        this.nextCursor = next;
      }
    } catch (error) {
      if (this.current(run)) {
        this.error = this.errorKey(error as Error);
        this.itemLookupStatus = 'error';
      }
    }
    return this.itemLookupStatus;
  }

  private todoIds(): string[] {
    const ids: string[] = [];
    this.taskCards.forEach((card: Record<string, Object>) => { const id = Wire.text(card, 'todo_id'); if (id && !ids.includes(id)) ids.push(id); });
    Object.keys(this.plans).forEach((planId: string) => Wire.rows(Wire.record(this.plans[planId]), 'tasks').forEach((task: Record<string, Object>) => {
      const id = Wire.text(task, 'todo_id'); if (id && !ids.includes(id)) ids.push(id);
    }));
    this.items.forEach((item: Record<string, Object>) => {
      if (['task', 'receipt', 'report', 'review', 'answered', 'routed'].includes(Wire.text(item, 'kind'))) {
        const id = Wire.text(item, 'ref'); if (id && !ids.includes(id)) ids.push(id);
      }
    });
    return ids;
  }

  private async refreshTodos(run: number): Promise<void> {
    if (!this.current(run) || !this.repository) return;
    const ids = this.todoIds();
    if (!ids.length) { this.todos = []; this.todoError = ''; this.todosReady = true; return; }
    let path = '/api/v1/todos';
    if (this.owner) path += '?manager=' + segment(this.owner);
    this.todosReady = false;
    try {
      if (!this.current(run)) return;
      const result = await this.repository.call(path);
      if (!this.current(run)) return;
      this.todos = Wire.rows(result, 'todos').filter((todo: Record<string, Object>) => {
        const id = Wire.text(todo, 'id');
        const manager = Wire.text(todo, 'manager', this.owner);
        return ids.includes(id) && manager === this.owner && Wire.text(todo, 'origin') === this.conversationId;
      });
      this.todoError = '';
      this.todosReady = true;
    } catch (error) { if (this.current(run)) { this.todoError = this.errorKey(error as Error); this.todosReady = false; } }
  }

  private errorKey(error: Error): string {
    const data = error as Object as Record<string, Object>;
    if (data['resultUnknown'] === true) return 'result_unknown';
    const status = typeof data['status'] === 'number' ? data['status'] as number : 0;
    if (status === 401 || status === 403) return 'permission_denied';
    if (status === 404) return 'unsupported';
    if (status === 409 || status === 410) return 'request_updated';
    return error.message === 'offline' ? 'offline' : 'failed';
  }

  private plan(id: string): Record<string, Object> { return Wire.record(this.plans[id]); }
  private planActionPath(id: string, action: string): string {
    return '/api/v1/task-plans/' + segment(id) + '/' + action;
  }
  private blocked(path: string): boolean {
    if (hasId(this.uncertainKeys, path)) return true;
    return !!this.client && this.client.operationBlocked(path);
  }
  private markBusy(key: string, value: boolean): void { this.busyKeys = updated(this.busyKeys, key, value); }
  private markUncertain(path: string, value: boolean): void { this.uncertainKeys = updated(this.uncertainKeys, path, value); }
  private setPlan(id: string, plan: Record<string, Object>): void {
    if (!id || Wire.text(plan, 'id') !== id) return;
    const conversation = Wire.text(plan, 'conversation');
    if (conversation && conversation !== this.conversationId) return;
    const next: Record<string, Object> = {};
    Object.keys(this.plans).forEach((key: string) => next[key] = this.plans[key]);
    next[id] = plan; this.plans = next;
    this.freshPlanIds = updated(this.freshPlanIds, id, true);
  }

  planState(id: string): string { return Wire.text(this.plan(id), 'state', 'unknown'); }
  planIsFresh(id: string): boolean { return hasId(this.freshPlanIds, id); }
  planLoading(id: string): boolean { return hasId(this.loadingPlanIds, id); }
  private setPlanActionError(id: string, error: string): void {
    const next: Record<string, Object> = {};
    Object.keys(this.planActionErrors).filter((key: string) => key !== id || !!error)
      .forEach((key: string) => next[key] = this.planActionErrors[key]);
    if (error) next[id] = error;
    this.planActionErrors = next;
  }
  planActionError(id: string): string {
    const error = this.planActionErrors[id];
    return typeof error === 'string' ? error as string : '';
  }
  private planAllowsWrites(): boolean { return !!this.client && this.client.canApprove(); }
  planCanStart(id: string): boolean {
    const path = this.planActionPath(id, 'start');
    return this.current(this.generation) && this.routeAdmitted() && this.planAllowsWrites() && this.planIsFresh(id) && this.planState(id) === 'open' &&
      !hasId(this.busyKeys, 'plan:' + id) && !this.blocked(path);
  }
  planCanDismiss(id: string): boolean {
    const path = this.planActionPath(id, 'dismiss');
    return this.current(this.generation) && this.routeAdmitted() && this.planAllowsWrites() && this.planIsFresh(id) && this.planState(id) === 'open' &&
      !hasId(this.busyKeys, 'plan:' + id) && !this.blocked(path);
  }
  planCanEdit(id: string): boolean {
    const path = this.planActionPath(id, 'start');
    return this.current(this.generation) && this.routeAdmitted() && this.planAllowsWrites() && this.planState(id) === 'open' &&
      !hasId(this.busyKeys, 'plan:' + id) && !this.blocked(path);
  }
  planBusy(id: string): boolean { return hasId(this.busyKeys, 'plan:' + id); }
  rememberPlan(id: string): boolean { return hasId(this.rememberPlans, id); }
  toggleRememberPlan(id: string): void { this.rememberPlans = updated(this.rememberPlans, id, !this.rememberPlan(id)); }

  beginEditPlan(id: string): boolean {
    if (!this.planIsFresh(id) || this.planState(id) !== 'open' || !this.planCanStart(id)) return false;
    this.editingPlanId = id;
    this.editingPlanVersion = JSON.stringify(this.plan(id));
    this.editTasks = Wire.rows(this.plan(id), 'tasks').map((task: Record<string, Object>) => new PlanEditTask(task));
    this.editing = true;
    return true;
  }

  updateEditTask(index: number, key: string, value: string): void {
    if (!this.editing || index < 0 || index >= this.editTasks.length || !['title', 'prompt', 'machine', 'cwd', 'permission'].includes(key)) return;
    const current = this.editTasks[index];
    if (key === 'permission') {
      if (current.readonly) current.permission = 'plan';
      else if (value === 'bypassPermissions') current.permission = value;
      else return;
    } else if (key === 'title') current.title = value;
    else if (key === 'prompt') current.prompt = value;
    else if (key === 'machine') current.machine = value;
    else if (key === 'cwd') current.cwd = value;
    this.editTasks = this.editTasks.slice();
  }

  private editPayload(): Record<string, Object>[] {
    const original = Wire.rows(this.plan(this.editingPlanId), 'tasks');
    return this.editTasks.map((edited: PlanEditTask, index: number) => {
      const before = original[index] || {};
      const patch: Record<string, Object> = {};
      ['title', 'prompt', 'machine', 'cwd'].forEach((key: string) => {
        const value = edited.value(key).trim();
        if (value && value !== Wire.text(before, key)) patch[key] = value;
      });
      if (!edited.readonly && edited.permission === 'bypassPermissions' && edited.permission !== Wire.text(before, 'permission', 'default')) patch['permission'] = edited.permission;
      return patch;
    });
  }

  cancelPlanEdit(): void { this.editing = false; this.editingPlanId = ''; this.editingPlanVersion = ''; this.editTasks = []; }

  async startEditedPlan(): Promise<boolean> {
    const id = this.editingPlanId;
    if (!id || this.editTasks.some((task: PlanEditTask) => !task.title.trim() || !task.prompt.trim() || !task.machine.trim() || !task.cwd.trim())) {
      this.error = 'required'; return false;
    }
    if (this.editTasks.some((task: PlanEditTask) => !task.readonly && task.permission !== 'bypassPermissions')) {
      this.error = 'assistant_feed_permission_required'; return false;
    }
    const started = await this.startPlan(id, this.rememberPlan(id), this.editPayload(), this.editingPlanVersion);
    if (started) this.cancelPlanEdit();
    return started;
  }

  async startPlan(id: string, remember: boolean = false, edits?: Record<string, Object>[], expectedVersion: string = ''): Promise<boolean> {
    const run = this.generation, path = this.planActionPath(id, 'start');
    const repository = this.repository;
    const route = this.routeSnapshot();
    if (!repository || !this.current(run) || !this.routeIsCurrent(route)) {
      if (this.blocked(path)) this.error = 'result_unknown';
      return false;
    }
    const displayedVersion = expectedVersion || JSON.stringify(this.plan(id));
    if (!displayedVersion || displayedVersion === '{}') {
      this.error = 'request_updated';
      this.setPlanActionError(id, this.error);
      return false;
    }
    await this.reconcilePlan(id, run);
    if (!this.current(run) || !this.routeIsCurrent(route)) return false;
    if (!this.planIsFresh(id)) { if (!this.error) this.error = 'failed'; return false; }
    const planVersion = JSON.stringify(this.plan(id));
    if (displayedVersion !== planVersion) {
      this.error = 'request_updated';
      this.setPlanActionError(id, this.error);
      return false;
    }
    if (!this.planCanStart(id)) {
      if (this.blocked(path)) this.error = 'result_unknown';
      else if (!this.planAllowsWrites()) this.error = 'permission_denied';
      if (this.error) this.setPlanActionError(id, this.error);
      return false;
    }
    const body = Wire.payload(['remember'], [remember]);
    if (edits) body['tasks'] = edits;
    const admit = (): boolean => this.current(run) && this.repository === repository && this.routeIsCurrent(route) &&
      this.planAllowsWrites() && this.planState(id) === 'open' && JSON.stringify(this.plan(id)) === planVersion;
    this.error = ''; this.setPlanActionError(id, ''); this.markBusy('plan:' + id, true);
    try {
      if (!admit()) {
        if (!this.planAllowsWrites()) { this.error = 'permission_denied'; this.setPlanActionError(id, this.error); }
        else if (this.current(run) && this.routeIsCurrent(route)) { this.error = 'request_updated'; this.setPlanActionError(id, this.error); }
        return false;
      }
      const response = await repository.call(path, 'POST', body, '', admit);
      if (!this.current(run)) return false;
      const started = Wire.object(response, 'plan');
      if (Wire.text(started, 'state') !== 'started' || Wire.text(started, 'id') !== id) {
        this.markUncertain(path, true); this.error = 'result_unknown'; this.setPlanActionError(id, this.error);
        await this.reconcilePlan(id, run);
        await this.refresh();
        return false;
      }
      this.setPlan(id, started); this.markUncertain(path, false);
      await this.refresh();
      if (!this.current(run)) return false;
      return true;
    } catch (error) {
      if (!this.current(run)) return false;
      this.error = this.errorKey(error as Error);
      this.setPlanActionError(id, this.error);
      if (this.error === 'result_unknown') {
        this.markUncertain(path, true);
        await this.reconcilePlan(id, run);
      } else if (this.error === 'request_updated' || this.error === 'unsupported') {
        await this.reconcilePlan(id, run);
      }
      if (this.current(run) && (this.error === 'result_unknown' || this.error === 'request_updated' || this.error === 'unsupported')) {
        // A competing device may have started the plan before this response was
        // lost or rejected. Read its linked Todos before presenting a retry path.
        await this.refreshTodos(run);
      }
      return false;
    } finally { if (this.generation === run) this.markBusy('plan:' + id, false); }
  }

  async dismissPlan(id: string, expectedVersion: string = ''): Promise<boolean> {
    const run = this.generation, path = this.planActionPath(id, 'dismiss'), repository = this.repository;
    const route = this.routeSnapshot();
    if (!repository || !this.current(run) || !this.routeIsCurrent(route)) {
      if (this.blocked(path)) this.error = 'result_unknown';
      return false;
    }
    const displayedVersion = expectedVersion || JSON.stringify(this.plan(id));
    if (!displayedVersion || displayedVersion === '{}') {
      this.error = 'request_updated';
      this.setPlanActionError(id, this.error);
      return false;
    }
    await this.reconcilePlan(id, run);
    if (!this.current(run) || !this.routeIsCurrent(route)) return false;
    if (!this.planIsFresh(id)) { if (!this.error) this.error = 'failed'; return false; }
    const planVersion = JSON.stringify(this.plan(id));
    if (displayedVersion !== planVersion) {
      this.error = 'request_updated';
      this.setPlanActionError(id, this.error);
      return false;
    }
    if (!this.planCanDismiss(id)) {
      if (this.blocked(path)) this.error = 'result_unknown';
      else if (!this.planAllowsWrites()) this.error = 'permission_denied';
      if (this.error) this.setPlanActionError(id, this.error);
      return false;
    }
    const admit = (): boolean => this.current(run) && this.repository === repository && this.routeIsCurrent(route) &&
      this.planAllowsWrites() && this.planState(id) === 'open' && JSON.stringify(this.plan(id)) === planVersion;
    this.error = ''; this.setPlanActionError(id, ''); this.markBusy('plan:' + id, true);
    try {
      if (!admit()) {
        if (!this.planAllowsWrites()) { this.error = 'permission_denied'; this.setPlanActionError(id, this.error); }
        else if (this.current(run) && this.routeIsCurrent(route)) { this.error = 'request_updated'; this.setPlanActionError(id, this.error); }
        return false;
      }
      const response = await repository.call(path, 'POST', Wire.payload([], []), '', admit);
      if (!this.current(run)) return false;
      const dismissed = Wire.object(response, 'plan');
      if (Wire.text(dismissed, 'state') !== 'dismissed' || Wire.text(dismissed, 'id') !== id) {
        this.markUncertain(path, true); this.error = 'result_unknown'; this.setPlanActionError(id, this.error);
        await this.reconcilePlan(id, run); return false;
      }
      this.setPlan(id, dismissed); this.markUncertain(path, false);
      await this.refresh(); return this.current(run);
    } catch (error) {
      if (!this.current(run)) return false;
      this.error = this.errorKey(error as Error);
      this.setPlanActionError(id, this.error);
      if (this.error === 'result_unknown') this.markUncertain(path, true);
      if (this.error === 'result_unknown' || this.error === 'request_updated' || this.error === 'unsupported') await this.reconcilePlan(id, run);
      return false;
    } finally { if (this.generation === run) this.markBusy('plan:' + id, false); }
  }

  private async reconcilePlan(id: string, run: number): Promise<void> {
    this.freshPlanIds = updated(this.freshPlanIds, id, false);
    if (!this.current(run) || !this.repository || !this.routeAdmitted()) return;
    const repository = this.repository;
    const path = this.ownerPath('/api/v1/task-plans/' + segment(id));
    this.loadingPlanIds = updated(this.loadingPlanIds, id, true);
    try {
      const result = await repository.call(path);
      if (!this.current(run) || this.repository !== repository || !this.routeAdmitted()) return;
      const plan = Wire.object(result, 'plan');
      if (Wire.text(plan, 'id') !== id) {
        this.error = 'request_updated';
        this.setPlanActionError(id, this.error);
        return;
      }
      this.setPlan(id, plan);
      const state = Wire.text(plan, 'state');
      const actionPath = this.planActionPath(id, 'start');
      const dismissPath = this.planActionPath(id, 'dismiss');
      if (state === 'started' || state === 'dismissed' || state === 'open' && !this.client?.operationBlocked(actionPath) && !this.client?.operationBlocked(dismissPath)) {
        this.markUncertain(actionPath, false); this.markUncertain(dismissPath, false);
      }
    } catch (error) {
      if (this.current(run)) {
        this.error = this.errorKey(error as Error);
        this.setPlanActionError(id, this.error);
      }
      // Keep the feed snapshot readable, but do not admit writes from a failed current-plan read.
    } finally {
      if (this.generation === run) this.loadingPlanIds = updated(this.loadingPlanIds, id, false);
    }
  }

  async editPlan(id: string): Promise<boolean> {
    const run = this.generation, route = this.routeSnapshot();
    const permission = this.planAllowsWrites();
    if (!permission) {
      this.error = 'permission_denied';
      this.setPlanActionError(id, this.error);
      return false;
    }
    if (!await this.refreshPlan(id) || !this.current(run) || !this.routeIsCurrent(route)) return false;
    if (!this.planAllowsWrites()) {
      this.error = 'permission_denied';
      this.setPlanActionError(id, this.error);
      return false;
    }
    return this.beginEditPlan(id);
  }

  async refreshPlan(id: string): Promise<boolean> {
    if (!id || !this.current(this.generation)) return false;
    const run = this.generation;
    this.error = '';
    this.setPlanActionError(id, '');
    await this.reconcilePlan(id, run);
    return this.current(run) && this.planIsFresh(id);
  }

  /** Returns the same lead Agent order used by the task receipt on iOS. */
  receiptLeadAgent(todoId: string): Record<string, Object> {
    const todo = this.todo(todoId);
    let selected: Record<string, Object> = {};
    let bestPriority = Number.MAX_SAFE_INTEGER;
    let bestUpdated = 0;
    Wire.rows(todo, 'agents').filter((agent: Record<string, Object>) => !Wire.text(agent, 'released_at') && !Wire.text(agent, 'dismissed_at'))
      .forEach((agent: Record<string, Object>) => {
        const machineId = Wire.text(agent, 'machine_id'), sessionId = Wire.text(agent, 'session_id');
        if (!machineId || !sessionId) return;
        const listed = this.client?.sessions.find((candidate: ResourceRow) => candidate.manager === this.owner &&
          candidate.machine === machineId && (candidate.session || candidate.id) === sessionId);
        if (!listed) return;
        const data = listed.data();
        const state = listed.state || Wire.text(data, 'state', Wire.text(data, 'status'));
        if (!Wire.text(data, 'state') && !Wire.text(data, 'status') && listed.state) data['state'] = listed.state;
        const priority = state === 'needs_you' ? 0 : state === 'unread' ? 1 : state === 'working' ? 2 :
          ['waiting', 'starting', 'not_started'].includes(state) ? 3 : state === 'idle' ? 4 : state === 'unknown' ? 5 : 6;
        const parsedUpdated = Date.parse(Wire.text(data, 'updated_at', Wire.text(agent, 'updated_at')));
        const updatedAt = Number.isFinite(parsedUpdated) ? parsedUpdated : 0;
        if (priority < bestPriority || priority === bestPriority && updatedAt > bestUpdated) {
          bestPriority = priority;
          bestUpdated = updatedAt;
          selected = data;
        }
      });
    return selected;
  }

  private receiptForRecovery(receiptId: string, todoId: string): Record<string, Object> {
    if (!receiptId || !todoId) return {};
    const item = this.items.find((candidate: Record<string, Object>) => Wire.text(candidate, 'id') === receiptId &&
      Wire.text(candidate, 'kind') === 'receipt' && Wire.text(candidate, 'ref') === todoId &&
      Wire.text(candidate, 'conversation', this.conversationId) === this.conversationId &&
      Wire.text(candidate, 'manager', this.owner) === this.owner);
    if (!item) return {};
    if (!this.receiptRecoveryResolution(item)) {
      const at = timestamp(Wire.text(item, 'at'));
      if (this.items.some((other: Record<string, Object>) => Wire.text(other, 'id') !== receiptId && Wire.text(other, 'kind') === 'receipt' && Wire.text(other, 'ref') === todoId && timestamp(Wire.text(other, 'at')) >= at)) return {};
      if (Wire.rows(this.todo(todoId), 'agents').some((agent: Record<string, Object>) => !Wire.text(agent, 'released_at') && !Wire.text(agent, 'dismissed_at') && timestamp(Wire.text(agent, 'linked_at')) > at)) return {};
    }
    return item;
  }

  private receiptRecoveryResolution(item: Record<string, Object>): string {
    return Wire.text(Wire.object(item, 'body'), 'resolution');
  }

  private receiptRecoveryState(item: Record<string, Object>): string {
    return receiptWireState(item);
  }

  private receiptRecoverySnapshot(item: Record<string, Object>): string {
    return JSON.stringify({
      id: Wire.text(item, 'id'), kind: Wire.text(item, 'kind'), ref: Wire.text(item, 'ref'),
      conversation: Wire.text(item, 'conversation', this.conversationId), manager: Wire.text(item, 'manager', this.owner),
      state: this.receiptRecoveryState(item), resolution: this.receiptRecoveryResolution(item)
    });
  }

  private receiptPlanFailure(todo: Record<string, Object>): string {
    const planId = Wire.text(todo, 'plan_id');
    const plan = this.plan(planId);
    const todoId = Wire.text(todo, 'id');
    const task = Wire.rows(plan, 'tasks').find((candidate: Record<string, Object>) => Wire.text(candidate, 'todo_id') === todoId);
    return Wire.text(todo, 'start_error') || Wire.text(task || {}, 'error');
  }

  canRetryReceipt(receiptId: string, todoId: string, allowBusy: boolean = false): boolean {
    const item = this.receiptForRecovery(receiptId, todoId), todo = this.todo(todoId);
    const path = this.ownerPath('/api/v1/todos/' + segment(todoId) + '/retry');
    return this.current(this.generation) && this.routeAdmitted() && this.planAllowsWrites() && !!todoId &&
      this.receiptRecoveryState(item) === 'failed' && Object.keys(todo).length > 0 &&
      Wire.text(todo, 'manager', this.owner) === this.owner && Wire.text(todo, 'origin') === this.conversationId &&
      !!Wire.text(todo, 'plan_id') && !!Wire.text(todo, 'done_at') && !!Wire.text(todo, 'start_error') &&
      (allowBusy || !hasId(this.busyKeys, 'todo:' + todoId)) && (allowBusy || !this.blocked(path));
  }

  canContinueReceipt(receiptId: string, todoId: string, allowBusy: boolean = false): boolean {
    const item = this.receiptForRecovery(receiptId, todoId), resolution = this.receiptRecoveryResolution(item), todo = this.todo(todoId);
    const path = this.ownerPath('/api/v1/todos/' + segment(todoId) + '/retry');
    if (!this.current(this.generation) || !this.routeAdmitted() || !this.planAllowsWrites() || !todoId ||
      this.receiptRecoveryState(item) !== 'interrupted' || !['', 'pending'].includes(resolution) ||
      !Object.keys(todo).length || Wire.text(todo, 'manager', this.owner) !== this.owner ||
      Wire.text(todo, 'origin') !== this.conversationId || !Wire.text(todo, 'plan_id') || Wire.text(todo, 'done_at') ||
      !allowBusy && (hasId(this.busyKeys, 'todo:' + todoId) || this.blocked(path))) return false;
    const lead = this.receiptLeadAgent(todoId), state = Wire.text(lead, 'state', Wire.text(lead, 'status'));
    const error = Wire.text(lead, 'turn_error');
    if (!error || !['idle', 'unread'].includes(state)) return false;
    const work = Wire.object(lead, 'work');
    const autoContinues = Wire.bool(lead, 'turn_error_resumes') ||
      Wire.text(work, 'phase') === 'blocked' && !!Wire.text(work, 'resets_at') && !!Wire.text(work, 'wake_at');
    const resetAtValue = Wire.text(lead, 'turn_error_resets_at') || Wire.text(work, 'resets_at');
    const resetAt = Date.parse(resetAtValue);
    const usageLimited = Wire.text(lead, 'turn_error_kind') === 'usage_limit' && !!error &&
      ['idle', 'unread'].includes(state) && (!Number.isFinite(resetAt) || Date.now() <= resetAt + 10 * 60 * 1000);
    // Match the receipt model: an automatic usage-limit continuation blocks a manual action only while the
    // limit is active. Expired limits are interrupted again; an unknown reset time remains limited.
    return !(usageLimited && autoContinues);
  }

  async retryReceipt(receiptId: string, todoId: string, action: string): Promise<boolean> {
    if (action !== 'retry' && action !== 'continue') return false;
    if (action === 'retry' ? !this.canRetryReceipt(receiptId, todoId) : !this.canContinueReceipt(receiptId, todoId)) return false;
    const run = this.generation, repository = this.repository;
    const route = this.routeSnapshot();
    if (!repository || !this.current(run) || !this.routeIsCurrent(route)) return false;
    const clickedReceipt = this.receiptForRecovery(receiptId, todoId);
    const clickedReceiptSnapshot = this.receiptRecoverySnapshot(clickedReceipt);
    this.error = '';
    // Re-read the clicked card from the authoritative feed before preparing its write. This makes the Hub's
    // resolution (especially `resumed`) part of the user's action even if the view was left open while it changed.
    const located = await this.locateItem(receiptId, 'receipt');
    if (!this.current(run) || this.repository !== repository || !this.routeIsCurrent(route)) return false;
    const freshReceipt = this.receiptForRecovery(receiptId, todoId);
    if (located !== 'found' || this.receiptRecoverySnapshot(freshReceipt) !== clickedReceiptSnapshot) {
      this.error = 'request_updated';
      return false;
    }
    if (!this.todosReady) {
      this.error = this.todoError || 'failed';
      return false;
    }
    if (action === 'retry' ? !this.canRetryReceipt(receiptId, todoId) : !this.canContinueReceipt(receiptId, todoId)) {
      this.error = 'request_updated';
      return false;
    }
    const path = this.ownerPath('/api/v1/todos/' + segment(todoId) + '/retry');
    const todoSnapshot = JSON.stringify(this.todo(todoId));
    const receiptSnapshot = this.receiptRecoverySnapshot(freshReceipt);
    const resolution = this.receiptRecoveryResolution(freshReceipt);
    // Busy and the blocked journal row are expected while this write is being journaled. The initial gate above
    // checks for pre-existing rows; this callback checks the captured receipt and todo after journaling.
    const canRecover = (): boolean => action === 'retry' ? this.canRetryReceipt(receiptId, todoId, true) : this.canContinueReceipt(receiptId, todoId, true);
    const admit = (): boolean => {
      const currentReceipt = this.receiptForRecovery(receiptId, todoId);
      return this.current(run) && this.repository === repository && this.routeIsCurrent(route) &&
        this.receiptRecoverySnapshot(currentReceipt) === receiptSnapshot && this.receiptRecoveryResolution(currentReceipt) === resolution &&
        JSON.stringify(this.todo(todoId)) === todoSnapshot && canRecover();
    };
    this.error = '';
    this.markBusy('todo:' + todoId, true);
    const conversation = this.conversationId, owner = this.owner;
    const acknowledged = (response: Record<string, Object>): boolean => {
      const todo = Wire.object(response, 'todo');
      return Wire.text(todo, 'id') === todoId && Wire.text(todo, 'origin') === conversation &&
        (!Wire.has(todo, 'manager') || typeof todo['manager'] === 'string' && todo['manager'] === owner);
    };
    try {
      if (!admit()) return false;
      const response = await repository.call(path, 'POST', Wire.payload([], []), '', admit, false, acknowledged);
      if (!this.current(run)) return false;
      if (!acknowledged(response)) {
        this.markUncertain(path, true);
        await this.locateItem(receiptId, 'receipt');
        if (this.current(run)) this.error = 'result_unknown';
        return false;
      }
      this.markUncertain(path, false);
      await this.refresh();
      return this.current(run);
    } catch (error) {
      if (!this.current(run)) return false;
      this.error = this.errorKey(error as Error);
      if (this.error === 'result_unknown') {
        this.markUncertain(path, true);
        await this.locateItem(receiptId, 'receipt');
        if (this.current(run)) this.error = 'result_unknown';
      }
      if (this.error === 'request_updated' || this.error === 'unsupported') await this.refreshTodos(run);
      return false;
    } finally {
      if (this.generation === run) this.markBusy('todo:' + todoId, false);
    }
  }

  setMessageDraft(id: string, value: string): void {
    const next: Record<string, Object> = {};
    Object.keys(this.messageDrafts).forEach((key: string) => next[key] = this.messageDrafts[key]);
    next[id] = value; this.messageDrafts = next;
  }
  messageDraft(item: Record<string, Object>): string {
    const id = Wire.text(item, 'id');
    const custom = this.messageDrafts[id];
    return typeof custom === 'string' ? custom as string : Wire.text(Wire.object(item, 'body'), 'draft');
  }
  messageState(item: Record<string, Object>): string {
    const body = Wire.object(item, 'body');
    if (Wire.text(body, 'sent_at')) return 'sent';
    if (Wire.text(body, 'filed_at')) return 'filed';
    if (Wire.text(body, 'dismissed_at')) return 'dismissed';
    if (hasId(this.pendingMessageIds, Wire.text(item, 'id'))) return 'pending';
    return 'open';
  }
  messageCanSend(item: Record<string, Object>): boolean {
    const id = Wire.text(item, 'id'), body = Wire.object(item, 'body'), ref = Wire.text(item, 'ref');
    const path = this.ownerPath('/api/v1/inbox/' + segment(ref) + '/reply');
    const draft = this.messageDraft(item);
    return this.current(this.generation) && Wire.text(item, 'kind') === 'message' && !!id && !!ref &&
      this.messageState(item) === 'open' && draft.trim().length > 0 && !draftPlaceholder(draft) && !this.blocked(path) && !hasId(this.busyKeys, 'message:' + id) &&
      Wire.has(body, 'draft');
  }
  async sendMessage(id: string, text: string): Promise<boolean> {
    const run = this.generation, item = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
    if (!item || !this.messageCanSend(item) || text !== this.messageDraft(item)) return false;
    const ref = Wire.text(item, 'ref');
    const path = '/api/v1/inbox/' + segment(ref) + '/reply' + (this.owner ? '?manager=' + segment(this.owner) : '');
    if (this.blocked(path)) { this.error = 'result_unknown'; return false; }
    const repository = this.repository;
    if (!repository || !this.current(run)) return false;
    const route = this.routeSnapshot();
    const admit = (): boolean => {
      const currentItem = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
      return this.current(run) && this.repository === repository && this.routeIsCurrent(route) && !!currentItem &&
        this.messageState(currentItem) === 'open' && this.messageDraft(currentItem) === text &&
        Wire.text(currentItem, 'ref') === ref;
    };
    this.markBusy('message:' + id, true); this.error = '';
    try {
      if (!admit()) return false;
      await repository.call(path, 'POST', Wire.payload(['text', 'card'], [text, id]), '', admit);
      if (!this.current(run)) return false;
      this.pendingMessageIds = updated(this.pendingMessageIds, id, true);
      await this.refresh();
      return this.current(run);
    } catch (error) {
      if (!this.current(run)) return false;
      this.error = this.errorKey(error as Error);
      if (this.error === 'result_unknown') this.pendingMessageIds = updated(this.pendingMessageIds, id, true);
      return false;
    } finally { if (this.generation === run) this.markBusy('message:' + id, false); }
  }

  messageCanDismiss(item: Record<string, Object>): boolean {
    const id = Wire.text(item, 'id'), body = Wire.object(item, 'body');
    const path = this.ownerPath('/api/v1/assistant/feed/' + segment(id) + '/dismiss');
    return this.current(this.generation) && Wire.text(item, 'kind') === 'message' && this.messageState(item) === 'open' &&
      !!Wire.text(body, 'draft') && !this.blocked(path) && !hasId(this.busyKeys, 'message:' + id);
  }
  async dismissMessage(id: string): Promise<boolean> {
    const run = this.generation, item = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
    if (!item || !this.messageCanDismiss(item) || !this.repository) return false;
    const repository = this.repository, route = this.routeSnapshot();
    const path = '/api/v1/assistant/feed/' + segment(id) + '/dismiss' + (this.owner ? '?manager=' + segment(this.owner) : '');
    const draft = Wire.text(Wire.object(item, 'body'), 'draft');
    const admit = (): boolean => {
      const currentItem = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
      return this.current(run) && this.repository === repository && this.routeIsCurrent(route) && !!currentItem &&
        this.messageState(currentItem) === 'open' && Wire.text(Wire.object(currentItem, 'body'), 'draft') === draft;
    };
    this.markBusy('message:' + id, true); this.error = '';
    try {
      if (!admit()) return false;
      const response = await repository.call(path, 'POST', Wire.payload(['draft'], [draft]), '', admit);
      if (!this.current(run)) return false;
      const updated = Wire.object(response, 'item');
      if (Wire.text(updated, 'id') !== id || Wire.text(updated, 'conversation') !== this.conversationId) { this.error = 'request_updated'; return false; }
      this.mergeItems([updated]); return true;
    } catch (error) {
      if (this.current(run)) {
        this.error = this.errorKey(error as Error);
        if (this.error === 'result_unknown') this.pendingMessageIds = updated(this.pendingMessageIds, id, true);
      }
      return false;
    }
    finally { if (this.generation === run) this.markBusy('message:' + id, false); }
  }
  messageCanUndo(item: Record<string, Object>): boolean {
    const id = Wire.text(item, 'id'), body = Wire.object(item, 'body'), token = Wire.text(body, 'dismissal_id');
    const path = this.ownerPath('/api/v1/assistant/feed/' + segment(id) + '/undo-dismiss');
    return this.current(this.generation) && Wire.text(item, 'kind') === 'message' && !!token && this.messageState(item) === 'dismissed' &&
      !this.blocked(path) && !hasId(this.busyKeys, 'message:' + id);
  }
  async undoDismissMessage(id: string): Promise<boolean> {
    const run = this.generation, item = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
    if (!item || !this.messageCanUndo(item) || !this.repository) return false;
    const repository = this.repository, route = this.routeSnapshot();
    const token = Wire.text(Wire.object(item, 'body'), 'dismissal_id');
    const path = '/api/v1/assistant/feed/' + segment(id) + '/undo-dismiss' + (this.owner ? '?manager=' + segment(this.owner) : '');
    const admit = (): boolean => {
      const currentItem = this.items.find((value: Record<string, Object>) => Wire.text(value, 'id') === id);
      return this.current(run) && this.repository === repository && this.routeIsCurrent(route) && !!currentItem &&
        this.messageState(currentItem) === 'dismissed' && Wire.text(Wire.object(currentItem, 'body'), 'dismissal_id') === token;
    };
    this.markBusy('message:' + id, true); this.error = '';
    try {
      if (!admit()) return false;
      const response = await repository.call(path, 'POST', Wire.payload(['dismissal_id'], [token]), '', admit);
      if (!this.current(run)) return false;
      const updated = Wire.object(response, 'item');
      if (Wire.text(updated, 'id') !== id || Wire.text(updated, 'conversation') !== this.conversationId) { this.error = 'request_updated'; return false; }
      this.mergeItems([updated]); return true;
    } catch (error) { if (this.current(run)) this.error = this.errorKey(error as Error); return false; }
    finally { if (this.generation === run) this.markBusy('message:' + id, false); }
  }

  todo(todoId: string): Record<string, Object> { return this.todos.find((value: Record<string, Object>) => Wire.text(value, 'id') === todoId) || {}; }
  taskCardsForConversation(): Record<string, Object>[] {
    const rows: Record<string, Object>[] = [];
    const seen: string[] = [];
    this.taskCards.forEach((card: Record<string, Object>) => {
      const id = Wire.text(card, 'todo_id');
      if (id && this.todo(id) && !seen.includes(id)) { rows.push(card); seen.push(id); }
    });
    // Legacy servers may omit task_cards; feed refs and started plan todo ids still identify real tasks.
    this.items.forEach((item: Record<string, Object>) => {
      const kind = Wire.text(item, 'kind');
      if (['task', 'receipt', 'report', 'review'].includes(kind)) {
        const id = Wire.text(item, 'ref');
        if (id && this.todo(id) && !seen.includes(id)) {
          rows.push(Wire.payload(['id', 'todo_id', 'state'], ['todo:' + id, id, 'unknown'])); seen.push(id);
        }
      }
    });
    Object.keys(this.plans).forEach((planId: string) => {
      const plan = Wire.record(this.plans[planId]);
      Wire.rows(plan, 'tasks').forEach((task: Record<string, Object>, index: number) => {
        const id = Wire.text(task, 'todo_id');
        if (id && this.todo(id) && !seen.includes(id)) {
          rows.push(Wire.payload(['id', 'plan_id', 'index', 'todo_id', 'at', 'updated_at', 'state'],
            ['plan:' + planId + ':' + String(index), planId, index, id, Wire.text(plan, 'created_at'), Wire.text(plan, 'updated_at'), 'unknown']));
          seen.push(id);
        }
      });
    });
    return rows.sort((left: Record<string, Object>, right: Record<string, Object>) => timestamp(Wire.text(left, 'at')) - timestamp(Wire.text(right, 'at')));
  }

  openTodo(id: string): ResourceRow | undefined {
    if (!this.current(this.generation)) return undefined;
    const todo = this.todo(id);
    if (!Object.keys(todo).length || Wire.text(todo, 'origin') !== this.conversationId || Wire.text(todo, 'manager', this.owner) !== this.owner) return undefined;
    const row = ResourceRow.from(todo, 'todo'); row.manager = this.owner; return row;
  }

  openAgent(todoId: string, agent: Record<string, Object>): ResourceRow | undefined {
    if (!this.current(this.generation) || Wire.text(this.todo(todoId), 'origin') !== this.conversationId) return undefined;
    const machine = Wire.text(agent, 'machine_id'), session = Wire.text(agent, 'session_id');
    if (!machine || !session) return undefined;
    const listed = this.client?.sessions.find((row: ResourceRow) => row.manager === this.owner && row.machine === machine && (row.session || row.id) === session);
    if (listed) return listed;
    const row = new ResourceRow(session, Wire.text(agent, 'title', Wire.text(agent, 'agent', session)), 'session');
    row.machine = machine; row.session = session; row.manager = this.owner; row.path = Wire.text(agent, 'cwd');
    row.raw = JSON.stringify(agent); return row;
  }
}
