// @vitest-environment jsdom
// The leader conversations (#3623): the hub's `assistant.leaders` come first among the sidebar's Persona chats, in the
// hub's order, pinned on any browser (none of this browser's pins needed), then this browser's pins, then the recent ones.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
import { t } from "../i18n";
import { refresh, reset } from "../lib/store";
import type { Session } from "../lib/types";
import { Sidebar } from "./Sidebar";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const conversation = (session_id: string, title: string, updated_at: string): Session => ({
  machine_id: "hub", session_id, title, cwd: "/persona", status: "idle", state: "idle", assistant: true, origin: "user", updated_at,
});
const conversations = [
  conversation("main", "Persona", "2026-11-10T09:00:00Z"),
  conversation("newest", "Trip plan", "2026-11-10T08:00:00Z"),
  conversation("mine", "Release notes", "2026-11-01T00:00:00Z"),
  conversation("lead-b", "Payments", "2026-11-02T00:00:00Z"),
  conversation("lead-a", "Growth", "2026-11-03T00:00:00Z"),
];
let leaders: string[] | undefined;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const rowIds = () => [...host.querySelectorAll<HTMLAnchorElement>('a[href^="/persona/"]')].map((a) => a.getAttribute("href")!.slice("/persona/".length)).filter((id) => id !== "new");
const row = (sid: string) => host.querySelector<HTMLAnchorElement>(`a[href="/persona/${sid}"]`)!;

beforeEach(() => {
  localStorage.clear();
  leaders = ["lead-a", "lead-b"];
  vi.stubGlobal("fetch", vi.fn((url: string) => {
    const path = new URL(url, "http://x").pathname;
    const body = path === "/api/v1/assistant" ? { machine_id: "hub", main: "main", leaders, conversations } : path === "/api/v1/machines" ? { machines: [{ id: "hub", name: "Hub", online: true }] } : {};
    return Promise.resolve(new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } }));
  }));
  history.replaceState(null, "", "/overview");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.unstubAllGlobals();
});

async function renderSidebar() {
  await act(() => refresh());
  act(() => root.render(<Sidebar route={{ name: "home" }} onSearch={() => {}} onNewAgent={() => {}} />));
  await flush();
}

describe("the leader conversations in the sidebar", () => {
  it("come first in the hub's order, pinned and labeled, with no pin of this browser's", async () => {
    await renderSidebar();
    expect(rowIds()).toEqual(["lead-a", "lead-b", "newest", "mine"]);
    expect(host.textContent).toContain(t("sidebar.pinned_section"));
    for (const id of ["lead-a", "lead-b"]) {
      expect(row(id).textContent).toContain(t("sidebar.leader"));
      expect(row(id).querySelector("svg")).not.toBeNull();
    }
    expect(row("newest").textContent).not.toContain(t("sidebar.leader"));
    // nothing pinned in this browser: the heading has no Unpin all
    expect(host.querySelector(`button[aria-label="${t("conversation-menu.unpin_all")}"]`)).toBeNull();
  });

  it("put this browser's pins after them", async () => {
    localStorage.setItem("assistant.pinned", '["mine", "lead-b"]');
    await renderSidebar();
    expect(rowIds()).toEqual(["lead-a", "lead-b", "mine", "newest"]);
    expect(row("mine").textContent).not.toContain(t("sidebar.leader"));
    expect(host.querySelector(`button[aria-label="${t("conversation-menu.unpin_all")}"]`)).not.toBeNull();
  });

  it("leave the list as before when the hub has none", async () => {
    leaders = undefined;
    localStorage.setItem("assistant.pinned", '["mine"]');
    await renderSidebar();
    expect(rowIds()).toEqual(["mine", "newest", "lead-a", "lead-b"]);
    expect(host.textContent).not.toContain(t("sidebar.leader"));
  });

  it("have no Pin / Unpin in their menu, while a local pin keeps its Unpin", async () => {
    localStorage.setItem("assistant.pinned", '["mine"]');
    await renderSidebar();
    const menuOf = async (sid: string) => {
      act(() => row(sid).dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, clientX: 10, clientY: 10 })));
      await flush();
      const items = [...document.querySelectorAll('[role="menu"] [role="menuitem"]')].map((el) => el.textContent ?? "");
      act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
      await flush();
      return items;
    };
    const lead = await menuOf("lead-a");
    expect(lead.some((x) => x.includes(t("conversation-menu.rename")))).toBe(true);
    expect(lead.some((x) => x.includes(t("conversation-menu.unpin")) || x.includes(t("conversation-menu.pin")))).toBe(false);
    const mine = await menuOf("mine");
    expect(mine.some((x) => x.includes(t("conversation-menu.unpin")))).toBe(true);
  });
});
