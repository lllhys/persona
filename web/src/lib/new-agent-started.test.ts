import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("./store", () => {
  const state = { launches: {} as Record<string, Partial<import("./new-agent").AgentLaunch>> };
  const listeners = new Set<() => void>();
  return {
    getState: () => state,
    subscribe: (l: () => void) => { listeners.add(l); return () => listeners.delete(l); },
    emit: () => listeners.forEach((l) => l()),
    listeners,
  };
});

import { whenStarted } from "./new-agent-started";
import * as store from "./store";

const s = store as unknown as { getState: () => { launches: Record<string, unknown> }; emit: () => void; listeners: Set<() => void> };
const session = { machine_id: "laptop", session_id: "s-9" };

beforeEach(() => {
  s.getState().launches = {};
  s.listeners.clear();
});

/** "New agent…" from the Inbox (#5933) hears the Agent once its launch is ready. */
describe("whenStarted", () => {
  it("tells once, when the launch is ready, not while it starts or after a failed try", () => {
    const onStarted = vi.fn();
    s.getState().launches.l1 = { id: "l1", phase: "creating" };
    whenStarted("l1", onStarted);
    s.getState().launches.l1 = { id: "l1", phase: "failed", session };
    s.emit();
    expect(onStarted).not.toHaveBeenCalled();
    s.getState().launches.l1 = { id: "l1", phase: "ready", session };
    s.emit();
    s.emit();
    expect(onStarted).toHaveBeenCalledTimes(1);
    expect(onStarted).toHaveBeenCalledWith(session);
    expect(s.listeners.size).toBe(0);
  });

  it("tells nothing for a launch dropped before it was ready", () => {
    const onStarted = vi.fn();
    s.getState().launches.l2 = { id: "l2", phase: "sending", session };
    whenStarted("l2", onStarted);
    delete s.getState().launches.l2;
    s.emit();
    expect(onStarted).not.toHaveBeenCalled();
    expect(s.listeners.size).toBe(0);
  });
});
