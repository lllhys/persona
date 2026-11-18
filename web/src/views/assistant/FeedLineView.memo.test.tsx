// @vitest-environment jsdom
// The main chat re-renders on every streamed item and session push; a hub line whose item is the same object does not
// re-render with it, and still follows the plan and the todo it shows through its own selectors.
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FeedItem, TaskPlan } from "../../lib/feed";
import type { Todo } from "../../lib/todos";

const hub = vi.hoisted(() => ({ listeners: new Map<string, ((m: Record<string, unknown>) => void)[]>() }));
vi.mock("../../lib/store", async (orig) => ({
  ...(await orig<typeof import("../../lib/store")>()),
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
    hub.listeners.set(type, [...(hub.listeners.get(type) ?? []), l]);
    return () => {};
  },
}));
const todos = vi.hoisted(() => ({ list: [] as unknown[], machines: [] as unknown[], posts: [] as string[] }));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: async (path: string, init?: { method?: string }) => {
    if (init?.method === "POST") {
      todos.posts.push(path);
      const m = /^\/task-plans\/([^/]+)\/dismiss$/.exec(path);
      if (m) return { plan: { id: m[1], conversation: "c1", state: "dismissed", tasks: [{ title: "x", prompt: "", machine: "box", cwd: "/w" }] } };
    }
    return path === "/todos" ? { todos: todos.list } : path === "/machines" ? { machines: todos.machines } : {};
  },
}));
// A live card stands in for a line's subtree: each FeedLineView render of a view item renders it once.
const liveRenders = vi.hoisted(() => ({ n: 0 }));
vi.mock("../../ui/AssistantCard", async (orig) => ({
  ...(await orig<typeof import("../../ui/AssistantCard")>()),
  LiveCard: ({ id }: { id: string }) => {
    liveRenders.n++;
    return <div data-testid="live">{id}</div>;
  },
}));

import { refreshTodos } from "../../lib/store";
import { FeedLineView } from "./Tasks";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let bump: () => void = () => {};

/** The chat: re-renders on bump() and draws the same items each time. */
function Chat({ items }: { items: FeedItem[] }) {
  const [n, setN] = useState(0);
  bump = () => setN((x) => x + 1);
  return (
    <div data-n={n}>
      {items.map((it) => (
        <FeedLineView key={it.id} item={it} simplified />
      ))}
    </div>
  );
}

const now = new Date().toISOString();
const item = (patch: Partial<FeedItem>): FeedItem => ({ id: "f1", conversation: "c1", at: now, kind: "view", ref: "v1", ...patch });
const plan = (title: string, state: TaskPlan["state"] = "open"): TaskPlan => ({
  id: "p1", conversation: "c1", state, tasks: [{ title, prompt: "", machine: "box", cwd: "/w/app" }],
});
const todo = (title: string): Todo => ({ id: "t1", title, created_at: now, updated_at: now });
const emit = (type: string, m: Record<string, unknown>) => act(() => hub.listeners.get(type)?.forEach((l) => l(m)));

beforeEach(() => {
  liveRenders.n = 0;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("FeedLineView in the main chat", () => {
  it("does not re-render when the chat does and its item is the same", () => {
    const items = [item({})];
    act(() => root.render(<Chat items={items} />));
    expect(liveRenders.n).toBe(1);
    for (let i = 0; i < 5; i++) act(() => bump());
    expect(liveRenders.n).toBe(1);
    // an item whose body changed is a new object (mergeInto): that line draws again
    act(() => root.render(<Chat items={[{ ...items[0], body: { view: {} } }]} />));
    expect(liveRenders.n).toBe(2);
  });

  it("follows the plan it shows", () => {
    emit("task_plan", { plan: plan("补测试") });
    act(() => root.render(<Chat items={[item({ id: "f2", kind: "plan", ref: "p1" })]} />));
    // an open plan is its card where it was made, with Start / Edit / Dismiss, as on iOS (#8193)
    expect(host.textContent).toContain("补测试");
    expect(host.querySelector("[data-plan-actions]")?.querySelectorAll("button").length).toBe(3);
    act(() => bump());
    emit("task_plan", { plan: plan("补测试", "started") });
    expect(host.textContent).toContain("补测试"); // started: the hand-off card of its task
  });

  it("draws an open plan from the item's own body when the store has none (#8193)", () => {
    const body = { plan: { ...plan("整理周报"), id: "p9" } };
    act(() => root.render(<Chat items={[item({ id: "f4", kind: "plan", ref: "p9", body })]} />));
    expect(host.textContent).toContain("整理周报");
    expect(host.querySelector("[data-plan-actions]")).not.toBeNull();
  });

  it("ignores a body plan that is not the item's own", () => {
    const body = { plan: { ...plan("别的计划"), id: "other" } };
    act(() => root.render(<Chat items={[item({ id: "f5", kind: "plan", ref: "p8", body })]} />));
    expect(host.textContent).toBe("");
  });

  it("draws a dismissed plan as its one-line outcome, without actions", () => {
    emit("task_plan", { plan: { ...plan("不做了", "dismissed"), id: "p7" } });
    act(() => root.render(<Chat items={[item({ id: "f6", kind: "plan", ref: "p7" })]} />));
    expect(host.querySelector("[data-plan-actions]")).toBeNull();
    expect(host.textContent).not.toBe("");
  });

  it("acts on the plan from the chat's card: Dismiss goes to the hub and the card becomes its outcome", async () => {
    emit("task_plan", { plan: { ...plan("清理旧分支"), id: "p6" } });
    act(() => root.render(<Chat items={[item({ id: "f7", kind: "plan", ref: "p6" })]} />));
    const buttons = [...(host.querySelector("[data-plan-actions]")?.querySelectorAll("button") ?? [])];
    await act(async () => buttons.at(-1)!.click());
    expect(todos.posts).toContain("/task-plans/p6/dismiss");
    expect(host.querySelector("[data-plan-actions]")).toBeNull();
  });

  it("follows the todo its hand-off card shows", async () => {
    todos.list = [todo("修登录")];
    await act(() => refreshTodos());
    act(() => root.render(<Chat items={[item({ id: "f3", kind: "task", ref: "t1" })]} />));
    expect(host.textContent).toContain("修登录");
    act(() => bump());
    todos.list = [todo("修登录和注册")];
    await act(() => refreshTodos());
    expect(host.textContent).toContain("修登录和注册");
  });
});
