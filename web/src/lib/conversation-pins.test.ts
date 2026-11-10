import { beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const kept = new Map<string, string>();
  (globalThis as Record<string, unknown>).localStorage = { getItem: (k: string) => kept.get(k) ?? null, setItem: (k: string, v: string) => void kept.set(k, v), removeItem: (k: string) => void kept.delete(k), clear: () => kept.clear() };
});

import { getPinnedConversations, parsePinned, pinOrder, sidebarConversations, togglePinnedConversation, unpinAllConversations } from "./conversation-pins";
import type { Session } from "./types";

const conv = (id: string, updated: string) => ({ machine_id: "hub", session_id: id, updated_at: updated }) as Session;

beforeEach(() => unpinAllConversations());

describe("pinned Persona conversations (iOS assistant.pinned)", () => {
  it("are kept in this browser, the newest pin first, and toggle off", () => {
    togglePinnedConversation("a");
    togglePinnedConversation("b");
    expect(getPinnedConversations()).toEqual(["b", "a"]);
    togglePinnedConversation("a");
    expect(getPinnedConversations()).toEqual(["b"]);
    expect(localStorage.getItem("assistant.pinned")).toBe('["b"]');
  });

  it("unpins all, leaving nothing stored", () => {
    togglePinnedConversation("a");
    unpinAllConversations();
    expect(getPinnedConversations()).toEqual([]);
    expect(localStorage.getItem("assistant.pinned")).toBeNull();
  });

  it("reads a broken value as no pins", () => {
    expect(parsePinned("{")).toEqual([]);
    expect(parsePinned('"x"')).toEqual([]);
    expect(parsePinned('["a", 3, ""]')).toEqual(["a"]);
  });
});

describe("the sidebar's Persona chats", () => {
  const list = [conv("main", "2026-11-05T09:00:00Z"), conv("a", "2026-11-01T00:00:00Z"), conv("b", "2026-11-04T00:00:00Z"), conv("c", "2026-11-03T00:00:00Z"), conv("d", "2026-11-02T00:00:00Z")];

  it("puts the pinned ones first in pin order, outside the limit, and never the main one", () => {
    const { pinned, recent } = sidebarConversations(list, "main", ["a", "main", "gone", "c"], 2);
    expect(pinned.map((c) => c.session_id)).toEqual(["a", "c"]);
    expect(recent.map((c) => c.session_id)).toEqual(["b", "d"]);
  });

  it("puts the hub's leaders first in their order, then this browser's pins, then the rest (#3623)", () => {
    const order = pinOrder(["d", "b"], ["c", "b"]);
    expect(order).toEqual(["d", "b", "c"]);
    const { pinned, recent } = sidebarConversations(list, "main", order, 5);
    expect(pinned.map((c) => c.session_id)).toEqual(["d", "b", "c"]);
    expect(recent.map((c) => c.session_id)).toEqual(["a"]);
    expect(pinOrder(undefined, ["c"])).toEqual(["c"]);
  });

  it("keeps the open conversation after the newest ones", () => {
    const { recent } = sidebarConversations(list, "main", [], 2, "a");
    expect(recent.map((c) => c.session_id)).toEqual(["b", "c", "a"]);
  });

  it("leaves the list as it is when the open one is already shown, is the main one or is gone, and lists all without a main one (#2283)", () => {
    const ids = (main: string | undefined, current?: string) => sidebarConversations(list, main, [], 2, current).recent.map((c) => c.session_id);
    expect(ids("main", "b")).toEqual(["b", "c"]);
    expect(ids("main", "main")).toEqual(["b", "c"]);
    expect(ids("main", "gone")).toEqual(["b", "c"]);
    expect(ids(undefined)).toEqual(["main", "b"]);
  });

  it("leaves the hub's automatic conversations out of 最近 unless pinned (#3264)", () => {
    const withOrigins = [
      { ...conv("inbox", "2026-11-06T09:00:00Z"), origin: "inbox" },
      { ...conv("task", "2026-11-06T08:00:00Z"), origin: "task" },
      { ...conv("wechat", "2026-11-06T07:00:00Z"), origin: "channel" },
      { ...conv("mine", "2026-11-06T06:00:00Z"), origin: "user" },
      conv("old", "2026-11-06T05:00:00Z"),
    ] as Session[];
    const { pinned, recent } = sidebarConversations(withOrigins, undefined, ["task"], 5);
    expect(pinned.map((c) => c.session_id)).toEqual(["task"]);
    expect(recent.map((c) => c.session_id)).toEqual(["wechat", "mine", "old"]);
  });
});
