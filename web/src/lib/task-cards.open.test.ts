import { describe, expect, it } from "vitest";
import type { TaskPlan } from "./feed";
import type { Supervision } from "./supervision";
import { cardBucket, type TaskCardMetadata } from "./task-cards";
import { todoBucket, triageTodos } from "./todo-triage";
import type { Todo } from "./todos";
import { assistantEntries } from "./assistant-entries";

// #3331: the conversation's In progress entry and the To-dos page's "Not done" read one rule (todoBucket).
const NOW = Date.parse("2026-11-10T08:00:00Z");
const hoursAgo = (h: number) => new Date(NOW - h * 3600_000).toISOString();
const at = hoursAgo(1);
const card = (extra: Partial<TaskCardMetadata>): TaskCardMetadata => ({ id: "slot", at, updated_at: at, state: "not_started", ...extra });
const todo = (extra: Partial<Todo> = {}): Todo => ({ id: "t", title: "Demo", origin: "c", created_at: at, updated_at: at, ...extra });
const plan = (id: string, state: TaskPlan["state"], conversation = "c", n = 1): TaskPlan => ({ id, conversation, state, created_at: at, tasks: Array.from({ length: n }, (_, i) => ({ title: `Task ${i}`, prompt: "p", cwd: "/demo", machine: "demo" })) });
const accepted = (): Supervision => ({ state: "accepted", updated_at: hoursAgo(2), reviews: [{ at: hoursAgo(2), verdict: "accepted", checks: [] }] } as unknown as Supervision);
const noSessions = () => undefined;

describe("cardBucket", () => {
  it("a task is open until it is closed", () => {
    expect(cardBucket(card({ todo_id: "t" }), todo(), {}, noSessions, NOW)).toBe("open");
    expect(cardBucket(card({ todo_id: "t" }), todo({ done_at: at }), {}, noSessions, NOW)).toBe("closed");
    // the hub no longer sends it: closed
    expect(cardBucket(card({ todo_id: "t" }), undefined, {}, noSessions, NOW)).toBe("closed");
  });

  it("shelved: a task with no activity for 48 hours and nothing running is not counted", () => {
    const stale = todo({ created_at: hoursAgo(72), updated_at: hoursAgo(49) });
    expect(cardBucket(card({ todo_id: "t" }), stale, {}, noSessions, NOW)).toBe("shelved");
    // 47 hours is still open
    expect(cardBucket(card({ todo_id: "t" }), todo({ created_at: hoursAgo(72), updated_at: hoursAgo(47) }), {}, noSessions, NOW)).toBe("open");
    // a restore from Shelved counts as a move
    expect(cardBucket(card({ todo_id: "t" }), stale, {}, noSessions, NOW, NOW - 3600_000)).toBe("open");
    // the user's own to-do without agents or origin never shelves itself
    expect(todoBucket(todo({ origin: undefined, created_at: hoursAgo(72), updated_at: hoursAgo(49) }), noSessions, NOW)).toBe("open");
  });

  it("accepted: a task_review accepted is finished, even before the hub closes it", () => {
    expect(cardBucket(card({ todo_id: "t" }), todo({ supervision: accepted() }), {}, noSessions, NOW)).toBe("closed");
    expect(cardBucket(card({ todo_id: "t" }), todo({ done_at: at, supervision: accepted() }), {}, noSessions, NOW)).toBe("closed");
  });

  it("a plan waiting for Start is Needs you's, never In progress'", () => {
    const plans = { open: plan("open", "open"), gone: plan("gone", "dismissed") };
    expect(cardBucket(card({ plan_id: "open", index: 0 }), undefined, plans, noSessions, NOW)).toBe("start");
    expect(cardBucket(card({ plan_id: "gone", index: 0 }), undefined, plans, noSessions, NOW)).toBe("closed");
    expect(cardBucket(card({ plan_id: "missing", index: 0 }), undefined, plans, noSessions, NOW)).toBe("closed");
  });
});

// In progress (#3387) is the open tasks that do not wait on the user; none of these do, so it is the page's open number.
const entryRows = (conversation: string, todos: Todo[], plans: Record<string, TaskPlan>, main?: string) => {
  const got = assistantEntries({ conversation, main, todos, sessions: [], interactions: [], plans, feed: [], lookup: noSessions, now: NOW });
  const ids = (items: typeof got.running) => items.map((item) => (item.kind === "task" ? item.todo.id : item.key));
  return { open: ids(got.running), shelved: ids(got.shelved), needsYou: got.needsYou.map((item) => item.key) };
};

describe("In progress counts what the To-dos page counts", () => {
  const todos: Todo[] = [
    todo({ id: "open" }),
    todo({ id: "shelved", created_at: hoursAgo(80), updated_at: hoursAgo(60) }),
    todo({ id: "accepted", supervision: accepted() }),
    todo({ id: "done", done_at: hoursAgo(3) }),
    todo({ id: "manual", origin: undefined }),
    todo({ id: "elsewhere", origin: "other-chat" }),
  ];
  const plans = { p: plan("p", "open", "main", 2) };

  it("the main conversation: every to-do, the same open number, shelved folded, plans not counted", () => {
    const main = todos.map((td) => (td.origin === "c" ? { ...td, origin: "main" } : td));
    const rows = entryRows("main", main, plans, "main");
    const page = triageTodos(main, noSessions, NOW);
    expect(rows.open.length).toBe(page.open);
    expect([...rows.open].sort()).toEqual(["elsewhere", "manual", "open"]);
    expect(rows.shelved).toEqual(["shelved"]);
    expect(rows.shelved.length).toBe(page.shelved);
    // the finished ones are in neither entry; the plan waiting for Start is one Needs you row, not its two tasks
    expect(rows.needsYou).toEqual(["plan:p"]);
  });

  it("another conversation counts only the tasks it handed out, by the same rule", () => {
    const rows = entryRows("c", todos, {});
    expect(rows.open).toEqual(["open"]);
    expect(rows.shelved).toEqual(["shelved"]);
  });
});
