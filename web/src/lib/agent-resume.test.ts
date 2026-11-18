import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "./api";
import { clearResume, resumeAgent, resumeInTerminal, resumeState, subscribeResume, terminalResume } from "./agent-resume";
import type { Session } from "./types";

afterEach(() => {
  clearResume("m1", "s1");
  vi.unstubAllGlobals();
});

describe("Resume a stopped Agent (#4754)", () => {
  it("posts the session's id and nothing else, and says resuming, then done", async () => {
    let finish: () => void = () => {};
    const post = vi.fn(() => new Promise<unknown>((resolve) => (finish = () => resolve({ session: {} }))));
    const seen: string[] = [];
    const off = subscribeResume(() => seen.push(resumeState("m1", "s1").phase));
    const going = resumeAgent("m1", "s1", post);
    expect(resumeState("m1", "s1").phase).toBe("resuming");
    expect(post).toHaveBeenCalledWith("/sessions", { method: "POST", json: { machine_id: "m1", resume_session_id: "s1" } });
    finish();
    expect(await going).toBe(true);
    off();
    expect(resumeState("m1", "s1").phase).toBe("done");
    expect(seen).toEqual(["resuming", "done"]);
  });

  it("does not send a second Resume while one is out", async () => {
    let finish: () => void = () => {};
    const post = vi.fn(() => new Promise<unknown>((resolve) => (finish = () => resolve({}))));
    const first = resumeAgent("m1", "s1", post);
    expect(await resumeAgent("m1", "s1", post)).toBe(false);
    expect(post).toHaveBeenCalledTimes(1);
    finish();
    await first;
  });

  it("keeps the hub's refusal in words for both places that show it, and lets it be tried again", async () => {
    const post = vi.fn().mockRejectedValueOnce(new ApiError(503, "offline", { code: "node_unreachable", delivered: "none" })).mockResolvedValueOnce({});
    expect(await resumeAgent("m1", "s1", post)).toBe(false);
    const failed = resumeState("m1", "s1");
    expect(failed.phase).toBe("failed");
    expect(failed.phase === "failed" && failed.error).toBeTruthy();
    expect(await resumeAgent("m1", "s1", post)).toBe(true);
    expect(resumeState("m1", "s1").phase).toBe("done");
  });

  it("goes through the hub's start endpoint by default", async () => {
    vi.stubGlobal("localStorage", { getItem: () => "device-token" });
    const fetch = vi.fn().mockResolvedValue(new Response('{"session":{}}', { status: 200 }));
    vi.stubGlobal("fetch", fetch);
    expect(await resumeAgent("m1", "s1")).toBe(true);
    expect(fetch.mock.calls[0][0]).toBe("/api/v1/sessions");
    expect(fetch.mock.calls[0][1].method).toBe("POST");
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ machine_id: "m1", resume_session_id: "s1" });
  });
});

describe("Resume in terminal (#5521): the stopped line's Resume and ⋯'s item are one action", () => {
  it("shows the terminal first, then posts the same resume", async () => {
    const order: string[] = [];
    const post = vi.fn(async () => {
      order.push("post");
      return {};
    });
    expect(await resumeInTerminal("m1", "s1", () => order.push("terminal"), post)).toBe(true);
    expect(order).toEqual(["terminal", "post"]);
    expect(post).toHaveBeenCalledWith("/sessions", { method: "POST", json: { machine_id: "m1", resume_session_id: "s1" } });
  });
});

describe("Resume in terminal: when an Agent's ⋯ offers it (#5521)", () => {
  // a Claude Code typed into a terminal of its own, closed since: the node lists it stopped (read and terminal)
  const stopped = { machine_id: "m1", session_id: "s1", title: "", cwd: "/w", status: "offline", agent: "claude", agent_surface: "cli",
    capabilities: { read: true, terminal: true }, read_only_reason: "Agent 已停止" } as Session;
  const herdr = { engine_hosts: ["herdr"] };
  const none = { engine_hosts: [] as string[] };

  it("offers a stopped Agent, Claude Code or a Codex thread nothing has open", () => {
    expect(terminalResume(stopped, herdr)).toBe("available");
    expect(terminalResume({ ...stopped, status: "interrupted" }, herdr)).toBe("available");
    expect(terminalResume({ ...stopped, agent: "codex", source_kind: "codex_history" }, herdr)).toBe("available");
    expect(terminalResume(stopped, undefined)).toBe("available"); // a hub before engine sets: the node's word holds
  });

  it("is disabled, with why, for an Agent that isn't running on a computer with no terminal engine", () => {
    const listed = { ...stopped, capabilities: undefined, read_only_reason: undefined }; // as the node lists it there
    expect(terminalResume(listed, none)).toBe("no_engine");
    expect(terminalResume({ ...listed, agent: "codex", source_kind: "codex_history", capabilities: { read: true } }, none)).toBe("no_engine");
    expect(terminalResume(stopped, none)).toBe("no_engine"); // engines turned off since the row was listed
  });

  it("is not offered while it runs, here or in a terminal of its own", () => {
    expect(terminalResume({ ...stopped, status: "idle", capabilities: undefined }, herdr)).toBe("hidden");
    expect(terminalResume({ ...stopped, status: "idle", source_kind: "claude_cli", capabilities: { read: true, send: true } }, herdr)).toBe("hidden");
    expect(terminalResume({ ...stopped, status: "idle", source_kind: "claude_cli", capabilities: { read: true } }, none)).toBe("hidden");
    expect(terminalResume({ ...stopped, starting: true }, herdr)).toBe("hidden");
  });

  it("leaves out assistant conversations, the desktop apps' sessions, other sources and older nodes' rows", () => {
    expect(terminalResume({ ...stopped, assistant: true }, herdr)).toBe("hidden");
    expect(terminalResume({ ...stopped, assistant: true }, none)).toBe("hidden");
    expect(terminalResume({ ...stopped, agent_surface: "desktop", capabilities: undefined }, none)).toBe("hidden");
    expect(terminalResume({ ...stopped, source_kind: "runtime", agent: "dsh", capabilities: { read: true } }, none)).toBe("hidden");
    expect(terminalResume({ ...stopped, capabilities: undefined, read_only_reason: undefined }, herdr)).toBe("hidden");
    expect(terminalResume(undefined, herdr)).toBe("hidden");
  });
});
