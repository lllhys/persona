// A task whose agent waits on its own background work or on a wake-up from the hub is Waiting, not Follow-up (#3274), and
// a task's agents are read from the list's rows too, not only from the chats opened on this page.
import { describe, expect, it } from "vitest";
import type { Session } from "./types";
import { agentWaits, makeSessionLookup, TODO_STATUS_INFO, todoStatus, waitingLine, type Todo } from "./todos";
import { taskSessionLookup, withTaskRows } from "./store";
import { boardSectionOf } from "./task-board";

const row = (sid: string, state: Session["state"], patch: Partial<Session> = {}): Session => ({
  machine_id: "m", session_id: sid, title: sid, cwd: "/code", status: state ?? "idle", state, updated_at: "2026-11-10T08:00:00Z", ...patch,
});

const task = (...sids: string[]): Todo => ({
  id: "t", title: "等 CI", created_at: "2026-11-10T08:00:00Z", updated_at: "2026-11-10T08:00:00Z",
  agents: sids.map((sid) => ({ machine_id: "m", session_id: sid, linked_at: "2026-11-10T08:00:00Z" })),
});

const running = row("run", "working");
const background = row("bg", "working", { background_working: true, work: { phase: "blocked", on: "external", line: "后台 1 个任务在跑，跑完会自己接着做" } });
const woken = row("ci", "idle", { work: { phase: "blocked", on: "external", wait: "CI 跑完" } });
const stopped = row("stop", "idle", { work: { phase: "done" } });
const onUser = row("user", "idle", { work: { phase: "blocked", on: "user" } });

describe("task waiting (#3274)", () => {
  const lookup = makeSessionLookup([running, background, woken, stopped, onUser]);

  it("running, waiting and stopped are three states", () => {
    expect(todoStatus(task("run"), lookup)).toBe("working");
    expect(todoStatus(task("bg"), lookup)).toBe("waiting");
    expect(todoStatus(task("ci"), lookup)).toBe("waiting");
    expect(todoStatus(task("stop"), lookup)).toBe("follow_up");
    expect(todoStatus(task("user"), lookup)).toBe("follow_up");
    expect(todoStatus(task("bg", "run"), lookup)).toBe("working");
    expect(todoStatus(task("stop", "ci"), lookup)).toBe("waiting");
  });

  it("an agent waits only when something brings it back", () => {
    expect([running, background, woken, stopped, onUser].map(agentWaits)).toEqual([false, true, true, false, false]);
    expect(agentWaits(row("fin", "unread", { work: { phase: "blocked", on: "external" } }))).toBe(false);
  });

  it("says what it waits for, as Waiting under way", () => {
    expect(waitingLine(task("stop", "ci"), lookup)).toBe("CI 跑完");
    expect(waitingLine(task("bg"), lookup)).toBe("后台 1 个任务在跑，跑完会自己接着做");
    expect(TODO_STATUS_INFO.waiting.label).not.toBe(TODO_STATUS_INFO.follow_up.label);
    expect(boardSectionOf({ todo: task("bg"), status: "waiting" })).toBe("working");
  });
});

describe("a task's agents outside the opened chats (#3274)", () => {
  it("reads the list's row when no chat has the agent", () => {
    const listed = { ...background };
    const lookup = taskSessionLookup({ sessions: {}, sessionSummaries: [listed], taskRows: {} });
    expect(lookup("m", "bg")).toBe(listed);
    expect(todoStatus(task("bg"), lookup)).toBe("waiting");
  });

  it("reads an open task's row outside the Agents window", () => {
    const outside = row("new", "working");
    const id = JSON.stringify(["m", "new"]);
    expect(taskSessionLookup({ sessions: {}, sessionSummaries: [], taskRows: { [id]: outside } })("m", "new")).toBe(outside);
    expect(withTaskRows(() => undefined, { [id]: outside })("m", "new")).toBe(outside);
  });

  it("same-state summaries replace old workflow progress and clear finished background work", () => {
    const flow = { task_id: "wf", status: "running", running: true, phase: "Review", agents_done: 4, agents_started: 6 };
    const detail = row("bg", "working", { rev: 5, background_working: true, workflows: [flow] });
    const listed = { ...detail, rev: 6, waiting: true, background_tasks_count: 0, workflows: undefined,
      running_workflows: 1, workflow_progress: { ...flow, agents_done: 5 } };
    expect(taskSessionLookup({ sessions: { m: [detail] }, sessionSummaries: [listed], taskRows: {} })("m", "bg")).toBe(listed);
    const finished = { ...listed, rev: 7, background_working: false, running_workflows: 0, workflow_progress: undefined };
    expect(taskSessionLookup({ sessions: { m: [detail] }, sessionSummaries: [finished], taskRows: {} })("m", "bg")).toBe(finished);
  });

  it("a closed chat's old complete row does not hide the list's newer state", () => {
    const old = row("bg", "idle", { rev: 5, subagents: [] });
    const now = { ...background, rev: 5 };
    expect(taskSessionLookup({ sessions: { m: [old] }, sessionSummaries: [now], taskRows: {} })("m", "bg")).toBe(now);
    // saying the same about its state, the complete row stands (its subagents and steps)
    const same = { ...old, state: "idle" as const };
    expect(taskSessionLookup({ sessions: { m: [old] }, sessionSummaries: [same], taskRows: {} })("m", "bg")).toBe(old);
    // a newer transcript is the complete row's
    const newer = { ...old, rev: 9 };
    expect(taskSessionLookup({ sessions: { m: [newer] }, sessionSummaries: [now], taskRows: {} })("m", "bg")).toBe(newer);
  });
});
