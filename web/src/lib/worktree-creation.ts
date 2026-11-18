import { t } from "../i18n";
import { ApiError, credentialOwner, getToken } from "./api";
import { uuid } from "./format";
import { defaultManager } from "./manager-choice";
import { getState } from "./store";
import { refreshWorkspaceCatalog } from "./workspace-catalog";
import { emitWorktreeRequest, branchProblem, formProblem, suggestedPath, isWorktreeCreated, sendWorktreeCreate, worktreeReceipt, worktreeAdmission,
  type WorktreeTarget, type WorktreeRequest, type WorktreePrepare, type WorktreeCreateBody, type WorktreeCreated, type WorktreeSource } from "./worktree";
import type { WorkspaceTarget } from "./workspace-target";

export interface RepositoryDraft { basic?: boolean; baseRef: string; preset: string; provider?: WorktreeSource }
export interface CreationDraft {
  source: WorktreeTarget;
  parent?: WorkspaceTarget | null;
  name: string;
  branch: string;
  path: string;
  pathEdited: boolean;
  automaticName: boolean;
  note: string;
  repositories: Record<string, RepositoryDraft>;
}
export interface CreationIntent {
  id: string;
  body: WorktreeCreateBody;
  target: WorktreeTarget;
  phase: "sending" | "pending" | "unknown" | "refused" | "created";
  created?: WorktreeCreated;
  error?: string;
}
export interface CreationSnapshot { draft: CreationDraft; intent?: CreationIntent; storageError?: string }
const sessions = new Map<string, WorktreeCreationSession>();
export const sourceKey = (target: WorktreeTarget) => JSON.stringify([target.manager, target.mid, target.host, target.workspaceId]);
export const repositoryDraft = (draft: CreationDraft): RepositoryDraft => draft.repositories[sourceKey(draft.source)] ?? { baseRef: "", preset: "" };
export function samePlacement(source: WorktreeTarget, parent: WorkspaceTarget): boolean {
  return parent.machineId === source.mid && parent.host === source.host && parent.owner === source.owner && parent.manager === source.manager;
}

export function unsupportedWorktreeParent(draft: CreationDraft, info: WorktreePrepare): boolean {
  const { source, parent } = draft;
  return parent !== undefined && !info.choose_parent
    && !(parent && samePlacement(source, parent) && parent.workspaceId === source.workspaceId);
}

export function creationBody(draft: CreationDraft, info: WorktreePrepare): WorktreeCreateBody {
  const { source, parent } = draft;
  const repo = repositoryDraft(draft);
  const advanced = source.host === "orca" && !repo.basic ? info.composer : undefined;
  const unsupported = () => { throw new Error(t("worktree-composer.unsupported")); };
  const parentBody = parent === null ? { no_parent: true as const } : parent ? { parent_workspace_id: parent.workspaceId } : {};
  if (parent && !samePlacement(source, parent)) throw new Error(t("worktree-composer.parent_mismatch"));
  if (advanced) {
    const has = (cap: string) => advanced.capabilities.includes(cap);
    if (!has("create")) unsupported();
    if (draft.branch.trim() && !has("branch_override")) unsupported();
    if (repo.provider && !has("source.github")) unsupported();
    if (repo.preset && (!has("sparse_presets") || !advanced.presets.some((p) => p.id === repo.preset))) unsupported();
    if (repo.baseRef && !advanced.refs.includes(repo.baseRef)) throw new Error(t("worktree-composer.base_missing"));
    if (draft.branch.trim()) { const problem = branchProblem(draft.branch); if (problem) throw new Error(problem); }
    return { workspace_id: source.workspaceId, kind: source.host, composer: true,
      ...(draft.name.trim() ? { name: draft.name.trim() } : {}),
      ...(draft.branch.trim() ? { branch_override: draft.branch.trim() } : {}),
      base_ref: repo.baseRef || advanced.base_ref, ...parentBody,
      ...(draft.note.trim() ? { note: draft.note.trim() } : {}),
      ...(repo.preset ? { sparse_preset_id: repo.preset } : {}),
      ...(repo.provider ? { source_id: `${repo.provider.type}:${repo.provider.number}`, source_url: repo.provider.url } : {}),
    };
  }
  // Never downgrade an advanced choice into a different legacy checkout.
  if (draft.name.trim() || draft.note.trim() || repo.baseRef || repo.preset || repo.provider) unsupported();
  if (unsupportedWorktreeParent(draft, info)) unsupported();
  if (draft.automaticName && !info.automatic_name) unsupported();
  const legacyForm = { ...draft, path: draft.pathEdited ? draft.path : suggestedPath(info.path, draft.branch) };
  if (!draft.automaticName) { const problem = formProblem(legacyForm); if (problem) throw new Error(problem); }
  return { workspace_id: source.workspaceId, ...(source.host ? { kind: source.host } : {}), ...(info.choose_parent ? parentBody : {}),
    ...(draft.automaticName ? { auto_name: true as const } : { branch: draft.branch.trim(), path: legacyForm.path.trim() }) };
}

