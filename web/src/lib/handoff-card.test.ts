import { describe, expect, it } from "vitest";
import { handoffCardBody, handoffCardState, wrapUpItems, wrapUpWait, type HandoffCardBody } from "./handoff-card";
import type { FeedItem } from "./feed";
import type { InboxItem } from "./inbox";

// A hand-off conversation's write-back card (#5932): the item, not the card, says whether it can still be sent.
const card: HandoffCardBody = { version: 2, kind: "reply", text: "周五上午给你", recap: "示例客户要报价", title: "示例客户", source: "lark" };
const item = (patch: Partial<InboxItem> = {}): InboxItem =>
  ({
    id: "lark:f1:100", source: "lark", kind: "dm", title: "示例客户", preview: "", count: 1, tier: "action",
    suggestion: { version: 2, kind: "reply", text: "周五上午给你", conversation: { machine_id: "server", session_id: "m1" } },
    handoffs: [{ machine_id: "server", session_id: "m1", at: "2026-11-13T09:00:00Z" }],
    ...patch,
  }) as InboxItem;

describe("a hand-off card's state", () => {
  it("is live while the item's suggestion is this version from this conversation", () => {
    expect(handoffCardState(card, "m1", item(), true)).toEqual({ state: "live" });
  });

  it("is replaced by a newer suggestion, one from elsewhere, or none (turned down)", () => {
    expect(handoffCardState(card, "m1", item({ suggestion: { version: 3, kind: "reply", text: "x", conversation: { machine_id: "server", session_id: "m1" } } }), true).state).toBe("replaced");
    expect(handoffCardState(card, "m1", item({ suggestion: { version: 2, kind: "reply", text: "x" } }), true).state).toBe("replaced");
    expect(handoffCardState(card, "m1", item({ suggestion: undefined }), true).state).toBe("replaced");
  });

  it("says what went out once the confirmed reply was sent, and that the user answered at the source", () => {
    const sent = item({ done_at: "2026-11-13T10:00:00Z", done_by: "replied", suggestion: undefined, chain: [{ id: "c1", kind: "confirmed", text: "你确认了", detail: "周五上午给你（改过）" }] });
    expect(handoffCardState(card, "m1", sent, true)).toEqual({ state: "sent", text: "周五上午给你（改过）" });
    const self = item({ done_at: "2026-11-13T10:00:00Z", done_by: "replied", suggestion: undefined, chain: [{ id: "c1", kind: "self_replied", text: "你在飞书回复了" }] });
    expect(handoffCardState(card, "m1", self, true).state).toBe("self");
  });

  it("is filed when the item was handled another way, and only the newest card says how it ended", () => {
    const done = item({ done_at: "2026-11-13T10:00:00Z", done_by: "user", suggestion: undefined });
    expect(handoffCardState(card, "m1", done, true).state).toBe("filed");
    expect(handoffCardState(card, "m1", done, false).state).toBe("replaced");
  });

  it("is unknown without the item", () => {
    expect(handoffCardState(card, "m1", undefined, true).state).toBe("unknown");
  });

  it("reads the body, and none without its version and text", () => {
    const feed = (body: Record<string, unknown>): FeedItem => ({ id: "f1", conversation: "m1", at: "2026-11-13T09:00:00Z", kind: "inbox_suggestion", ref: "lark:f1:100", body });
    expect(handoffCardBody(feed({ ...card }))?.version).toBe(2);
    expect(handoffCardBody(feed({ text: "x" }))).toBeUndefined();
  });
});

describe("Wrap up chips", () => {
  it("are the open items handed to this conversation, newest hand-off first, two at most", () => {
    const handed = (id: string, at: string, sid = "m1", done = false) =>
      item({ id, title: id, handoffs: [{ machine_id: "server", session_id: sid, at }], done_at: done ? at : undefined });
    const items = [
      handed("a", "2026-11-13T08:00:00Z"),
      handed("b", "2026-11-13T09:00:00Z"),
      handed("c", "2026-11-13T07:00:00Z"),
      handed("done", "2026-11-13T10:00:00Z", "m1", true),
      handed("other", "2026-11-13T10:00:00Z", "m2"),
      item({ id: "plain", handoffs: undefined }),
    ];
    expect(wrapUpItems(items, "m1").map((x) => x.id)).toEqual(["b", "a"]);
    expect(wrapUpItems(items, "m3")).toEqual([]);
  });

  it("leave out an item handled with nothing waiting, and keep one whose suggestion waits or that is still worked on", () => {
    const handed = (id: string, patch: Partial<InboxItem>) => item({ id, title: id, ...patch });
    const items = [
      handed("handled", { suggestion: undefined, handling: { state: "done" } }),
      handed("to-send", { handling: { state: "done" } }),
      handed("working", { suggestion: undefined, handling: { state: "agent" } }),
    ];
    expect(wrapUpItems(items, "m1").map((x) => x.id).sort()).toEqual(["to-send", "working"]);
  });

  it("say what waits: a reply to send, anything else to confirm, nothing without a suggestion", () => {
    expect(wrapUpWait(item())).toBe("send");
    expect(wrapUpWait(item({ suggestion: { version: 3, kind: "conclusion", text: "x" } }))).toBe("confirm");
    expect(wrapUpWait(item({ suggestion: { version: 3, kind: "agent", text: "x" } }))).toBe("confirm");
    expect(wrapUpWait(item({ suggestion: undefined }))).toBeUndefined();
  });
});
