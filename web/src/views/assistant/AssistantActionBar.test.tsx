// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { getFeedState, loadFeed, type FeedItem, type TaskPlan } from "../../lib/feed";
import { getPanel, closePanel } from "../../lib/panel";
import { putTodo, refresh, refreshTodos, reset, upsertSession } from "../../lib/store";
import type { Todo } from "../../lib/todos";
import { AssistantActionBar, DOCK_CAPSULE, FeedLineView, TaskDock } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const at = "2026-11-10T08:00:00Z";
const conversation = "tray-demo";
const plan: TaskPlan = { id: "tray-plan", conversation, state: "started", created_at: at, tasks: [{ title: "Demo task", prompt: "Demo checks", cwd: "/demo", machine: "demo", todo_id: "tray-task" }] };
let todo: Todo;
let host: HTMLDivElement;
let root: Root;
let writes: string[];
const feed: FeedItem[] = [
  { id: "tray-plan-event", conversation, at, kind: "plan", ref: plan.id },
  { id: "tray-task-event", conversation, at, kind: "task", ref: "tray-task" },
  { id: "tray-review-event", conversation, at, kind: "review", ref: "tray-task", body: { verdict: "accepted" } },
  { id: "tray-receipt", conversation, at, kind: "receipt", ref: "tray-task", body: { state: "done", summary: "Demo result" } },
];

beforeEach(async () => {
  writes = [];
  todo = { id: "tray-task", title: "Demo task", origin: conversation, plan_id: plan.id, created_at: at, updated_at: at, pr_url: "https://github.com/example/demo/pull/42", agents: [{ machine_id: "demo", session_id: "worker", linked_at: at }], supervision: { supervisor: conversation, progress: { kind: "progress", step: 2, steps: 5, text: "Run the checks", at } } };
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    const path = String(url);
    if (init?.method && init.method !== "GET") writes.push(path);
    const body = path.includes("/feed") ? { items: feed, plans: { [plan.id]: plan }, task_cards: [{ id: `plan:${plan.id}:0`, plan_id: plan.id, index: 0, todo_id: todo.id, at, updated_at: at, state: "accepted" }] } : path.includes("/todos") ? { todos: [todo] } : path.includes("/sessions") ? { sessions: [{ machine_id: "demo", session_id: "worker", title: "Demo agent", state: "working", status: "working", updated_at: at }] } : {};
    return new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
  }));
  await act(async () => {
    await refresh();
    await loadFeed(conversation);
    await refreshTodos();
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<StrictMode><AssistantActionBar conversation={conversation} />{feed.map((item) => <FeedLineView key={item.id} item={item} simplified />)}</StrictMode>));
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  closePanel();
  reset();
  delete getFeedState().plans[plan.id];
  delete getFeedState().feeds[conversation];
  delete getFeedState().taskCards[conversation];
  vi.unstubAllGlobals();
});
const entries = () => [...host.querySelectorAll<HTMLButtonElement>("[data-assistant-actionbar] [data-tray-entry]")];
const entry = (key: string) => host.querySelector<HTMLButtonElement>(`[data-assistant-actionbar] [data-tray-entry="${key}"]`)!;
const click = (label: string) => {
  const button = [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === label);
  expect(button).toBeDefined();
  act(() => button!.click());
};