export function createdWorkspace(intent: CreationIntent): WorkspaceTarget | undefined {
  if (!intent.created) return;
  const { target, created } = intent;
  return { machineId: target.mid, host: target.host || "", workspaceId: created.workspace_id, cwd: created.path,
    label: created.path.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || created.branch, owner: target.owner, manager: target.manager };
}

/** One session outlives its dialog; in-flight completions cannot mutate another draft. */
export class WorktreeCreationSession {
  private snapshot: CreationSnapshot;
  private listeners = new Set<() => void>();
  private sending?: Promise<void>;
  constructor(private key: string, snapshot: CreationSnapshot) { this.snapshot = snapshot; }
  getSnapshot = () => this.snapshot;
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  private publish(next: CreationSnapshot): boolean {
    try {
      sessionStorage.setItem(this.key, JSON.stringify(next));
      this.snapshot = next;
      this.listeners.forEach((listener) => listener());
      return true;
    } catch {
      this.snapshot = { ...next, storageError: t("worktree-composer.storage_failed") };
      this.listeners.forEach((listener) => listener());
      return false;
    }
  }
  update(patch: Partial<CreationDraft>) {
    if (this.snapshot.intent && this.snapshot.intent.phase !== "refused") return;
    this.publish({ ...this.snapshot, draft: { ...this.snapshot.draft, ...patch } });
  }
  updateRepository(patch: Partial<RepositoryDraft>) {
    const draft = this.snapshot.draft;
    this.update({ repositories: { ...draft.repositories, [sourceKey(draft.source)]: { ...repositoryDraft(draft), ...patch } } });
  }
  fresh() {
    if (this.snapshot.intent?.phase !== "created") return;
    this.publish({ draft: this.snapshot.draft });
  }
  submit(info?: WorktreePrepare): Promise<void> {
    if (this.sending) return this.sending;
    const { draft } = this.snapshot;
    if (!worktreeAdmission(draft.source)) return Promise.reject(new Error(t("worktree.owner_changed")));
    let intent = this.snapshot.intent;
    if (intent?.phase === "created") return Promise.resolve();
    if (!intent || intent.phase === "refused") {
      if (!info) throw new Error(t("worktree-composer.not_ready"));
      const body = creationBody(draft, info);
      if (!intent || JSON.stringify(body) !== JSON.stringify(intent.body) || sourceKey(intent.target) !== sourceKey(draft.source)) {
        intent = { id: uuid(), body, target: { ...draft.source }, phase: "sending" };
      }
    }
    const current = { ...intent, phase: "sending" as const, error: undefined };
    // Persist before dispatch: even a terminated page must recover the same ID and body.
    const previous = this.snapshot;
    if (!this.publish({ draft, intent: current })) {
      this.snapshot = { ...previous, storageError: t("worktree-composer.storage_failed") };
      this.listeners.forEach((listener) => listener());
      return Promise.resolve();
    }
    this.sending = this.deliver(current, Boolean(this.snapshot.intent && intent.phase !== "sending")).finally(() => { this.sending = undefined; });
    return this.sending;
  }
  private async deliver(intent: CreationIntent, retry: boolean) {
    let next: CreationIntent;
    try {
      if (retry) {
        let state = "";
        try { state = (await worktreeReceipt(intent.target, intent.id)).state; } catch { /* Exact-ID replay remains safe when receipt reads are unavailable. */ }
        if (state === "unknown" || state === "failed") {
          this.publish({ ...this.snapshot, intent: { ...intent, phase: state === "failed" ? "refused" : "unknown" } });
          return;
        }
      }
      const result = await sendWorktreeCreate(intent.target, intent.body, intent.id);
      next = isWorktreeCreated(result) ? { ...intent, phase: "created", created: result }
        : { ...intent, phase: "state" in result && result.state === "pending" ? "pending" : "unknown" };
    } catch (error) {
      let state = "";
      try { state = (await worktreeReceipt(intent.target, intent.id)).state; } catch { /* An unavailable receipt never proves a timeout was refused. */ }
      const refused = state === "failed" || !state && error instanceof ApiError && error.body.delivered === "none";
      next = { ...intent, phase: refused ? "refused" : state === "pending" ? "pending" : "unknown", error: error instanceof Error ? error.message : String(error) };
    }
    this.publish({ ...this.snapshot, intent: next });
    if (next.phase === "created" && worktreeAdmission(intent.target)) void refreshWorkspaceCatalog();
  }
}

