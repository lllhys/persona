// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initI18n, t } from "../../i18n";
import { credentialOwner, setToken } from "../../lib/api";
import { DOC_RULES, MESSAGE_RULES, type KBSelection } from "../../lib/kb-selection";
import { KnowledgeSelectionEditor } from "./KnowledgeSelection";

const events = vi.hoisted(() => new Map<string, (msg: Record<string, unknown>) => void>());
vi.mock("../../lib/store", () => ({ onHubEvent: (type: string, callback: (msg: Record<string, unknown>) => void) => { events.set(type, callback); return () => events.delete(type); } }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const fixture = (): KBSelection => ({
  version: 1,
  messages: { order: [["mentions", "mine"], ...MESSAGE_RULES.slice(2).map((n) => [n])], rules: Object.fromEntries(MESSAGE_RULES.map((n) => [n, { on: true, days: null, attachment_days: null, ...(n === "mentions" ? { include_all: false } : n === "mine" ? { context: 0 } : {}) }])) },
  docs: { order: [["mentions", "comment_mentions"], ["authored"], ["viewed"]], rules: Object.fromEntries(DOC_RULES.map((n) => [n, { on: true, days: null }])) },
  items: [{ source: "example-chat", kind: "chat", id: "chat/one", name: "Same name", read: true, store: false, days: null }],
  history: { "example-chat": { back_days: 90 }, "another-source": { back_days: null } },
});
let host: HTMLDivElement, root: Root;
let saved: KBSelection[];
let readFails: boolean, writeFails: boolean;
const response = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
const fetcher = async (input: RequestInfo | URL, init?: RequestInit) => {
  const url = new URL(String(input), "http://localhost");
  expect(new Headers(init?.headers).get("X-Persona-Manager")).toBeTruthy();
  if (init?.method === "PUT") {
    saved.push(JSON.parse(String(init.body)));
    return writeFails ? response({ error: "Read-only device" }, 403) : response(saved.at(-1));
  }
  if (url.pathname.endsWith("/kb/selection")) return readFails ? response({ error: "Selection unavailable" }, 503) : response(fixture());
  if (url.pathname.endsWith("/connectors")) return response({ connectors: [
    { id: "example-chat", name: "Example chats", capabilities: ["history"], manager: "assistant" },
    { id: "other-manager", name: "Wrong manager", capabilities: ["history"], manager: "other" },
    { id: "no-history", name: "No history", capabilities: ["archive"], manager: "assistant" },
  ] });
  if (url.pathname.endsWith("/kb/selection/candidates")) return response({ source: "example-chat", cached: true, chats: [
    { id: "chat/one", name: "Same name", source: "example-chat" },
    { id: "chat/two", name: "Same name", source: "example-chat" },
  ] });
  if (url.pathname.endsWith("/kb/docs")) return response({ items: [{ kind: "docs", id: "sanitized-file", name: "Same name" }, { kind: "docs", id: "legacy-file", name: "Legacy document" }] });
  if (url.pathname.endsWith("/kb/docs/sanitized-file")) return response({ detail: { id: "sanitized-file", meta: { id: "source/token:exact", source: "example-docs" } } });
  if (url.pathname.endsWith("/kb/docs/legacy-file")) return response({ detail: { id: "legacy-file", meta: {} } });
  throw new Error(`Unexpected request: ${url.pathname}`);
};
const flush = async () => { for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); }); };
const label = (name: string) => t(`settings-knowledge.${name}`);
const button = (name: string, scope: ParentNode = host) => [...scope.querySelectorAll("button")].find((b) => b.textContent === label(name))!;
const rule = (name = "messages/dm") => host.querySelector<HTMLElement>(`[data-selection-rule="${name}"]`)!;
const input = (name = "messages/dm") => rule(name).querySelector<HTMLInputElement>('input[type="number"]')!;
const type = (field: HTMLInputElement, value: string) => act(() => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(field, value);
  field.dispatchEvent(new Event("input", { bubbles: true }));
});
const click = async (name: string, scope?: ParentNode) => { await act(async () => button(name, scope).click()); await flush(); };
const render = async (manager = "assistant") => { await act(async () => root.render(<KnowledgeSelectionEditor key={`${credentialOwner()}/${manager}`} manager={manager} owner={credentialOwner()} />)); await flush(); };
beforeEach(async () => {
  await initI18n("en");
  setToken("fixture-token");
  saved = []; readFails = false; writeFails = false;
  vi.stubGlobal("fetch", vi.fn(fetcher));
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(async () => { await act(async () => root.unmount()); host.remove(); vi.unstubAllGlobals(); });

it("round-trips history, null days, optional rule fields and grouped priority through the real API adapter", async () => {
  await render();
  expect(rule().textContent).toContain(label("rule_messages_dm"));
  type(input(), "30");
  await click("save");
  const expected = fixture(); expected.messages.rules.dm.days = 30;
  expect(saved).toEqual([expected]);
  expect(host.textContent).toContain(label("saved"));
  expect(button("save").disabled).toBe(true);
});
it("never offers save or fabricated defaults after a failed or unsupported initial read", async () => {
  readFails = true; await render();
  expect(host.textContent).toContain("Selection unavailable");
  expect(button("save")).toBeUndefined();
  readFails = false; await click("reload");
  expect(button("save").disabled).toBe(true);
  expect(saved).toHaveLength(0);
});
it("keeps a denied write as an unsaved draft", async () => {
  await render(); writeFails = true;
  type(input(), "14"); await click("save");
  expect(host.textContent).toContain("Read-only device");
  expect(input().value).toBe("14");
  expect(button("save").disabled).toBe(false);
  expect(host.textContent).not.toContain(label("saved"));
});
it("blocks fractional/out-of-range days and context without a write", async () => {
  await render();
  for (const n of ["0", "36501", "1.5"]) { type(input(), n); expect(button("save").disabled).toBe(true); }
  type(input(), "");
  const context = rule("messages/mine").querySelector<HTMLInputElement>('input[max="200"]')!;
  type(context, "201"); expect(button("save").disabled).toBe(true);
  type(context, "200"); await click("save");
  expect(saved[0].messages.rules.mine.context).toBe(200);
  expect(saved[0].messages.rules.dm.days).toBeNull();
});
it("moves complete priority levels and splits/joins a rule without moving item overrides", async () => {
  await render();
  await click("split", rule("messages/mentions"));
  await click("join", rule("messages/mentions"));
  await click("up", host.querySelector('[data-selection-level="messages/1"]')!);
  await click("save");
  expect(saved[0].messages.order[0]).toEqual(["dm"]);
  expect(saved[0].messages.order[1]).toEqual(["mine", "mentions"]);
  expect(saved[0].messages.order.flat().sort()).toEqual([...MESSAGE_RULES].sort());
  expect(saved[0].items).toEqual(fixture().items);
});
it("uses candidate source/id, filters other managers and distinguishes equal names", async () => {
  await render(); await click("add_chat");
  const select = host.querySelector<HTMLSelectElement>("select")!;
  expect([...select.options].map((o) => o.value)).toEqual(["", "example-chat"]);
  await act(async () => { select.value = "example-chat"; select.dispatchEvent(new Event("change", { bubbles: true })); }); await flush();
  expect(host.textContent).toContain(label("cached"));
  expect(button("added").disabled).toBe(true);
  await click("add"); await click("save");
  expect(saved[0].items?.map((i) => i.id)).toEqual(["chat/one", "chat/two"]);
});
it("uses original document metadata rather than the KB filename and preserves different item semantics", async () => {
  await render(); await click("add_doc"); await click("add");
  expect(host.textContent).toContain(label("doc_rules"));
  expect(host.textContent).toContain(label("chat_personal"));
  expect(host.textContent).toContain(label("doc_days"));
  await click("save");
  expect(saved[0].items?.[1]).toEqual({ source: "example-docs", kind: "doc", id: "source/token:exact", name: "Same name", read: true, store: false, days: null });
});
it("distinguishes blank Store-all document retention from the chat text-retention fallback", async () => {
  const selection = fixture();
  selection.items![0].store = true;
  selection.items!.push({ source: "example-docs", kind: "doc", id: "document-token", name: "Example document", read: true, store: true, days: null });
  vi.stubGlobal("fetch", vi.fn(async () => response(selection)));
  await render();
  const items = host.querySelectorAll('[data-selection-item]');
  expect(items[0].textContent).toContain(label("store_all"));
  expect(items[1].textContent).toContain("Blank days follows document rules and creates no default deletion deadline.");
  expect(items[1].textContent).not.toContain(label("store_all"));
});
it("refuses a document with missing stable source identity", async () => {
  await render(); await click("add_doc");
  const row = [...host.querySelectorAll("div")].find((e) => e.textContent === "Legacy documentlegacy-fileAdd")!;
  await click("add", row); expect(host.textContent).toContain(label("missing_identity"));
  expect(button("save").disabled).toBe(true);
});
it("stops storing when Read is disabled and removing an override restores rule behavior", async () => {
  await render();
  const item = host.querySelector('[data-selection-item]')!;
  const switches = item.querySelectorAll<HTMLButtonElement>('[role="switch"]');
  await act(async () => switches[1].click());
  await act(async () => switches[0].click());
  await click("save");
  expect(saved[0].items?.[0]).toMatchObject({ read: false, store: false });
  await click("remove"); await click("save"); expect(saved[1].items).toEqual([]);
});
it("blocks a dirty draft on observable current-manager changes but ignores other managers", async () => {
  await render(); type(input(), "10");
  await act(async () => events.get("kb_selection")!({ manager: "other" }));
  expect(button("save").disabled).toBe(false);
  await act(async () => events.get("kb_selection")!({}));
  expect(button("save").disabled).toBe(true);
  expect(host.textContent).toContain(label("external_change"));
  await click("discard_reload"); expect(input().value).toBe("");
});
it("ignores a late GET after manager replacement", async () => {
  let resolve!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn((url, init) => new Headers(init?.headers).get("X-Persona-Manager") === "assistant" ? new Promise<Response>((done) => { resolve = done; }) : fetcher(url, init)));
  await render(); await render("second");
  const old = fixture(); old.messages.rules.dm.days = 999;
  await act(async () => resolve(response(old))); await flush();
  expect(input().value).toBe("");
});
it("ignores a late write receipt after credential replacement", async () => {
  await render();
  let resolve!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn((url, init) => init?.method === "PUT" ? new Promise<Response>((done) => { resolve = done; }) : fetcher(url, init)));
  type(input(), "10");
  await act(async () => button("save").click());
  setToken("replacement-fixture-token"); await render();
  const old = fixture(); old.messages.rules.dm.days = 10;
  await act(async () => resolve(response(old))); await flush();
  expect(input().value).toBe("");
  expect(host.textContent).not.toContain(label("saved"));
});
it("ignores older source candidates after a newer source selection", async () => {
  await render();
  let resolve!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn((url, init) => String(url).includes("candidates") ? new Promise<Response>((done) => { resolve = done; }) : fetcher(url, init)));
  await click("add_chat");
  const select = host.querySelector<HTMLSelectElement>("select")!;
  await act(async () => { select.value = "example-chat"; select.dispatchEvent(new Event("change", { bubbles: true })); });
  await act(async () => { select.value = ""; select.dispatchEvent(new Event("change", { bubbles: true })); });
  await act(async () => resolve(response({ chats: [{ id: "late", name: "Retired source", source: "example-chat" }] }))); await flush();
  expect(host.textContent).not.toContain("Retired source");
});
it("ignores late document detail after closing its picker", async () => {
  await render(); await click("add_doc");
  let resolve!: (r: Response) => void;
  vi.stubGlobal("fetch", vi.fn(() => new Promise<Response>((done) => { resolve = done; })));
  await act(async () => button("add").click());
  await click("add_doc");
  await act(async () => resolve(response({ detail: { meta: { source: "example-docs", id: "late-document" } } }))); await flush();
  expect(host.textContent).not.toContain("late-document");
  expect(button("save").disabled).toBe(true);
});
it("serializes saves and does not erase an observable change while a write is pending", async () => {
  await render(); type(input(), "10");
  let resolve!: (r: Response) => void;
  const pending = vi.fn((url, init) => init?.method === "PUT" ? new Promise<Response>((done) => { resolve = done; }) : fetcher(url, init));
  vi.stubGlobal("fetch", pending);
  await act(async () => { button("save").click(); button("save").click(); });
  expect(pending).toHaveBeenCalledTimes(1);
  expect(input().disabled || input().closest("fieldset")?.disabled).toBe(true);
  await act(async () => events.get("kb_selection")!({ manager: "assistant" }));
  const next = fixture(); next.messages.rules.dm.days = 10;
  await act(async () => resolve(response(next))); await flush();
  expect(host.textContent).toContain(label("external_change"));
  expect(button("save").disabled).toBe(true);
});
it("clears its own change echo only after confirming the saved document through GET", async () => {
  await render(); type(input(), "10");
  let canonical = fixture();
  vi.stubGlobal("fetch", vi.fn(async (_url, init) => {
    if (init?.method === "PUT") {
      canonical = JSON.parse(String(init.body));
      events.get("kb_selection")!({ manager: "assistant" });
      return response(canonical);
    }
    return response(canonical);
  }));
  await click("save");
  expect(host.textContent).not.toContain(label("external_change"));
  expect(host.textContent).toContain(label("saved"));
  type(input(), "11"); expect(button("save").disabled).toBe(false);
});
