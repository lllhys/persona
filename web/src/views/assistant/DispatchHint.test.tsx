// @vitest-environment jsdom
// The line under a reply that dispatched work (#3700): it opens the dispatched task's Agent in the real side panel, never the
// whole In progress tray.
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  globalThis.IntersectionObserver ??= class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } } as unknown as typeof IntersectionObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

import { t } from "../../i18n";
import { getFeedState, type FeedItem, type TaskPlan } from "../../lib/feed";
import { closePanel, getPanel } from "../../lib/panel";
import { href } from "../../lib/router";
import { applyCompleteSessions, refreshTodos, reset } from "../../lib/store";
import type { Todo } from "../../lib/todos";
import type { Session } from "../../lib/types";
import { SidePanel } from "../SidePanel";
import { AssistantActionBar, FeedLineView } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const at = "2026-11-10T08:00:00Z";
const conversation = "dispatch-demo";
const agent: Session = { machine_id: "example-computer", session_id: "example-agent", title: "Example Agent", cwd: "/example", state: "working", status: "working", todo: { id: "example-task", title: "Example task" } } as Session;
const linked = (id: string, title: string, sessions: Session[]): Todo => ({
  id, title, origin: conversation, created_at: at, updated_at: at,
  agents: sessions.map((s) => ({ machine_id: s.machine_id, session_id: s.session_id, title: s.title, linked_at: at })),
});
let served: Todo[];
let host: HTMLDivElement;
let root: Root;
const settle = () => act(async () => { await new Promise((r) => setTimeout(r, 0)); });

beforeEach(() => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => new Response(JSON.stringify(String(url).includes("/todos") ? { todos: served } : String(url).includes("/items") ? { items: [], to_seq: 0 } : {}), { headers: { "Content-Type": "application/json" } })));
  vi.spyOn(window, "open").mockReturnValue(null);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  closePanel();
  reset();
  for (const id of Object.keys(getFeedState().plans)) delete getFeedState().plans[id];
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

async function show(item: FeedItem, todos: Todo[], sessions: Session[]) {
  served = todos;
  await act(async () => {
    applyCompleteSessions(sessions);
    await refreshTodos();
  });
  act(() => root.render(<StrictMode><AssistantActionBar conversation={conversation} /><FeedLineView item={item} simplified /><SidePanel /></StrictMode>));
  await settle();
}
async function click(el: HTMLElement) {
  act(() => el.click());
  for (let i = 0; i < 6; i++) await settle();
}
const tray = () => host.querySelector<HTMLButtonElement>('[data-tray-entry="running"]')!;

describe("Dispatched line", () => {
  it("one dispatched task: names it and opens its Agent's chat in the side panel, not the In progress tray", async () => {
    await show({ id: "task-event", conversation, at, kind: "task", ref: "example-task" }, [linked("example-task", "Example task", [agent])], [agent]);
    const hint = host.querySelector<HTMLButtonElement>("[data-dispatch-hint]")!;
    expect(hint.textContent).toBe(`${t("assistant-tasks.dispatch_single", { title: "Example task" })}· ${t("assistant-tasks.dispatch_open")}`);
    expect(hint.textContent).not.toContain(t("assistant-tasks.tray_running"));
    await click(hint);
    expect(getPanel()).toEqual({ kind: "task", todo: "example-task" });
    // the panel is that Agent's own chat (ChatView), titled by the task, as a click on its In progress row gives
    const aside = host.querySelector("aside")!;
    expect(aside.querySelector("header h1")?.textContent).toBe("Example task");
    expect(aside.querySelector("[data-chat-scroll]")).not.toBeNull();
    act(() => aside.querySelector<HTMLButtonElement>(`header button[aria-label="${t("chat-chatview.more")}"]`)!.click());
    await settle();
    const newWindow = [...document.querySelectorAll<HTMLElement>('[role="menuitem"]')].find((el) => el.textContent?.includes(t("common.open_in_new_window")))!;
    act(() => newWindow.click());
    expect(window.open).toHaveBeenLastCalledWith(href({ name: "session", mid: agent.machine_id, sid: agent.session_id }), "_blank", "noopener");
    // and the In progress tray stayed closed
    expect(tray().getAttribute("aria-expanded")).toBe("false");
    expect(host.querySelector("section[aria-label]")).toBeNull();
  });

  it("one dispatched task whose Agent has not started: opens the task card in the side panel", async () => {
    await show({ id: "task-event", conversation, at, kind: "task", ref: "example-task" }, [linked("example-task", "Example task", [])], []);
    await click(host.querySelector<HTMLButtonElement>("[data-dispatch-hint]")!);
    const aside = host.querySelector("aside")!;
    expect(getPanel()).toEqual({ kind: "task", todo: "example-task" });
    // the task card's header (PanelHeader), no Agent chat yet
    expect(aside.querySelector("header")?.textContent).toContain("Example task");
    expect(aside.querySelector("[data-chat-scroll]")).toBeNull();
    expect(tray().getAttribute("aria-expanded")).toBe("false");
  });

  it("several dispatched tasks: each title opens its own task, and only those tasks are listed", async () => {
    const other: Session = { ...agent, session_id: "other-agent", title: "Other Agent" };
    const plan: TaskPlan = { id: "plan-many", conversation, state: "started", created_at: at, tasks: [
      { title: "Example task", prompt: "a", cwd: "/example", machine: "example-computer", todo_id: "example-task" },
      { title: "Other task", prompt: "b", cwd: "/example", machine: "example-computer", todo_id: "other-task" },
    ] };
    getFeedState().plans[plan.id] = plan;
    // an unrelated running task of the same conversation is in In progress, not in this line
    await show({ id: "plan-event", conversation, at, kind: "plan", ref: plan.id },
      [linked("example-task", "Example task", [agent]), linked("other-task", "Other task", [other]), linked("unrelated", "Unrelated task", [])], [agent, other]);
    const hint = host.querySelector("[data-dispatch-hint]")!;
    expect(hint.textContent).toContain(t("assistant-tasks.dispatch_many", { count: 2 }));
    expect(hint.textContent).not.toContain("Unrelated task");
    const links = [...hint.querySelectorAll<HTMLButtonElement>("button[data-dispatched-task]")];
    expect(links.map((b) => b.textContent)).toEqual(["Example task", "Other task"]);
    await click(links[1]!);
    expect(getPanel()).toEqual({ kind: "task", todo: "other-task" });
    expect(host.querySelector("aside header h1")?.textContent).toBe("Other task");
    expect(tray().getAttribute("aria-expanded")).toBe("false");
  });
});