export async function creationSession(request: WorktreeRequest): Promise<WorktreeCreationSession> {
  const owner = credentialOwner();
  const token = getToken();
  const manager = request.target.manager || getState().machines.find((m) => m.id === request.target.mid)?.manager || defaultManager();
  const requested = request.source ? { ...request.target, mid: request.source.machineId, workspaceId: request.source.workspaceId, host: request.source.host,
    cwd: request.source.cwd, label: request.source.label, owner: request.source.owner, manager: request.source.manager } : request.target;
  const source: WorktreeTarget = { ...requested, owner: requested.owner ?? owner, manager: requested.manager ?? manager };
  if (!worktreeAdmission(source)) throw new Error(t("worktree.owner_changed"));
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(token));
  if (credentialOwner() !== owner || getToken() !== token || !worktreeAdmission(source)) throw new Error(t("worktree.owner_changed"));
  const partition = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
  const placement = request.parent === null ? "none" : request.parent ? JSON.stringify([request.parent.manager ?? manager, request.parent.machineId, request.parent.host, request.parent.workspaceId]) : "source";
  const key = `persona.worktree-create.v1.${partition}.${sourceKey(source)}.${placement}`;
  const cached = sessions.get(`${owner}:${key}`);
  if (cached) return cached;
  const bindParent = (parent: WorkspaceTarget | null | undefined) => parent ? { ...parent, owner: parent.owner ?? owner, manager: parent.manager ?? manager } : parent;
  let snapshot: CreationSnapshot = { draft: { source, parent: bindParent(request.parent), name: "", branch: "", path: "", pathEdited: false,
    automaticName: false, note: "", repositories: {} } };
  try {
    const raw = sessionStorage.getItem(key);
    if (raw) {
      const saved = JSON.parse(raw) as CreationSnapshot;
      if (!saved.draft?.source?.workspaceId || !saved.draft.repositories || saved.intent && (!saved.intent.id || !saved.intent.body || !saved.intent.target)) throw new Error();
      saved.draft.source.owner = owner;
      if (saved.draft.parent) saved.draft.parent.owner = owner;
      if (saved.intent) { saved.intent.target.owner = owner; if (saved.intent.phase === "sending") saved.intent.phase = "unknown"; }
      snapshot = saved;
    }
  } catch { throw new Error(t("worktree-composer.recovery_failed")); }
  const session = new WorktreeCreationSession(key, snapshot);
  sessions.set(`${owner}:${key}`, session);
  return session;
}

/** Opens the child composer with source and visual placement kept independently. */
export function openChildWorktree(parent: WorkspaceTarget, options: { source?: WorkspaceTarget; changed?: () => void; onCreated?: WorktreeRequest["onCreated"] } = {}) {
  const bind = (t: WorkspaceTarget): WorkspaceTarget => ({ ...t, owner: t.owner ?? credentialOwner(),
    manager: t.manager ?? (getState().machines.find((m) => m.id === t.machineId)?.manager || defaultManager()) });
  const source = bind(options.source ?? parent);
  const request: WorktreeRequest = { target: { mid: source.machineId, workspaceId: source.workspaceId, host: source.host,
    label: source.label, cwd: source.cwd, owner: source.owner, manager: source.manager }, source, parent: bind(parent),
    changed: options.changed ?? (() => {}), onCreated: options.onCreated };
  emitWorktreeRequest(request);
}

export const openNewWorktreeFromTarget = openChildWorktree;