describe("Assistant task progress trays", () => {
  it("keeps three entries in order, collapses by default and replaces conversation cards with one hint", () => {
    expect(entries().map((button) => button.textContent)).toEqual([`${t("assistant-tasks.tray_pending")}`, `${t("assistant-tasks.tray_progress")}1`, `${t("assistant-tasks.tray_agents")}1`]);
    expect(host.querySelector("section")).toBeNull();
    expect(host.querySelectorAll("[data-dispatch-hint]")).toHaveLength(1);
    expect(host.textContent).not.toContain("Demo result");
    act(() => (host.querySelector("[data-dispatch-hint]") as HTMLButtonElement).click());
    expect(host.querySelectorAll("[data-task-slot]")).toHaveLength(1);
    expect(host.querySelector('[role="progressbar"]')?.getAttribute("aria-valuenow")).toBe("2");
    expect(host.textContent).toContain("PR #42");
    expect(host.textContent).not.toContain(t("assistant-tasks.review_accepted"));
  });

  it("updates the same row for blocked and accepted states, and opens the original task detail", () => {
    act(() => (host.querySelector("[data-dispatch-hint]") as HTMLButtonElement).click());
    const row = host.querySelector("[data-task-slot]");
    act(() => putTodo({ ...todo, supervision: { ...todo.supervision!, progress: { kind: "blocked", step: 3, steps: 5, text: "Waiting for a decision" } } }));
    expect(host.querySelector("[data-task-slot]")).toBe(row);
    expect(host.querySelector('[role="progressbar"] .bg-orange')).not.toBeNull();
    act(() => putTodo({ ...todo, done_at: at, supervision: { ...todo.supervision!, state: "accepted" } }));
    expect(host.querySelector("[data-task-slot]")).toBe(row);
    expect(row?.textContent).toContain(t("assistant-tasks.review_accepted"));
    expect(row?.textContent).not.toContain("Waiting for a decision");
    expect(row?.textContent).not.toContain("Run the checks");
    expect(row?.querySelector('[role="progressbar"]')).toBeNull();
    act(() => (row as HTMLButtonElement).click());
    expect(getPanel()?.kind).toBe("task");
    expect(getPanel()).toMatchObject({ todo: todo.id });
  });

  it("does not relabel cancelled or auto-completed work as accepted", () => {
    act(() => (host.querySelector("[data-dispatch-hint]") as HTMLButtonElement).click());
    act(() => putTodo({ ...todo, done_at: at, done_by: "auto" }));
    expect(host.textContent).not.toContain(t("assistant-tasks.review_accepted"));
    act(() => putTodo({ ...todo, done_at: at, supervision: { ...todo.supervision!, state: "cancelled" } }));
    expect(host.textContent).toContain(t("assistant-tasks.review_cancelled"));
    expect(host.textContent).not.toContain(t("assistant-tasks.review_accepted"));
  });

  it("reuses round-bound owner acceptance and deduplicates its supervision decision", async () => {
    const firstRound = { outcome: "decide" as const, verdict: "Choose the rollout", recommend: "Use the demo rollout", asked_at: at };
    todo = { ...todo, ask: firstRound, supervision: { ...todo.supervision!, state: "needs_user" } };
    await act(async () => putTodo(todo));
    act(() => (entry("pending")).click());
    expect(host.querySelectorAll('[data-brief-row="accept"]')).toHaveLength(1);
    expect(host.textContent).not.toContain(t("assistant-tasks.review_needs_user"));
    const first = [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === t("assistant-brief.take"))!;
    const nextRound = "2026-11-10T08:20:00Z";
    await act(async () => {
      first.click();
      todo = { ...todo, ask: { ...firstRound, asked_at: nextRound } };
      putTodo(todo);
    });
    const second = [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === t("assistant-brief.take"))!;
    expect(second).not.toBe(first);
    await act(async () => second.click());
    const rounds = vi.mocked(fetch).mock.calls.filter(([url]) => String(url).endsWith("/accept")).map(([, init]) => JSON.parse(String(init?.body)).asked_at);
    expect(rounds).toEqual([at, nextRound]);
    expect(writes.filter((path) => path.includes("message"))).toEqual([]);
  });

  it("moves awaiting decisions and drafts to Pending without sending on tray/filter navigation", async () => {
    const draft: FeedItem = { id: "tray-draft", conversation, at, kind: "message", ref: "draft-ref", body: { draft: "Demo reply", text: "Demo message" } };
    getFeedState().feeds[conversation] = [...feed, draft];
    act(() => putTodo({ ...todo, supervision: { ...todo.supervision!, state: "needs_user" } }));
    const pending = entry("pending");
    act(() => pending.click());
    expect(host.querySelector('[data-testid="message-card"]')).not.toBeNull();
    expect(host.textContent).toContain(t("assistant-tasks.review_needs_user"));
    act(() => (entry("agents")).click());
    click(t("assistant-tasks.all"));
    expect(writes).toEqual([]);
    act(() => upsertSession({ machine_id: "demo", session_id: "worker", title: "Demo agent", cwd: "/demo", state: "unread", status: "unread", updated_at: at }));
    click(t("lib-state-labels.unread"));
    expect(host.textContent).toContain("Demo agent");
  });

  it("draws the entries as the dock's old All / task capsules, with the open tray selected", () => {
    const dockHost = document.createElement("div");
    document.body.append(dockHost);
    const dockRoot = createRoot(dockHost);
    act(() => dockRoot.render(<TaskDock conversation={conversation} />));
    const all = dockHost.querySelector<HTMLButtonElement>('[data-testid="task-dock-board"]')!;
    const capsule = DOCK_CAPSULE.split(" ");
    for (const button of [all, ...entries()]) for (const name of capsule) expect(button.classList).toContain(name);
    act(() => dockRoot.unmount());
    dockHost.remove();
    // no fixed-width grid or tall rounded rectangle left over from #2920
    expect(host.querySelector("[data-assistant-actionbar] .grid")).toBeNull();
    for (const button of entries()) expect(button.className).not.toMatch(/rounded-control|py-2\b|flex-1/);
    // the open tray is the selected capsule; aria-expanded / aria-controls still say which one
    expect(entries().map((button) => button.getAttribute("aria-expanded"))).toEqual(["false", "false", "false"]);
    act(() => entry("progress").click());
    expect(entry("progress").getAttribute("aria-expanded")).toBe("true");
    expect(entry("progress").classList).toContain("bg-ink");
    expect(document.getElementById(entry("progress").getAttribute("aria-controls")!)?.tagName).toBe("SECTION");
    expect(entry("pending").classList).not.toContain("bg-ink");
    act(() => entry("progress").click());
    expect(entry("progress").getAttribute("aria-expanded")).toBe("false");
    expect(host.querySelector("section")).toBeNull();
  });

  it("keeps an empty Pending quiet (grey) but still opens it", () => {
    const pending = entry("pending");
    expect(pending.disabled).toBe(false);
    expect(pending.classList).toContain("text-ink-3");
    expect(pending.querySelector("[aria-hidden]")?.classList).toContain("bg-ink-3");
    expect(entry("progress").querySelector("[aria-hidden]")?.classList).toContain("bg-accent");
    act(() => pending.click());
    expect(host.textContent).toContain(t("assistant-tasks.tray_empty_pending"));
  });
});
