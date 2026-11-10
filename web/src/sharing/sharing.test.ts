// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setToken } from "../lib/api";
import { SharesClient } from "./client";
import { SharingModel } from "./model";
import { selectedCount } from "./SharingDialog";
import type { Catalog, ShareMetadata } from "./types";
const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
const now = () => new Date(Date.now() + 600000).toISOString();
const catalog: Catalog = { can_share: true, reason: null, can_include_future: true, snapshot_id: "snapshot-1", snapshot_version: "version-1", expires_at: now(), complete: true, total_messages: 101, omitted_messages: 1, omissions: ["source_unsupported"], messages: [{ message_id: "catalog-1", role: "user", text: "Synthetic first input", selectable: true }], next_cursor: "catalog-next" };
const share = (patch: Partial<ShareMetadata> = {}): ShareMetadata => ({ share_id: "share-1", revision: 1, content_version: 1, selection: { mode: "all", count: 101 }, include_future: false, show_tools: false, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), expires_at: now(), status: "active", publication_status: "frozen", pause_reason: null, ...patch });
function setup(fetch: typeof globalThis.fetch) { vi.stubGlobal("fetch", fetch); const client = new SharesClient("assistant-synthetic", "computer-synthetic", "session-synthetic", () => true); return new SharingModel(client, () => {}); }
beforeEach(() => setToken("synthetic-credential-a"));
afterEach(() => { vi.unstubAllGlobals(); setToken(""); });
describe("sharing uses the frozen HTTP contract", () => {
  it("pins explicit authority, never retries creation, and fences credential A/B/A", async () => {
    let finish!: (res: Response) => void; let current = true;
    const fetch = vi.fn<typeof globalThis.fetch>(() => new Promise((resolve) => { finish = resolve; })); vi.stubGlobal("fetch", fetch);
    const client = new SharesClient("assistant-synthetic", "computer-synthetic", "session-synthetic", () => current);
    const list = client.list(); const init = fetch.mock.calls[0][1]!;
    expect(init).toMatchObject({ credentials: "omit", cache: "no-store", redirect: "error" });
    expect(init.headers).toMatchObject({ Authorization: "Bearer synthetic-credential-a", "X-Persona-Manager": "assistant-synthetic" });
    setToken("synthetic-credential-b"); setToken("synthetic-credential-a"); finish(response({ shares: [] })); await expect(list).rejects.toMatchObject({ code: "scope_changed" });
    current = false; await expect(client.list()).rejects.toMatchObject({ code: "scope_changed" }); expect(fetch).toHaveBeenCalledTimes(1); client.dispose();
  });
  it("uses the existing CSPRNG UUID fallback when an HTTP app has no crypto.randomUUID", async () => {
    const real = crypto; vi.stubGlobal("crypto", { getRandomValues: (a: Uint8Array) => real.getRandomValues(a) });
    const fetch = vi.fn<typeof globalThis.fetch>(async () => response({ share: share(), public_url: `https://share.example.test/share/${"A".repeat(64)}` })); vi.stubGlobal("fetch", fetch);
    const client = new SharesClient("assistant-synthetic", "computer-synthetic", "session-synthetic", () => true);
    await client.create({ snapshot_id: "snapshot-1", snapshot_version: "version-1", selection: { mode: "all" }, include_future: false, show_tools: false, expires_in_days: 3 }, { sensitive_content: true, policy_version: 1, preview_id: "preview" });
    expect(JSON.parse(String(fetch.mock.calls[0][1]?.body)).client_request_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/); client.dispose();
  });
  it("all means the complete server catalog, not the loaded tail; exact preview acknowledgement binds create", async () => {
    const calls: { path: string; body: Record<string, unknown> }[] = [];
    const fetch = vi.fn<typeof globalThis.fetch>(async (url, init) => {
      const path = String(url); const body = init?.body ? JSON.parse(String(init.body)) : {}; calls.push({ path, body });
      if (path.includes("share-catalog")) return response(catalog);
      if (path.includes("share-preview")) return response({ preview_id: "preview-1", policy_version: 1, expires_at: now(), selected_count: 101, omitted_count: 1, messages: [], next_cursor: null });
      return response({ share: share(), public_url: `https://share.example.test/share/${"A".repeat(64)}` });
    }); const model = setup(fetch);
    await model.beginCreate(); expect(model.state).toMatchObject({ days: 3, includeFuture: false, showTools: false });
    expect(model.state.catalog?.messages).toHaveLength(1); await model.preview(); model.acknowledge(true); await model.save();
    expect(calls.at(-1)?.body).toMatchObject({ machine: "computer-synthetic", session_id: "session-synthetic", selection: { mode: "all" }, expires_in_days: 3, include_future: false, show_tools: false, acknowledgement: { sensitive_content: true, preview_id: "preview-1", policy_version: 1 } });
    expect(model.state.stage).toBe("created"); await model.list(); expect(model.state.publicURL).toBeNull(); model.dispose();
  });
  it("pagination retains snapshot identity and policy changes invalidate consent, including away/back", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>(async (url) => String(url).includes("cursor=") ? response({ ...catalog, messages: [{ message_id: "catalog-older", role: "assistant", text: "Synthetic older result", selectable: true }], next_cursor: null }) : String(url).includes("share-catalog") ? response(catalog) : response({ preview_id: "preview-1", policy_version: 1, expires_at: now(), selected_count: 101, omitted_count: 0, messages: [], next_cursor: null }));
    const model = setup(fetch); await model.beginCreate(); await model.moreCatalog(); expect(model.state.catalog?.messages).toHaveLength(2);
    expect(String(fetch.mock.calls[1][0])).toContain("snapshot_id=snapshot-1");
    model.change({ selection: { mode: "from", from_message_id: "catalog-older" } }); await model.preview(); model.acknowledge(true); expect(model.canSave()).toBe(true);
    model.change({ days: 7 }); model.change({ days: 3 }); expect(model.canSave()).toBe(false); expect(model.state.preview).toBeNull(); model.dispose();
  });
  it("requires complete preview pagination and recovers a lost creation without replay", async () => {
    let pages = 0; const expiry = now(); const fetch = vi.fn<typeof globalThis.fetch>(async (url, init) => {
      if (String(url).includes("share-catalog")) return response(catalog);
      if (String(url).includes("share-preview")) return response({ preview_id: "preview-1", policy_version: 1, expires_at: expiry, selected_count: 101, omitted_count: 0, messages: [], next_cursor: pages++ === 0 ? "preview-next" : null });
      if (init?.method === "POST") throw new TypeError("synthetic connection interruption");
      return response({ shares: [share()] });
    }); const model = setup(fetch); await model.beginCreate(); await model.preview(); model.acknowledge(true); expect(model.canSave()).toBe(false); await model.morePreview(); model.acknowledge(true);
    const saving = model.save(); void model.save(); await saving; expect(model.state.error).toBe("lost"); expect(fetch.mock.calls.filter(([, init]) => init?.method === "POST" && !String(init.body).includes("operation"))).toHaveLength(1);
    await model.list(); expect(model.state.shares).toHaveLength(1); expect(model.state.publicURL).toBeNull(); model.dispose();
  });
  it("narrows sealed selections and revokes offline without catalog or preview; revision conflicts cannot revive", async () => {
    let revision = 1;
    const fetch = vi.fn<typeof globalThis.fetch>(async (url, init) => {
      if (String(url).includes("/messages")) return response({ revision, content_version: revision, messages: [{ message_id: "editor-1", role: "user", text: "Synthetic sealed input" }], next_cursor: null });
      if (init?.method === "PATCH") { const body = JSON.parse(String(init.body)); expect(body).toEqual({ expected_revision: 1, remove_message_ids: ["editor-1"] }); revision++; return response({ share: share({ revision, content_version: revision }) }); }
      if (init?.method === "DELETE") return response({ share_id: "share-1", revision: ++revision, status: "revoked" });
      return response({ share: share(), can_widen: false, reason: "authority_unavailable" });
    }); const model = setup(fetch); await model.edit("share-1"); model.change({ removed: ["editor-1"] }); expect(model.canSave()).toBe(true); await model.save(); await model.revoke(); expect(model.policy()).toBeNull(); expect(model.state.target?.status).toBe("revoked");
    expect(fetch.mock.calls.some(([url]) => /share-catalog|share-preview/.test(String(url)))).toBe(false); model.dispose();
  });
  it("expiry extension/tools/future/replacement all require a new exact policy preview", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>(async (url) => String(url).includes("/messages") ? response({ revision: 1, content_version: 1, messages: [], next_cursor: null }) : String(url).includes("share-preview") ? response({ preview_id: "update-preview", policy_version: 1, expires_at: now(), selected_count: 1, omitted_count: 0, messages: [], next_cursor: null }) : response({ share: share(), can_widen: true, reason: null }));
    const model = setup(fetch); await model.edit("share-1"); model.change({ showTools: true, includeFuture: true, expiresAt: new Date(Date.now() + 86400000).toISOString() });
    expect(model.canSave()).toBe(false); await model.preview(); expect(JSON.parse(String(fetch.mock.calls.at(-1)?.[1]?.body))).toMatchObject({ operation: "update", share_id: "share-1", expected_revision: 1, expected_content_version: 1, include_future: true, show_tools: true });
    model.acknowledge(true); expect(model.canSave()).toBe(true); model.change({ expiresAt: new Date(Date.now() + 31 * 86400000).toISOString() }); expect(model.policy()).toBeNull(); model.dispose();
  });
  it("accepts an independent HTTP origin only after a separately bound transport acknowledgement", async () => {
    const fetch = vi.fn<typeof globalThis.fetch>(async (url) => {
      if (String(url).includes("share-catalog")) return response(catalog);
      if (String(url).includes("share-preview")) return response({ preview_id: "http-preview", policy_version: 1, expires_at: now(), selected_count: 1, omitted_count: 0, messages: [], next_cursor: null, requires_insecure_transport_ack: true });
      return response({ share: share(), public_url: `http://localhost:10306/share/${"A".repeat(64)}` });
    });
    const model = setup(fetch); await model.beginCreate(); await model.preview(); model.acknowledge(true);
    expect(model.canSave()).toBe(false); model.acknowledgeTransport(true); expect(model.canSave()).toBe(true);
    model.change({ days: 7 }); expect(model.state.transportAcknowledged).toBe(false);
    await model.preview(); model.acknowledge(true); model.acknowledgeTransport(true); await model.save();
    expect(JSON.parse(String(fetch.mock.calls.at(-1)?.[1]?.body)).acknowledgement).toEqual({ sensitive_content: true, policy_version: 1, preview_id: "http-preview", insecure_transport: true });
    expect(model.state.stage).toBe("created"); model.dispose();
  });
  it.each(["http://localhost:10306", "https://share.example.test"])("rejects an unacknowledged HTTP or same-origin capability response: %s", async (origin) => {
    const url = origin.startsWith("https") ? location.origin : origin;
    const model = setup(vi.fn<typeof globalThis.fetch>(async (path) => String(path).includes("share-catalog") ? response(catalog) : String(path).includes("share-preview") ? response({ preview_id: "preview", policy_version: 1, expires_at: now(), selected_count: 1, omitted_count: 0, messages: [], next_cursor: null }) : response({ share: share(), public_url: `${url}/share/${"A".repeat(64)}` })));
    await model.beginCreate(); await model.preview(); model.acknowledge(true); await model.save(); expect(model.state.publicURL).toBeNull(); expect(model.state.error).toBe("lost"); model.dispose();
  });
  it("denied catalogs never permit preview/create", async () => {
    const model = setup(vi.fn<typeof globalThis.fetch>(async () => response({ can_share: false, reason: "ownership_unbound", can_include_future: false, messages: [] })));
    await model.beginCreate(); expect(model.policy()).toBeNull(); await model.preview(); expect(model.state.preview).toBeNull(); model.dispose();
  });
  it("sends exactly one selection shape: individual IDs, from a message, or all", async () => {
    const bodies: Record<string, unknown>[] = [];
    const fetch = vi.fn<typeof globalThis.fetch>(async (url, init) => {
      if (init?.body) bodies.push(JSON.parse(String(init.body)));
      if (String(url).includes("share-catalog")) return response({ ...catalog, next_cursor: null, messages: [...catalog.messages, { message_id: "catalog-2", role: "assistant", text: "Synthetic answer", selectable: true }] });
      return response({ preview_id: "preview-i", policy_version: 1, expires_at: now(), selected_count: 1, omitted_count: 0, messages: [], next_cursor: null });
    }); const model = setup(fetch); await model.beginCreate();
    model.change({ selection: { mode: "individual", message_ids: [] } }); expect(model.policy()).toBeNull();
    model.change({ selection: { mode: "individual", message_ids: ["catalog-2"] } }); await model.preview();
    expect(bodies.at(-1)).toEqual({ operation: "create", snapshot_id: "snapshot-1", snapshot_version: "version-1", selection: { mode: "individual", message_ids: ["catalog-2"] }, include_future: false, show_tools: false, expires_in_days: 3 });
    model.change({ selection: { mode: "from", from_message_id: "" } }); expect(model.policy()).toBeNull(); model.dispose();
    const c = { total_messages: 101, messages: [{ message_id: "a" }, { message_id: "b" }] };
    expect([selectedCount(c, { mode: "all" }), selectedCount(c, { mode: "from", from_message_id: "b" }), selectedCount(c, { mode: "individual", message_ids: ["a", "b"] }), selectedCount(c, { mode: "from", from_message_id: "" })]).toEqual([101, 100, 2, null]);
  });
  it("discards a late catalog after the dialog's scope changes", async () => {
    let finish!: (res: Response) => void; let current = true;
    vi.stubGlobal("fetch", vi.fn<typeof globalThis.fetch>(() => new Promise((resolve) => { finish = resolve; })));
    const model = new SharingModel(new SharesClient("assistant-synthetic", "computer-synthetic", "session-synthetic", () => current), () => {});
    const opening = model.beginCreate(); current = false; finish(response(catalog)); await opening;
    expect(model.state.catalog).toBeNull(); expect(model.state.error).toBe("scope_changed"); model.dispose();
  });
});
