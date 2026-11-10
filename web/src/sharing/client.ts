// Sharing uses a separate bounded transport: never put capability responses in application breadcrumbs or receipts.
import { BASE } from "../lib/base";
import { credentialOwner, getToken } from "../lib/api";
import { currentLanguage } from "../i18n";
import { uuid } from "../lib/format";
import type { Acknowledgement, Catalog, CreatePolicy, MessagePage, Preview, ShareMetadata, UpdatePolicy } from "./types";

export class ShareError extends Error {
  constructor(readonly code: string, readonly status = 0) { super(code); }
}
export class SharesClient {
  private readonly token = getToken();
  private readonly owner = credentialOwner();
  private readonly controller = new AbortController();
  constructor(readonly manager: string, readonly machine: string, readonly session: string, private readonly current: () => boolean) {}
  dispose() { this.controller.abort(); }
  valid() { return !this.controller.signal.aborted && this.owner === credentialOwner() && this.token === getToken() && this.current(); }
  private async request<T>(path: string, method = "GET", json?: unknown): Promise<T> {
    if (!this.manager || !this.valid()) throw new ShareError("scope_changed");
    const controller = new AbortController();
    const abort = () => controller.abort();
    this.controller.signal.addEventListener("abort", abort, { once: true });
    const timer = setTimeout(abort, 45_000);
    try {
      const body = json === undefined ? undefined : JSON.stringify(json);
      if (body && new TextEncoder().encode(body).length > 64 * 1024) throw new ShareError("request_limit");
      const res = await fetch(`${BASE}/api/v1${path}`, {
        method, body, signal: controller.signal, credentials: "omit", cache: "no-store", redirect: "error",
        headers: { Authorization: `Bearer ${this.token}`, "X-Persona-Manager": this.manager, "Accept-Language": currentLanguage(), "Content-Type": "application/json", Accept: "application/json" },
      });
      if (!this.valid()) throw new ShareError("scope_changed");
      if (!res.headers.get("Content-Type")?.includes("application/json")) throw new ShareError("invalid_response", res.status);
      const reader = res.body?.getReader();
      if (!reader) throw new ShareError("invalid_response");
      const chunks: Uint8Array[] = []; let size = 0;
      try {
        for (;;) {
          const { done, value } = await reader.read(); if (done) break;
          size += value.length; if (size > 512 * 1024) throw new ShareError("response_limit");
          chunks.push(value);
        }
      } finally { await reader.cancel().catch(() => {}); }
      if (!this.valid()) throw new ShareError("scope_changed");
      const bytes = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.length; }
      let data: Record<string, unknown>;
      try { data = JSON.parse(new TextDecoder().decode(bytes)); } catch { throw new ShareError("invalid_response"); }
      if (!res.ok) throw new ShareError(typeof data.code === "string" ? data.code : "request_failed", res.status);
      return data as T;
    } catch (e) {
      if (e instanceof ShareError) throw e;
      throw new ShareError(this.valid() ? "request_failed" : "scope_changed");
    } finally { clearTimeout(timer); this.controller.signal.removeEventListener("abort", abort); }
  }
  private get sessionPath() { return `/sessions/${encodeURIComponent(this.machine)}/${encodeURIComponent(this.session)}`; }
  catalog(snapshot?: string, cursor?: string) {
    const q = new URLSearchParams({ limit: "100" });
    if (snapshot) q.set("snapshot_id", snapshot); if (cursor) q.set("cursor", cursor);
    return this.request<Catalog>(`${this.sessionPath}/share-catalog?${q}`);
  }
  preview(policy: CreatePolicy | UpdatePolicy, share?: string) {
    const { remove_message_ids: _remove, ...update } = policy as UpdatePolicy;
    // Offline removal is narrowing and never needs a preview; the preview endpoint accepts target policies only.
    return this.request<Preview>(`${this.sessionPath}/share-preview`, "POST", share ? { operation: "update", share_id: share, ...update } : { operation: "create", ...policy });
  }
  previewPage(id: string, cursor: string) { return this.request<Preview>(`${this.sessionPath}/share-preview/${encodeURIComponent(id)}?${new URLSearchParams({ cursor, limit: "100" })}`); }
  create(policy: CreatePolicy, acknowledgement: Acknowledgement) {
    return this.request<{ share: ShareMetadata; public_url: string }>("/agent-shares", "POST", {
      client_request_id: uuid(), machine: this.machine, session_id: this.session, ...policy, acknowledgement,
    }); // Deliberately one attempt. A lost response is recovered through tokenless list/revoke/recreate.
  }
  list() { return this.request<{ shares: ShareMetadata[] }>(`/agent-shares?${new URLSearchParams({ machine: this.machine, session_id: this.session })}`); }
  get(id: string) { return this.request<{ share: ShareMetadata; can_widen: boolean; reason: string | null }>(`/agent-shares/${encodeURIComponent(id)}`); }
  messages(id: string, cursor?: string) { return this.request<MessagePage>(`/agent-shares/${encodeURIComponent(id)}/messages?${new URLSearchParams({ limit: "100", ...(cursor ? { cursor } : {}) })}`); }
  update(id: string, policy: UpdatePolicy, acknowledgement?: Acknowledgement) {
    return this.request<{ share: ShareMetadata }>(`/agent-shares/${encodeURIComponent(id)}`, "PATCH", { ...policy, ...(acknowledgement ? { acknowledgement } : {}) });
  }
  revoke(id: string, revision: number) { return this.request<{ share_id: string; revision: number; status: "revoked" }>(`/agent-shares/${encodeURIComponent(id)}`, "DELETE", { expected_revision: revision }); }
}
