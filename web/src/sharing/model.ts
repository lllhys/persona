import { ShareError, type SharesClient } from "./client";
import type { Catalog, CreatePolicy, MessagePage, Preview, Selection, ShareMetadata, UpdatePolicy } from "./types";
export interface SharingState {
  stage: "list" | "create" | "edit" | "created"; busy: boolean; error: string | null;
  catalog: Catalog | null; shares: ShareMetadata[]; target: ShareMetadata | null; canWiden: boolean; widenReason: string | null;
  selection: Selection; includeFuture: boolean; showTools: boolean; days: 1 | 3 | 7 | 30; expiresAt: string;
  replacing: boolean; sealed: MessagePage | null; removed: string[];
  preview: Preview | null; acknowledged: boolean; transportAcknowledged: boolean; publicURL: string | null;
}
export class SharingModel {
  state: SharingState = {
    stage: "list", busy: false, error: null, catalog: null, shares: [], target: null, canWiden: false, widenReason: null,
    selection: { mode: "all" }, includeFuture: false, showTools: false, days: 3, expiresAt: "", replacing: false, sealed: null, removed: [],
    preview: null, acknowledged: false, transportAcknowledged: false, publicURL: null,
  };
  private dead = false;
  private epoch = 0;
  private prepared: { fingerprint: string; policy: CreatePolicy | UpdatePolicy } | null = null;
  constructor(readonly client: SharesClient, private readonly changed: () => void) {}
  dispose() { this.dead = true; this.epoch++; this.client.dispose(); this.state.publicURL = null; this.state.preview = null; this.state.catalog = null; this.state.sealed = null; this.state.shares = []; this.state.target = null; this.prepared = null; }
  private emit() { if (!this.dead) this.changed(); }
  private async run(operation: () => Promise<void>) {
    if (this.dead || this.state.busy) return;
    this.state.busy = true; this.state.error = null; const epoch = this.epoch; this.emit();
    try { await operation(); }
    catch (e) {
      if (!this.dead && epoch === this.epoch) this.state.error = e instanceof ShareError ? e.code : "failure";
    } finally { if (!this.dead && epoch === this.epoch) { this.state.busy = false; this.emit(); } }
  }
  private valid() { if (this.dead || !this.client.valid()) throw new ShareError("scope_changed"); }
  private invalidate() { this.state.preview = null; this.state.acknowledged = false; this.state.transportAcknowledged = false; this.prepared = null; }
  change(patch: Partial<Pick<SharingState, "selection" | "includeFuture" | "showTools" | "days" | "expiresAt" | "removed">>) {
    if (this.state.busy || this.dead) return;
    Object.assign(this.state, patch); this.invalidate(); this.emit();
  }
  acknowledge(value: boolean) { this.state.acknowledged = value && this.previewValid(); this.emit(); }
  acknowledgeTransport(value: boolean) { this.state.transportAcknowledged = value && this.previewValid(); this.emit(); }
  previewValid() {
    const p = this.state.preview;
    return !!p && !p.next_cursor && Date.parse(p.expires_at) > Date.now() && this.prepared?.fingerprint === JSON.stringify(this.policy()) && this.client.valid();
  }
  async list() {
    await this.run(async () => {
      const { shares } = await this.client.list(); this.valid();
      this.state.shares = shares; this.state.stage = "list"; this.state.publicURL = null; this.invalidate();
    });
  }
  async beginCreate() {
    await this.run(async () => {
      this.state.stage = "create"; this.state.target = null; this.state.publicURL = null; this.state.catalog = null;
      this.state.selection = { mode: "all" }; this.state.includeFuture = false; this.state.showTools = false; this.state.days = 3; this.invalidate(); this.emit();
      this.state.catalog = await this.client.catalog(); this.valid();
    });
  }
  async refreshCatalog() {
    await this.run(async () => { this.invalidate(); this.state.selection = { mode: "all" }; this.state.catalog = await this.client.catalog(); this.valid(); });
  }
  async moreCatalog() {
    const base = this.state.catalog;
    if (!base?.snapshot_id || !base.next_cursor) return;
    await this.run(async () => {
      const page = await this.client.catalog(base.snapshot_id, base.next_cursor!); this.valid();
      if (page.snapshot_id !== base.snapshot_id || page.snapshot_version !== base.snapshot_version || this.state.catalog !== base) throw new ShareError("revision_conflict");
      this.state.catalog = { ...page, messages: [...base.messages, ...page.messages] };
    });
  }
  async edit(id: string) {
    await this.run(async () => {
      const result = await this.client.get(id); this.valid();
      this.state.stage = "edit"; this.state.target = result.share; this.state.canWiden = result.can_widen; this.state.widenReason = result.reason;
      this.state.includeFuture = result.share.include_future; this.state.showTools = result.share.show_tools; this.state.expiresAt = result.share.expires_at;
      this.state.replacing = false; this.state.removed = []; this.state.catalog = null; this.state.sealed = null; this.invalidate(); this.emit();
      const messages = await this.client.messages(id); this.valid();
      if (messages.revision !== result.share.revision || messages.content_version !== result.share.content_version) throw new ShareError("revision_conflict");
      this.state.sealed = messages;
    });
  }
  async moreSealed() {
    const base = this.state.sealed; const target = this.state.target;
    if (!base?.next_cursor || !target) return;
    await this.run(async () => {
      const page = await this.client.messages(target.share_id, base.next_cursor!); this.valid();
      if (page.revision !== base.revision || page.content_version !== base.content_version) { this.state.sealed = null; this.state.removed = []; throw new ShareError("revision_conflict"); }
      this.state.sealed = { ...page, messages: [...base.messages, ...page.messages] };
    });
  }
  async replaceSelection() {
    if (!this.state.canWiden) return;
    await this.run(async () => {
      this.state.replacing = true; this.state.removed = []; this.state.selection = { mode: "all" }; this.invalidate();
      this.state.catalog = await this.client.catalog(); this.valid();
    });
  }
  policy(): CreatePolicy | UpdatePolicy | null {
    const s = this.state, c = s.catalog;
    const selected = s.selection.mode === "individual" ? s.selection.message_ids.length > 0 && s.selection.message_ids.length <= 1000 : s.selection.mode === "from" ? !!s.selection.from_message_id : true;
    if (s.stage === "create" || s.replacing) {
      if (!c?.can_share || !c.complete || !c.snapshot_id || !c.snapshot_version || !c.expires_at || Date.parse(c.expires_at) <= Date.now() || !selected || (s.includeFuture && !c.can_include_future)) return null;
    }
    if (s.stage === "create" && c?.snapshot_id && c.snapshot_version) return {
      snapshot_id: c.snapshot_id, snapshot_version: c.snapshot_version, selection: s.selection,
      include_future: s.includeFuture, show_tools: s.showTools, expires_in_days: s.days,
    };
    const target = s.target; if (!target || target.status !== "active") return null;
    const expiry = Date.parse(s.expiresAt);
    if (!Number.isFinite(expiry) || expiry <= Date.now() || expiry > Date.parse(target.created_at) + 30 * 86400000) return null;
    const update: UpdatePolicy = { expected_revision: target.revision };
    if (s.includeFuture !== target.include_future || (s.includeFuture && target.publication_status === "paused")) update.include_future = s.includeFuture;
    if (s.showTools !== target.show_tools) update.show_tools = s.showTools;
    if (expiry !== Date.parse(target.expires_at)) update.expires_at = new Date(expiry).toISOString();
    if (s.replacing && c?.snapshot_id && c.snapshot_version) Object.assign(update, { snapshot_id: c.snapshot_id, snapshot_version: c.snapshot_version, selection: s.selection });
    else if (s.removed.length) update.remove_message_ids = s.removed;
    if (this.needsPreviewFor(update)) {
      if (!s.canWiden || s.removed.length) return null;
      update.expected_content_version = target.content_version;
    }
    return Object.keys(update).length > 1 ? update : null;
  }
  private needsPreviewFor(policy: UpdatePolicy) {
    const target = this.state.target;
    return !!policy.selection || policy.show_tools === true || policy.include_future === true || (!!policy.expires_at && !!target && Date.parse(policy.expires_at) > Date.parse(target.expires_at));
  }
  needsPreview() { const p = this.policy(); return this.state.stage === "create" || (!!p && this.needsPreviewFor(p as UpdatePolicy)); }
  async preview() {
    const policy = this.policy(); if (!policy || ("remove_message_ids" in policy && policy.remove_message_ids?.length)) return;
    await this.run(async () => {
      this.invalidate();
      const p = await this.client.preview(policy, this.state.target?.share_id); this.valid();
      this.prepared = { policy, fingerprint: JSON.stringify(policy) }; this.state.preview = p;
    });
  }
  async morePreview() {
    const base = this.state.preview; if (!base?.next_cursor) return;
    await this.run(async () => {
      const page = await this.client.previewPage(base.preview_id, base.next_cursor!); this.valid();
      if (page.preview_id !== base.preview_id || page.policy_version !== base.policy_version || page.expires_at !== base.expires_at || page.requires_insecure_transport_ack !== base.requires_insecure_transport_ack || this.state.preview !== base) { this.invalidate(); throw new ShareError("revision_conflict"); }
      this.state.preview = { ...page, messages: [...base.messages, ...page.messages] };
    });
  }
  canSave() { return !this.state.busy && !!this.policy() && (!this.needsPreview() || (this.state.acknowledged && this.previewValid() && (!this.state.preview?.requires_insecure_transport_ack || this.state.transportAcknowledged))); }
  async save() {
    if (!this.canSave()) return;
    const policy = this.policy()!; const p = this.state.preview;
    const acknowledgement = this.needsPreview() && p ? { sensitive_content: true as const, policy_version: p.policy_version, preview_id: p.preview_id, ...(p.requires_insecure_transport_ack && this.state.transportAcknowledged ? { insecure_transport: true as const } : {}) } : undefined;
    const create = this.state.stage === "create";
    await this.run(async () => {
      if (create) {
        try {
          const result = await this.client.create(policy as CreatePolicy, acknowledgement!); this.valid();
          const url = new URL(result.public_url);
          if (!["http:", "https:"].includes(url.protocol) || url.origin === location.origin || (url.protocol === "http:" && !acknowledgement?.insecure_transport) || url.username || url.password || url.search || url.hash || !/^\/share\/[A-Za-z0-9_-]{64}$/.test(url.pathname)) throw new ShareError("invalid_response");
          this.state.publicURL = result.public_url; this.state.target = result.share; this.state.stage = "created"; this.invalidate();
        } catch (e) {
          this.invalidate();
          if (e instanceof ShareError && ["public_origin_unavailable", "revision_conflict", "owner_enrollment_required", "not_owner", "scope_changed"].includes(e.code)) throw e;
          throw new ShareError("lost");
        }
      } else {
        const result = await this.client.update(this.state.target!.share_id, policy as UpdatePolicy, acknowledgement); this.valid();
        this.state.target = result.share; this.invalidate(); this.state.sealed = null; this.state.removed = []; this.state.replacing = false;
        this.state.includeFuture = result.share.include_future; this.state.showTools = result.share.show_tools; this.state.expiresAt = result.share.expires_at;
        const messages = await this.client.messages(result.share.share_id); this.valid();
        if (messages.revision !== result.share.revision || messages.content_version !== result.share.content_version) throw new ShareError("revision_conflict");
        this.state.sealed = messages;
      }
    });
  }
  async revoke() {
    const target = this.state.target; if (!target || target.status !== "active") return;
    await this.run(async () => {
      const result = await this.client.revoke(target.share_id, target.revision); this.valid();
      if (result.share_id !== target.share_id || result.status !== "revoked" || result.revision !== target.revision + 1) throw new ShareError("invalid_response");
      this.state.target = { ...target, status: "revoked", revision: result.revision }; this.state.sealed = null; this.state.publicURL = null; this.invalidate();
    });
  }
}
