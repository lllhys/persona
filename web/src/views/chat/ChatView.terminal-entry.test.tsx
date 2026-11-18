// @vitest-environment jsdom
// The way to an agent's terminal from its chat (#7343): on its page the terminal button switches in place; in the side
// panel (a task's or an Agent's), which shows no terminal in place, the same button opens the agent's page in its
// terminal view. While the conversation is shown, the button carries the attention dot when the terminal waits on a
// dialog: herdr calls the agent blocked, or a send here was refused with dialog_open.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  globalThis.IntersectionObserver ??= class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } } as unknown as typeof IntersectionObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
vi.mock("../../lib/store", async (load) => ({
  ...(await load<typeof import("../../lib/store")>()),
  listenSession: () => () => {},
}));
vi.mock("../../lib/panel", async (load) => ({ ...(await load<typeof import("../../lib/panel")>()), usePanel: () => null }));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }) }));

import { applyCompleteSessions, reset, setOutbox } from "../../lib/store";
import type { Session } from "../../lib/types";
import { t } from "../../i18n";
import { ChatView } from "./ChatView";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const row = (x: Partial<Session> = {}): Session => ({ machine_id: "mac", session_id: "s1", title: "Checks", cwd: "/code", agent: "codex", status: "idle", state: "idle", ...x }) as Session;
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  localStorage.clear();
  vi.stubGlobal("fetch", vi.fn((url: string) => Promise.resolve(new Response(JSON.stringify(String(url).includes("/items") ? { items: [], to_seq: 0 } : {}), { status: 200, headers: { "Content-Type": "application/json" } }))));
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

async function open(view: React.ReactElement, session = row()) {
  act(() => applyCompleteSessions([session]));
  act(() => root.render(view));
  for (let i = 0; i < 6; i++) await settle();
}
const terminalButton = () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-chatview.terminal")}"]`);
const dot = () => terminalButton()?.parentElement?.querySelector("[data-terminal-waits]") ?? null;
async function menuItems(): Promise<string[]> {
  act(() => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-chatview.more_actions")}"]`)!.click());
  await settle();
  const items = [...document.querySelectorAll<HTMLElement>('[role="menu"] [role="menuitem"]')].map((x) => x.textContent ?? "");
  act(() => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" })));
  return items;
}

it("on the agent's page: the terminal button and ⋯ › Terminal, no dot while nothing waits", async () => {
  await open(<ChatView mid="mac" sid="s1" inPage />);
  expect(terminalButton()).not.toBeNull();
  expect(dot()).toBeNull();
  expect((await menuItems()).some((x) => x.includes(t("chat-chatview.terminal")))).toBe(true);
});

it("in the side panel: the terminal button opens the agent's page in its terminal view and closes the panel", async () => {
  const router = await import("../../lib/router");
  const navigate = vi.spyOn(router, "navigate").mockImplementation(() => {});
  const onClose = vi.fn();
  try {
    await open(<ChatView mid="mac" sid="s1" panel={{ onClose, items: [] }} />);
    expect(terminalButton()).not.toBeNull();
    expect((await menuItems()).some((x) => x.includes(t("chat-chatview.terminal")))).toBe(true);
    act(() => terminalButton()!.click());
    expect(navigate).toHaveBeenCalledWith({ name: "session", mid: "mac", sid: "s1", view: "term" });
    expect(onClose).toHaveBeenCalled();
  } finally {
    navigate.mockRestore();
  }
});

it("an agent herdr calls blocked lights the terminal button, on its page and in the panel", async () => {
  await open(<ChatView mid="mac" sid="s1" inPage />, row({ status: "blocked", state: "needs_you" }));
  expect(dot()).not.toBeNull();
  act(() => root.render(<ChatView key="panel" mid="mac" sid="s1" panel={{ onClose: () => {}, items: [] }} />));
  for (let i = 0; i < 6; i++) await settle();
  expect(dot()).not.toBeNull();
});

it("a send refused because a dialog has the terminal lights the terminal button", async () => {
  await open(<ChatView mid="mac" sid="s1" inPage />);
  expect(dot()).toBeNull();
  act(() => setOutbox("mac/s1", [{ id: "m1", text: "go on", images: [], at: Date.now(), state: "failed", error: "x", code: "dialog_open" }]));
  await settle();
  expect(dot()).not.toBeNull();
});

it("an offline agent, or a chat that cannot show a terminal, offers none in the panel", async () => {
  await open(<ChatView mid="mac" sid="s1" panel={{ onClose: () => {}, items: [] }} />, row({ status: "offline", state: "offline", capabilities: { read: true, terminal: false } as Session["capabilities"] }));
  expect(terminalButton()).toBeNull();
});
