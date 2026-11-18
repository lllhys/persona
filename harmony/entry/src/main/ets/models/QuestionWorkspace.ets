import type { ClientModel } from './ClientModel';
import type { HubRepository } from './HubRepository';
import { Decision } from './Wire';

/** An immutable request identity owns a retained, local-only question draft. */
export class QuestionDraft {
  readonly token: string;
  readonly id: string;
  readonly raw: string;
  text: Record<string, string> = {};
  picks: Record<string, string[]> = {};
  private editing: string = '';
  begin(session: string): void { this.editing = session; }
  owns(session: string): boolean { return this.editing === session; }
  constructor(token: string, request: Decision) { this.token = token; this.id = request.id; this.raw = request.raw; }
  save(text: Record<string, string>, picks: Record<string, string[]>, session: string): void {
    if (!this.owns(session)) return;
    const next: Record<string, string> = {}, selected: Record<string, string[]> = {};
    Object.keys(text).forEach((key: string) => next[key] = text[key]);
    Object.keys(picks).forEach((key: string) => selected[key] = picks[key].slice());
    this.text = next; this.picks = selected;
  }
}
export class QuestionSheet {
  readonly id: string; readonly request: Decision; readonly draft: QuestionDraft;
  constructor(id: string, request: Decision, draft: QuestionDraft) { this.id = id; this.request = request; this.draft = draft; }
}
let questionSheetSequence: number = 0;
let questionDraftSequence: number = 0;
class QuestionContext {
  readonly drafts: Map<string, QuestionDraft> = new Map();
  sync(requests: Decision[]): void {
    const ids = requests.map((request: Decision) => request.id);
    Array.from(this.drafts.keys()).forEach((id: string) => { if (!ids.includes(id)) this.drafts.delete(id); });
    requests.forEach((request: Decision) => {
      if (this.drafts.get(request.id)?.raw !== request.raw) this.drafts.set(request.id, new QuestionDraft(String(++questionDraftSequence), request));
    });
  }
}
class QuestionContexts {
  readonly ownerKey: string;
  readonly context: QuestionContext = new QuestionContext();
  constructor(key: string) { this.ownerKey = key; }
}
const retained: WeakMap<ClientModel, QuestionContexts> = new WeakMap();

export function questionPrompt(request: Decision): string { return request.prompt || request.fields[0]?.title || request.fields[0]?.key || ''; }

/** A conversation presents one native answer sheet, independently of its ordinary composer. */
@ObservedV2
export class QuestionWorkspace {
  @Trace shown: boolean = false;
  @Trace sheet: QuestionSheet | undefined;
  private owner: ClientModel | undefined;
  private repository: HubRepository | undefined;
  private generation: number = 0;
  private scope: string = '';
  private target: string = '';
  private active: boolean = false;
  private context: QuestionContext | undefined;
  private presented: QuestionSheet | undefined;

  sync(model: ClientModel, active: boolean): void {
    this.active = active;
    if (!active || !model.connected || model.selected.kind !== 'conversation') { this.shown = false; return; }
    const requests = model.personaQuestions.filter((request: Decision) => request.kind === 'question' && ['open', 'pending', 'unknown'].includes(request.status));
    if (!requests.length) { retained.delete(model); this.context = undefined; this.shown = false; return; }
    const ownerKey = JSON.stringify([model.scope(), model.connectionGeneration(), model.target()]);
    let contexts = retained.get(model);
    if (!contexts || contexts.ownerKey !== ownerKey) { contexts = new QuestionContexts(ownerKey); retained.set(model, contexts); }
    const context = contexts.context;
    context.sync(requests);
    if (this.shown && (this.owner !== model || this.context !== context || !this.current(this.sheet?.draft))) this.shown = false;
    this.context = context;
  }
  open(model: ClientModel, active: boolean, expected: Decision): boolean {
    if (this.shown || this.presented) return false;
    this.sync(model, active);
    if (!active || !model.connected || model.selected.kind !== 'conversation' || !model.canApprove()) return false;
    const request = model.personaQuestions.find((item: Decision) => item.id === expected.id && item.raw === expected.raw && item.machine === expected.machine && item.session === expected.session);
    const draft = this.context?.drafts.get(expected.id);
    if (!request || request.kind !== 'question' || request.status !== 'open' || !draft || draft.raw !== request.raw) return false;
    this.owner = model; this.repository = model.repo(); this.generation = model.connectionGeneration(); this.scope = model.scope(); this.target = model.target();
    this.sheet = new QuestionSheet(String(++questionSheetSequence), request, draft); draft.begin(this.sheet.id); this.shown = true; return true;
  }
  private current(draft: QuestionDraft | undefined): boolean {
    const owner = this.owner;
    if (!draft || !this.sheet || this.sheet.draft !== draft || !draft.owns(this.sheet.id) || !owner || !this.active || !owner.connected || !owner.owns(this.generation, this.scope) || owner.target() !== this.target || this.context?.drafts.get(draft.id) !== draft) return false;
    try { if (owner.repo() !== this.repository) return false; } catch (_) { return false; }
    return owner.personaQuestions.some((request: Decision) => request.id === draft.id && request.raw === draft.raw && ['open', 'pending', 'unknown'].includes(request.status));
  }
  ticket(sheet: QuestionSheet): () => boolean {
    const owner = this.owner, context = this.context, generation = this.generation, scope = this.scope, target = this.target;
    return (): boolean => this.shown && this.sheet === sheet && this.owner === owner && this.context === context && this.generation === generation && this.scope === scope && this.target === target && this.current(sheet.draft);
  }
  beginPresentation(): QuestionSheet | undefined { this.presented = this.sheet; return this.presented; }
  endPresentation(expected: QuestionSheet | undefined): void { if (this.presented === expected) this.presented = undefined; }
  dismiss(expected: QuestionSheet | undefined, nativeDismiss: () => void): void {
    if (expected && this.presented === expected) this.close(expected);
    nativeDismiss();
  }
  close(expected?: QuestionSheet): void { if (!expected || this.sheet === expected) this.shown = false; }
  retire(): void { this.active = false; this.shown = false; }
}
