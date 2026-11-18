import type { ClientModel } from './ClientModel';
import type { HubRepository } from './HubRepository';

export type ComposerOverlayKind = 'attachments' | 'actions';

let composerOverlaySequence: number = 0;

/** A one-shot admission token for a transient composer chooser. */
export class ComposerOverlaySession {
  readonly id: string;
  readonly kind: ComposerOverlayKind;
  readonly request: number;
  readonly preserveInput: boolean;
  readonly owner: ClientModel;

  constructor(id: string, kind: ComposerOverlayKind, request: number, preserveInput: boolean, owner: ClientModel) {
    this.id = id;
    this.kind = kind;
    this.request = request;
    this.preserveInput = preserveInput;
    this.owner = owner;
  }
}

/** Owns one ephemeral chooser admission and its optional native-presentation lease. */
@ObservedV2
export class ComposerOverlayWorkspace {
  @Trace shown: boolean = false;
  @Trace session: ComposerOverlaySession | undefined;
  @Trace presenting: boolean = false;

  private active: boolean = false;
  private scope: string = '';
  private generation: number = 0;
  private target: string = '';
  private repository: HubRepository | undefined;
  private presented: ComposerOverlaySession | undefined;

  open(model: ClientModel, active: boolean, kind: ComposerOverlayKind, request: number,
    preserveInput: boolean): ComposerOverlaySession | undefined {
    if (this.shown || this.presenting || this.presented) return undefined;
    this.active = active;
    if (!active || !model.isChat()) return undefined;

    const scope = model.scope();
    const generation = model.connectionGeneration();
    const target = model.target();
    if (!model.owns(generation, scope)) return undefined;

    let repository: HubRepository | undefined;
    if (model.connected) {
      try { repository = model.repo(); } catch (_) { repository = undefined; }
    }

    const session = new ComposerOverlaySession(String(++composerOverlaySequence), kind, request, preserveInput, model);
    this.scope = scope;
    this.generation = generation;
    this.target = target;
    this.repository = repository;
    this.session = session;
    this.shown = true;
    return session;
  }

  sync(model: ClientModel, active: boolean): void {
    this.active = active;
    const session = this.session;
    if (!session || !this.shown) return;
    if (!active || session.owner !== model || !this.ownerCurrent(session.owner)) this.close(session);
  }

  current(session: ComposerOverlaySession, needsRepository: boolean = false): boolean {
    if (!this.shown || this.session !== session || !this.active || !this.ownerCurrent(session.owner)) return false;
    if (needsRepository) {
      if (!session.owner.connected || !this.repository) return false;
      try { if (session.owner.repo() !== this.repository) return false; } catch (_) { return false; }
    }
    return true;
  }

  ticket(session: ComposerOverlaySession, needsRepository: boolean = false): () => boolean {
    return (): boolean => this.current(session, needsRepository);
  }

  /** Called only when the native chooser actually starts presenting. */
  begin(session: ComposerOverlaySession): void {
    if (this.session === session && this.shown && (!this.presented || this.presented === session)) {
      this.presented = session;
      this.presenting = true;
    }
  }

  /** Releases this session and/or its matching native presentation, never a newer lease. */
  finish(session: ComposerOverlaySession): boolean {
    const isCurrent = this.session === session;
    const isPresented = this.presented === session;
    if (!isCurrent && !isPresented) return false;
    if (this.presented === session) this.presented = undefined;
    if (isCurrent) {
      this.shown = false;
      this.session = undefined;
      this.repository = undefined;
    }
    this.presenting = false;
    return isCurrent;
  }

  close(expected?: ComposerOverlaySession): boolean {
    const current = this.session;
    if (!current || (expected && current !== expected)) return false;
    this.shown = false;
    return true;
  }

  /** Immediately retires every lease when the native host is removed. */
  retire(): void {
    this.active = false;
    this.shown = false;
    this.session = undefined;
    this.repository = undefined;
    this.presented = undefined;
    this.presenting = false;
    this.scope = '';
    this.generation = 0;
    this.target = '';
  }

  private ownerCurrent(owner: ClientModel): boolean {
    try {
      return owner.isChat() && owner.owns(this.generation, this.scope) && owner.target() === this.target;
    } catch (_) { return false; }
  }
}
