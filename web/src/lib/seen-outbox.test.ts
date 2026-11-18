// The seen outbox (lib/seen-outbox.ts): a session this page marked seen stays seen until the hub's own rows say it was
// recorded, a new turn ended, or the agent works or needs you again.
import { describe, expect, it } from "vitest";
import { dueMarks, heldRows, heldSeen, liveMarks, markOver, newMark, nextDue, SEEN_CONFIRM_MS, SEEN_KEEP_MS, unansweredMarks, type SeenMark } from "./seen-outbox";
import { runtimeIdentity } from "./runtime-list";
import type { Session } from "./types";

const T0 = Date.parse("2026-01-01T00:00:00Z");
const row = (patch: Partial<Session> = {}): Session => ({
  machine_id: "m", session_id: "s", title: "s", cwd: "/", status: "done", state: "unread",
  updated_at: "2026-01-01T00:00:00Z", seen_at: "0001-01-01T00:00:00Z", ...patch,
});

describe("seen outbox", () => {
  it("only waits for unread agents", () => {
    expect(newMark(row(), T0)).toBeDefined();
    expect(newMark(row({ state: "idle" }), T0)).toBeUndefined();
    expect(newMark(row({ state: "working", status: "working" }), T0)).toBeUndefined();
  });

  it("waits for Just finished agents too, and shows them seen meanwhile (#9751)", () => {
    const finished = row({ state: "idle", work: { phase: "done", line: "Merged", just_finished: true } });
    const mark = newMark(finished, T0)!;
    expect(mark).toBeDefined();
    const shown = heldSeen(finished, mark, T0 + 1000);
    expect(shown.state).toBe("idle");
    expect(shown.work).toEqual({ phase: "done", line: "Merged", just_finished: false });
    expect(markOver({ ...finished, seen_at: "2026-01-01T00:00:03Z", work: { phase: "done", line: "Merged" } }, mark, T0 + 3000)).toBe(true);
    expect(newMark(row({ state: "idle", work: { phase: "done", line: "Merged" } }), T0)).toBeUndefined();
  });

  it("holds an unread row seen until the hub records it", () => {
    const mark = newMark(row(), T0)!;
    // the hub's next list still says unread (the seen was lost or not taken yet): shown seen
    expect(heldSeen(row(), mark, T0 + 1000).state).toBe("idle");
    expect(markOver(row(), mark, T0 + 1000)).toBe(false);
    // the hub recorded it: the mark is over
    expect(markOver(row({ state: "idle", seen_at: "2026-01-01T00:00:03Z" }), mark, T0 + 3000)).toBe(true);
  });

  it("does not take the page's own seen_at for the hub's record", () => {
    const local = "2026-01-01T00:00:01.000Z";
    const mark = newMark(row(), T0, local)!;
    expect(markOver(row({ state: "idle", seen_at: local }), mark, T0 + 1000)).toBe(false);
  });

  it("lets a new turn, work or a question show again", () => {
    const mark = newMark(row(), T0)!;
    // the transcript touched without a new turn (inside the hub's 2 s): still the same turn
    expect(markOver(row({ updated_at: "2026-01-01T00:00:01Z" }), mark, T0)).toBe(false);
    // another turn ended after the look: unread again
    const next = row({ updated_at: "2026-01-01T00:01:00Z" });
    expect(markOver(next, mark, T0)).toBe(true);
    expect(heldSeen(next, mark, T0).state).toBe("unread");
    expect(markOver(row({ state: "working", status: "working" }), mark, T0)).toBe(true);
    expect(markOver(row({ state: "needs_you", status: "blocked" }), mark, T0)).toBe(true);
  });

  it("gives up after a while", () => {
    const mark = newMark(row(), T0)!;
    expect(markOver(row(), mark, T0 + SEEN_KEEP_MS + 1)).toBe(true);
    expect(liveMarks({ a: mark }, T0 + SEEN_KEEP_MS + 1)).toEqual({});
  });

  it("keeps the same array when nothing is held", () => {
    const rows = [row({ session_id: "a" }), row({ session_id: "b" })];
    expect(heldRows(rows, {}, T0)).toBe(rows);
    const held = heldRows(rows, { [runtimeIdentity(rows[1])]: newMark(rows[1], T0)! }, T0);
    expect(held[0]).toBe(rows[0]);
    expect(held[1].state).toBe("idle");
  });

  it("retries a socket mark over HTTP after SEEN_CONFIRM_MS, and resends what was not answered", () => {
    const socket: SeenMark = { ...newMark(row(), T0)!, route: "socket" };
    const http: SeenMark = { ...socket, route: "http" };
    const answered: SeenMark = { ...http, answered: true };
    const marks = { a: socket, b: http, c: answered };
    expect(dueMarks(marks, T0 + SEEN_CONFIRM_MS - 1)).toEqual([]);
    expect(dueMarks(marks, T0 + SEEN_CONFIRM_MS)).toEqual(["a"]);
    expect(nextDue(marks, T0)).toBe(T0 + SEEN_CONFIRM_MS);
    expect(unansweredMarks(marks, T0)).toEqual(["a", "b"]);
  });
});
