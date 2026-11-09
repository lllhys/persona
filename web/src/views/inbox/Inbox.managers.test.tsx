// @vitest-environment jsdom
// The inbox with several managers (docs/assistant/managers.md "Clients"): each manager's group keeps its own sections open or
// closed, a route opens the copy of its manager, the Muted and Rules dialogs list each manager's under its name, and
// a rule from another manager's item is read and saved with that manager.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

const calls = vi.hoisted(() => ({
  list: [] as { path: string; method: string; header?: string; json?: unknown }[], items: [] as unknown[],
  roster: { default: "assistant", managers: [{ id: "assistant", name: "助手", online: true, builtin: true }, { id: "m-1", name: "工作", online: true }] },
}));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { method?: string; json?: unknown; headers?: Record<string, string> }) => {
    const header = init?.headers?.["X-Persona-Manager"];
    calls.list.push({ path, method: init?.method ?? "GET", header, json: init?.json });
    if (path.startsWith("/inbox?")) return Promise.resolve({ items: calls.items });
    if (path === "/mail/accounts") return Promise.resolve({ accounts: [] });
    if (path === "/managers") return Promise.resolve(calls.roster);
    if (path === "/inbox/mutes") return Promise.resolve({ mutes: header === "m-1" ? [{ id: "u1", source: "lark", tag: "chat", value: "oc_w", name: "Work group" }] : [{ id: "u2", source: "lark", tag: "chat", value: "oc_h", name: "Home group" }] });
    if (path === "/inbox/policies" && (init?.method ?? "GET") === "GET") return Promise.resolve({ policies: header === "m-1" ? [{ id: "p1", source: "lark", tag: "chat", value: "oc_w", name: "Work group", tier: "noise" }] : [] });
    if (path.startsWith("/inbox/policies")) return Promise.resolve({ policy: { id: "p1", source: "lark", tag: "chat", value: "oc_w", name: "Work group", tier: "fyi" } });
    return Promise.resolve({});
  },
}));
vi.mock("../../lib/store", async (orig) => {
  const m = await orig<typeof import("../../lib/store")>();
  return { ...m, useStore: <T,>(sel: (s: ReturnType<typeof m.getState>) => T) => m.useStore((s) => sel({ ...s, loaded: true })) };
});

import { resetInboxForTests, type InboxItem } from "../../lib/inbox";
import { resetManagerChoice } from "../../lib/manager-choice";
import { setRoster } from "../../lib/managers";
import { routedItem } from "./Inbox";
import { InboxList } from "./InboxList";
import { openPolicyDialog, PolicyDialogHost } from "./PolicyDialog";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const ROSTER = calls.roster;
const chat = { tag: "chat", value: "oc_w", name: "Work group" };
const item = (id: string, manager: string, patch: Partial<InboxItem> = {}): InboxItem => ({
  id, manager, source: "lark", kind: "group", account: "feishu", account_name: "Lark", title: `${manager} ${id}`, preview: "text", count: 1,
  updated_at: "2026-11-01T09:00:00Z", tier: "action", lark: { feed_id: "f1", is_group: true }, mutes: [chat], ...patch,
});

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};

beforeEach(() => {
  calls.list = [];
  calls.items = [item("lark:f1:1", "assistant"), item("lark:f1:1", "m-1"), item("lark:f2:1", "m-1", { tier: "fyi" }), item("lark:f3:1", "assistant", { tier: "fyi" })];
  resetInboxForTests();
  resetManagerChoice();
  setRoster(ROSTER);
  vi.stubGlobal("CSS", { escape: (v: string) => v });
  Element.prototype.scrollIntoView = vi.fn();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  setRoster(null);
  resetManagerChoice();
  vi.unstubAllGlobals();
});

const sections = () => [...host.querySelectorAll<HTMLElement>("[data-section]")].map((s) => [s.dataset.section, s.querySelector("button[aria-expanded]")?.getAttribute("aria-expanded") ?? "always"]);
const clickText = async (text: string) => {
  const b = [...document.body.querySelectorAll<HTMLElement>("button, [role=menuitem]")].find((x) => x.getAttribute("aria-label") === text || x.textContent?.trim() === text || x.textContent?.includes(text))!;
  await act(async () => b.click());
  await flush();
};

describe("the inbox of several managers", () => {
  it("keeps each manager's sections open or closed on their own", async () => {
    act(() => root.render(<InboxList />));
    await flush();
    expect(sections()).toEqual([["action", "true"], ["fyi", "true"], ["action", "true"], ["fyi", "true"]]);
    // close the first group's FYI (the default's): the other group's stays open
    const fyi = host.querySelectorAll<HTMLElement>('[data-section="fyi"] button[aria-expanded]')[0];
    act(() => fyi.click());
    expect(sections()).toEqual([["action", "true"], ["fyi", "false"], ["action", "true"], ["fyi", "true"]]);
  });

  it("opens the copy of the manager the route names; a route without one opens the default's", () => {
    const items = calls.items as InboxItem[];
    expect(routedItem(items, "lark:f1:1", "m-1")?.manager).toBe("m-1");
    expect(routedItem(items, "lark:f1:1", undefined)?.manager).toBe("assistant");
    expect(routedItem(items, "lark:f2:1", undefined)?.manager).toBe("m-1"); // only m-1 has it
    expect(routedItem(items, "lark:f3:1", "m-1")).toBeUndefined();
  });

  it("lists each manager's mutes under its name, and unmutes with it", async () => {
    act(() => root.render(<InboxList />));
    await flush();
    await clickText("更多操作");
    await clickText("已静音");
    const groups = [...document.body.querySelectorAll<HTMLElement>("[data-mutes-manager]")];
    expect(groups.map((g) => g.dataset.mutesManager)).toEqual(["assistant", "m-1"]);
    expect(groups[1].textContent).toContain("工作");
    expect(groups[1].textContent).toContain("Work group");
    // each read names its manager, the default's too: what it answers is stamped with the one asked
    expect(calls.list.filter((c) => c.path === "/inbox/mutes").map((c) => c.header)).toEqual(["assistant", "m-1"]);
    const unmute = groups[1].querySelector("button")!;
    await act(async () => unmute.click());
    await flush();
    expect(calls.list.find((c) => c.path === "/inbox/mutes/u1")).toMatchObject({ method: "DELETE", header: "m-1" });
  });

  it("reads and saves a rule of another manager's item with that manager", async () => {
    act(() => root.render(<PolicyDialogHost />));
    act(() => openPolicyDialog({ item: item("lark:f1:1", "m-1"), option: chat, source: "lark" }));
    await flush();
    expect(calls.list.find((c) => c.path === "/inbox/policies")?.header).toBe("m-1");
    await clickText("保存");
    expect(calls.list.find((c) => c.path === "/inbox/policies/p1")).toMatchObject({ method: "PATCH", header: "m-1" });
  });
});
