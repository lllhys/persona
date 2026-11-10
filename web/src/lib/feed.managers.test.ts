// @vitest-environment jsdom
// What a conversation's feed offers is its manager's (docs/assistant/managers.md "Clients"): a plan's Start / Don't start
// and a briefing question's Handled name the conversation's manager, since their routes name no conversation and the
// hub would otherwise take them to the default; the default's conversation names none, whichever assistant is current.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { conversationHeaders, dismissPlan, handledLater, startPlan, type TaskPlan } from "./feed";
import { chooseManager, noteConversation, resetManagerChoice, setRosterDefault } from "./manager-choice";
import { getState, reset } from "./store";
import type { Session } from "./types";

const calls: { url: string; method: string; header: string | null }[] = [];
const plan = (conversation: string, id = `p-${conversation}`): TaskPlan => ({ id, conversation, tasks: [], state: "open" });

beforeEach(() => {
  calls.length = 0;
  reset();
  resetManagerChoice();
  setRosterDefault("assistant");
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, method: init?.method ?? "GET", header: new Headers(init?.headers).get("X-Persona-Manager") });
    const body = url.includes("/task-plans/") ? { plan: { ...plan("c"), id: url.split("/")[4], state: "started" } } : url.includes("/todos") ? { todos: [] } : {};
    return { ok: true, status: 200, text: async () => JSON.stringify(body), headers: new Headers() } as Response;
  }));
});
afterEach(() => {
  reset();
  resetManagerChoice();
  vi.unstubAllGlobals();
});

const named = (path: string) => calls.find((c) => c.url === `/api/v1${path}` && c.method === "POST")?.header;

describe("a conversation's plans and questions", () => {
  it("name the manager a page noted for the conversation (the current assistant's own, or a hand-off's)", async () => {
    chooseManager("m-work");
    noteConversation("work-main", "m-work");
    await startPlan(plan("work-main"), false);
    await dismissPlan(plan("work-main", "p-2"));
    await handledLater("q-1", "work-main");
    expect(named("/task-plans/p-work-main/start")).toBe("m-work");
    expect(named("/task-plans/p-2/dismiss")).toBe("m-work");
    expect(named("/later/q-1/handled")).toBe("m-work");
  });

  it("name the manager the hub's sessions list has the conversation under (opened from a link)", async () => {
    Object.assign(getState(), { sessions: { mac: [{ machine_id: "mac", session_id: "listed", manager: "m-home", assistant: true } as Session] } });
    await startPlan(plan("listed"), false);
    expect(named("/task-plans/p-listed/start")).toBe("m-home");
  });

  it("name none for the default's conversation, even with another assistant current", async () => {
    chooseManager("m-work");
    Object.assign(getState(), { sessions: { hub: [{ machine_id: "hub", session_id: "default-main", manager: "assistant", assistant: true } as Session] } });
    await startPlan(plan("default-main"), false);
    expect(named("/task-plans/p-default-main/start")).toBeNull();
    expect(conversationHeaders("unknown")).toBeUndefined();
  });
});
