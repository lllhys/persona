// What Persona sent to its tasks' agents (#1542): only a routed item marked from "assistant" is Persona's; a task's card
// takes in the hand-off written right after it (same task, same conversation), never a later message or the user's.
import { describe, expect, it } from "vitest";
import { displayLines, drawnFeed, firstLine, HAND_OFF_MS, sentByPersona, sentTo, shownFeed, takenIn, type FeedItem } from "./feed";

// a local noon, so a few minutes either way stay the same day in any time zone
const t0 = new Date(2026, 10, 8, 12, 0, 0).getTime();
const today = new Date(t0 + 3_600_000);
const at = (ms: number) => new Date(t0 + ms).toISOString();
const card = (id: string, ref: string, ms: number, conversation = "c1"): FeedItem => ({ id, conversation, at: at(ms), kind: "task", ref });
const persona = (id: string, ref: string, ms: number, extra: Record<string, unknown> = {}, conversation = "c1"): FeedItem => ({
  id, conversation, at: at(ms), kind: "routed", ref, body: { text: "把内存降下来\n背景：…", from: "assistant", ...extra },
});
const user = (id: string, ref: string, ms: number): FeedItem => ({ id, conversation: "c1", at: at(ms), kind: "routed", ref, body: { text: "顺便看看日志" } });

describe("sent to agents (#1542)", () => {
  it("is Persona's only when the hub marked it", () => {
    expect(sentByPersona(persona("a", "t", 0))).toBe(true);
    expect(sentByPersona(user("b", "t", 0))).toBe(false);
    // an interrupt from a hub before #1542 has no from: drawn as it was
    expect(sentByPersona({ ...user("c", "t", 0), body: { text: "先停", interrupted: true } })).toBe(false);
    expect(sentByPersona({ id: "d", conversation: "c1", at: at(0), kind: "report", ref: "t", body: { from: "assistant" } })).toBe(false);
  });

  it("takes in only the hand-off right after its own card", () => {
    const feed = [
      card("card", "t1", 0),
      persona("handoff", "t1", 8),
      user("said", "t1", 1000), // the user's own word: stays a bubble
      persona("rework", "t1", 600_000), // later: a folded line of its own
      persona("other", "t2", 2000), // another task, no card here
      card("card3", "t3", 2000),
      persona("elsewhere", "t3", 3000, {}, "c2"), // t3's card is in c1, the message in c2
      persona("before", "t4", 4000),
      card("card4", "t4", 5000), // written before its card: not a hand-off
    ];
    expect([...takenIn(feed, today)]).toEqual(["handoff"]);
    expect(shownFeed(feed, today).map((i) => i.id)).toEqual(["card", "said", "rework", "other", "card3", "elsewhere", "before", "card4"]);
    expect(sentTo(feed, "t1").map((i) => i.id)).toEqual(["handoff", "rework"]);
    expect(takenIn([card("c", "t", 0), persona("late", "t", HAND_OFF_MS + 1)], today).size).toBe(0);
    expect([...takenIn([card("c", "t", 0), persona("edge", "t", HAND_OFF_MS)], today)]).toEqual(["edge"]);
  });

  it("gives the hand-off back when its card is from another day (#1668)", () => {
    const feed = [card("card", "t1", 0), persona("handoff", "t1", 8)];
    expect([...takenIn(feed, new Date(t0 + 11 * 3_600_000))]).toEqual(["handoff"]); // 23:00 the same day
    expect(takenIn(feed, new Date(t0 + 13 * 3_600_000)).size).toBe(0); // 01:00 the next day
    expect(shownFeed(feed, new Date(t0 + 13 * 3_600_000)).map((i) => i.id)).toEqual(["card", "handoff"]);
  });

  it("leaves the hand-off out of the chat's lines", () => {
    // displayLines reads the clock: a card from today, 00:01, with what came after it
    const base = new Date();
    base.setHours(0, 1, 0, 0);
    const shift = base.getTime() - t0;
    const at = (it: FeedItem): FeedItem => ({ ...it, at: new Date(Date.parse(it.at) + shift).toISOString() });
    const ids = displayLines([], [card("card", "t1", 0), persona("handoff", "t1", 8), persona("rework", "t1", 600_000)].map(at))
      .filter((l) => l.kind === "feed")
      .map((l) => l.id);
    expect(ids).toEqual(["feed-card", "feed-rework"]);
  });

  it("folds to the first line with words", () => {
    expect(firstLine("\n\n  把内存降下来，目标是不再被杀掉。  \n\n背景：\n- 一周 14 次")).toBe("把内存降下来，目标是不再被杀掉。");
    expect(firstLine("（督办：验收没通过，第 1 轮返工，最多 2 轮）\r\n逐条验收：")).toBe("（督办：验收没通过，第 1 轮返工，最多 2 轮）");
    expect(firstLine(" \n \n")).toBe("");
  });
});

// A task's card comes once, when it is handed out (#8237): its receipts, its Agent's reports and its reviews stay in the
// feed for the trays and are not among the chat's lines; everything else is, in order.
describe("what a Persona conversation draws of its feed", () => {
  it("leaves out how a task goes on", () => {
    const kinds = ["plan", "task", "receipt", "answered", "report", "routed", "review", "message", "today"];
    const items = kinds.map((kind, i): FeedItem => ({ id: `f${i}`, conversation: "c1", at: at(i * 1000), kind, ref: "t1" } as FeedItem));
    expect(drawnFeed(items).map((i) => i.kind)).toEqual(["plan", "task", "answered", "routed", "message", "today"]);
    expect(displayLines([], items).map((l) => (l.kind === "feed" ? l.item.kind : l.kind))).not.toContain("receipt");
  });
});
