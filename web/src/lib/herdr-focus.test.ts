import { describe, expect, it } from "vitest";
import { FocusGuard, FOLLOW_MS, SENT_MS, decideFocus, focusAddress, followRoute, parseFocus, type HerdrFocus } from "./herdr-focus";
import type { Session } from "./types";

const ev = (over: Partial<HerdrFocus> = {}): HerdrFocus => ({
  id: "v1",
  machine_id: "m1",
  workspace_id: "w1",
  tab_id: "w1:t1",
  pane_id: "w1:p2",
  source: "client",
  mine: true,
  ...over,
});
const here = { shown: "s-other", front: true, sent: false };

describe("parseFocus", () => {
  it("reads the hub's message", () => {
    expect(parseFocus({ type: "herdr_focus", id: "v1", machine_id: "m1", workspace_id: "w1", tab_id: "w1:t2", pane_id: "w1:p3", session_id: "s1", source: "persona", mine: true })).toEqual({
      id: "v1",
      machine_id: "m1",
      workspace_id: "w1",
      tab_id: "w1:t2",
      pane_id: "w1:p3",
      session_id: "s1",
      source: "persona",
      mine: true,
    });
  });
  it("takes an unknown source for 'other' and a missing mine for false", () => {
    expect(parseFocus({ id: "v1", pane_id: "w1:p1", source: "whatever" })).toMatchObject({ source: "other", mine: false });
  });
  it("needs a view and a pane", () => {
    expect(parseFocus({ pane_id: "w1:p1" })).toBeNull();
    expect(parseFocus({ id: "v1" })).toBeNull();
    expect(parseFocus({ id: 1, pane_id: 2 })).toBeNull();
  });
});

describe("focusAddress", () => {
  it("is the session of an agent pane and the pane's address for a shell", () => {
    expect(focusAddress({ pane_id: "w1:p2", session_id: "s1" })).toBe("s1");
    expect(focusAddress({ pane_id: "w1:p2" })).toBe("pane:w1:p2");
  });
});

describe("decideFocus", () => {
  it("follows a change made in this view", () => {
    expect(decideFocus(ev(), here)).toBe("follow");
    expect(decideFocus(ev({ session_id: "s1" }), here)).toBe("follow");
  });
  it("does nothing when the page is on that pane already", () => {
    expect(decideFocus(ev(), { ...here, shown: "pane:w1:p2" })).toBe("same");
    expect(decideFocus(ev({ session_id: "s1", source: "other", mine: false }), { ...here, shown: "s1" })).toBe("same");
  });
  it("takes the focus the page asked for, coming back, for an echo, however the node saw it", () => {
    expect(decideFocus(ev({ source: "persona" }), here)).toBe("echo");
    expect(decideFocus(ev({ source: "other", mine: false }), { ...here, sent: true })).toBe("echo");
    expect(decideFocus(ev({ source: "client" }), { ...here, sent: true })).toBe("echo");
  });
  it("does not let a change made elsewhere drag the page", () => {
    expect(decideFocus(ev({ source: "other", mine: false }), here)).toBe("elsewhere"); // the computer's own window
    expect(decideFocus(ev({ source: "client", mine: false }), here)).toBe("elsewhere"); // another browser's view
    expect(decideFocus(ev({ source: "persona", mine: false }), here)).toBe("elsewhere"); // another browser's sidebar
  });
  it("does not move a page whose surface is not the live one in front", () => {
    expect(decideFocus(ev(), { ...here, front: false })).toBe("elsewhere");
  });
});

describe("FocusGuard", () => {
  it("remembers a sent focus for a while", () => {
    const g = new FocusGuard();
    g.noteSent("s1", 1000);
    expect(g.wasSent("s1", 1000 + SENT_MS)).toBe(true);
    expect(g.wasSent("s1", 1001 + SENT_MS)).toBe(false);
    expect(g.wasSent("s2", 1000)).toBe(false);
  });
  it("forgets old sends when it notes new ones", () => {
    const g = new FocusGuard();
    g.noteSent("old", 0);
    g.noteSent("new", SENT_MS + 10);
    expect(g.wasSent("old", SENT_MS + 10)).toBe(false);
    expect(g.wasSent("new", SENT_MS + 10)).toBe(true);
  });
  it("a follow is spent by the first arrival at its address, and sends nothing back", () => {
    const g = new FocusGuard();
    g.noteFollow("s1", 5000);
    expect(g.consumeFollow("s2", 5001)).toBe(false); // another address: not ours
    expect(g.consumeFollow("s1", 5001)).toBe(true);
    expect(g.consumeFollow("s1", 5002)).toBe(false); // spent: the user going there later is the user
  });
  it("a follow nobody arrived at in time is stale", () => {
    const g = new FocusGuard();
    g.noteFollow("s1", 5000);
    expect(g.consumeFollow("s1", 5000 + FOLLOW_MS + 1)).toBe(false);
  });
  it("a follow and a send do not feed each other", () => {
    // herdr → page: follow to s1; the page opens on s1 and must not send a focus for it
    const g = new FocusGuard();
    g.noteFollow("s1", 100);
    expect(g.consumeFollow("s1", 110)).toBe(true);
    // page → herdr: the page sends s2; the change comes back as its echo and is not a follow
    g.noteSent("s2", 200);
    expect(decideFocus(ev({ session_id: "s2", source: "persona" }), { shown: "s2", front: true, sent: g.wasSent("s2", 210) })).toBe("same");
    expect(decideFocus(ev({ session_id: "s2", source: "persona" }), { shown: "s3", front: true, sent: g.wasSent("s2", 210) })).toBe("echo");
  });
});

const session = (over: Partial<Session> = {}): Session =>
  ({ machine_id: "m1", session_id: "s1", title: "t", cwd: "/code", status: "idle", state: "idle", space_id: "w1", slot_id: "w1:t1", ...over }) as Session;

describe("followRoute", () => {
  it("opens an agent pane on its page in the terminal view", () => {
    expect(followRoute("m1", { pane_id: "w1:p1", session_id: "s1" }, [session()])).toEqual({ name: "tab", mid: "m1", space: "w1", slot: "w1:t1", sid: "s1", view: "term" });
    expect(followRoute("m1", { pane_id: "w1:p1", session_id: "s1" }, [session({ space_id: undefined, slot_id: undefined })])).toEqual({ name: "session", mid: "m1", sid: "s1", view: "term" });
  });
  it("opens a plain shell on the 终端 page by its address", () => {
    expect(followRoute("m1", { pane_id: "w2:p3" }, [session()])).toEqual({ name: "terminal", mid: "m1", id: "pane:w2:p3" });
  });
  it("falls back to the pane's address for an agent that cannot show a terminal here", () => {
    expect(followRoute("m1", { pane_id: "w1:p1", session_id: "gone" }, [session()])).toEqual({ name: "terminal", mid: "m1", id: "pane:w1:p1" });
    expect(followRoute("m1", { pane_id: "w1:p1", session_id: "s1" }, [session({ state: "offline" })])).toEqual({ name: "terminal", mid: "m1", id: "pane:w1:p1" });
  });
});
