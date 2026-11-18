import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyCompleteSessions, connect, getState, reset } from "./store";
import type { Session } from "./types";

// #9049: on a socket in list mode the hub sends hints only, so the assistant's conversations (the Persona sidebar, All
// chats) must follow the rows the list reads after a hint, or a conversation read mid-turn keeps spinning.

class Socket {
  static OPEN = 1;
  static last: Socket;
  readyState = 1;
  onopen?: () => void;
  onmessage?: (event: { data: string }) => void;
  constructor(public url: string) { Socket.last = this; }
  send() {}
  close() { this.readyState = 3; }
  push(value: unknown) { this.onmessage?.({ data: JSON.stringify(value) }); }
}

const row = (id: string, extra: Partial<Session> = {}): Session => ({ machine_id: "hub", session_id: id, title: id, cwd: "/home/me/.persona/assistant", status: "idle", state: "idle", updated_at: "2026-11-16T05:00:00Z", ...extra });
const working = (id: string, extra: Partial<Session> = {}) => row(id, { status: "working", state: "working", assistant: true, ...extra });
let changed: Session[];
let conversations: Session[];

beforeEach(() => {
  vi.useFakeTimers();
  changed = [];
  conversations = [working("main"), working("leader", { turn_error: "API Error: 503", turn_error_kind: "server" })];
  vi.stubGlobal("WebSocket", Socket);
  vi.stubGlobal("location", { protocol: "http:", host: "demo" });
  vi.stubGlobal("localStorage", { getItem: () => "demo-token" });
  vi.stubGlobal("window", { setTimeout, clearTimeout });
  vi.stubGlobal("fetch", vi.fn(async (path: string) => {
    const url = new URL(path, "http://demo");
    if (url.pathname === "/api/v1/assistant") return Response.json({ machine_id: "hub", main: "main", conversations });
    if (url.pathname === "/api/v1/sessions") {
      if (url.searchParams.has("counts")) return Response.json({ total: 3, machine: { hub: 3 }, project: {}, group: {}, state: {}, revision: "r1" });
      if (url.searchParams.has("since")) return Response.json({ sessions: changed, remove: [], total: 3, has_more: false, revision: "r2" });
      return Response.json({ sessions: [], remove: [], total: 3, has_more: false, revision: "r1" });
    }
    return Response.json({});
  }));
});
afterEach(() => { reset(); vi.useRealTimers(); vi.unstubAllGlobals(); });
const settle = () => vi.advanceTimersByTimeAsync(0);
const conversation = (id: string) => getState().assistant?.conversations.find((c) => c.session_id === id);
const kept = (id: string) => getState().sessions.hub?.find((s) => s.session_id === id);

async function listMode() {
  connect();
  Socket.last.push({ type: "hello", features: ["sessions_list", "sessions_lookup"] });
  await settle();
  await settle();
  expect(conversation("leader")?.state).toBe("working");
}

describe("the assistant's conversations follow the list (#9049)", () => {
  it("a conversation read mid-turn shows finished once a hint's read says its turn ended", async () => {
    await listMode();
    changed = [row("leader", { assistant: true, waiting: false, last_text: "Done." })];
    Socket.last.push({ type: "sessions_list", revision: "r2" });
    await settle();
    await settle();
    expect(conversation("leader")).toMatchObject({ state: "idle", status: "idle", last_text: "Done." });
    // a summary row says "none" by leaving a marker out: the failed turn before is no longer this one's
    expect(conversation("leader")?.turn_error).toBeUndefined();
    expect(conversation("leader")?.turn_error_kind).toBeUndefined();
    expect(conversation("main")?.state).toBe("working"); // not in the change: as it was
  });

  it("the detail kept of a conversation takes the change and keeps what a summary never carries", async () => {
    await listMode();
    applyCompleteSessions([working("leader", { queued: ["next"], background: [{ id: "bg", kind: "shell", description: "watch" }] }), row("agent", { state: "working", status: "working" })]);
    changed = [row("leader", { assistant: true, waiting: false }), row("agent", { waiting: false })];
    Socket.last.push({ type: "sessions_list", revision: "r2" });
    await settle();
    await settle();
    expect(kept("leader")).toMatchObject({ state: "idle", queued: ["next"], background: [{ id: "bg" }] });
    // an Agent's kept detail is not the assistant's: its own chat reads it (summaries never replace a chat's detail)
    expect(kept("agent")?.state).toBe("working");
  });

  it("rows of another machine or not among the conversations change nothing", async () => {
    await listMode();
    const before = getState().assistant;
    changed = [row("leader", { machine_id: "laptop", waiting: false }), row("stranger", { waiting: false })];
    Socket.last.push({ type: "sessions_list", revision: "r2" });
    await settle();
    await settle();
    expect(getState().assistant).toBe(before);
  });
});
