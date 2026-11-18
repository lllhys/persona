// Side sessions folded into their parent's chat (#7944): the fold rule, the parent's folded state with its sides-only
// unread step, the rollup from the side rows a list holds, the panel's order and where a side's card and link go. Without
// `sessions_side_fold` in hello nothing folds a side.
import { afterEach, describe, expect, it } from "vitest";
import { requestRoute } from "./card-focus";
import { foldedChildren, foldedState, isFoldedIn, listedKeys, requestHome } from "./child-sessions";
import { childCards, foldedRow, foldRows, homeKey } from "./folded-rows";
import { countSides, foldedStateWith, foldHome, foldsUnder, hubSides, isSide, searchHitSide, setSideFold, sideOrder, sidesAmong } from "./side-fold";
import type { Interaction, Session } from "./types";

const row = (session_id: string, extra: Partial<Session> = {}): Session => ({ machine_id: "mac", session_id, title: session_id, cwd: "/code", status: "idle", state: "idle", ...extra }) as Session;
// the review and three side sessions of it: one waits on the user, one is unread, one is a stopped side of a side; and a
// Codex subagent of the first side (it folds through that side: folded_side)
const parent = row("review", { children: { total: 4, needs_you: 1, sides: 3, sides_needs_you: 1, sides_unread: 1 } });
const btw = row("btw", { folded_under: "review", folded_as: "side", state: "needs_you", status: "blocked", updated_at: "2026-11-15T10:04:00Z" });
const chat = row("chat", { folded_under: "review", folded_as: "side", state: "unread", updated_at: "2026-11-15T10:09:00Z" });
const stopped = row("old", { folded_under: "review", folded_as: "side", state: "offline", status: "offline", updated_at: "2026-11-15T10:30:00Z" });
const kid = row("kid", { folded_under: "review", folded_side: "btw", is_subagent: true, parent_session_id: "btw" });
const rows = [parent, btw, chat, stopped, kid];

afterEach(() => setSideFold(false));

describe("without sessions_side_fold", () => {
  it("folds no side: each stays a row of its own, and its card is its own", () => {
    expect(isSide(btw)).toBe(false);
    expect(foldsUnder(btw)).toBeUndefined();
    // a row the hub folded through a side folds under that side, as before side folding
    expect(foldsUnder(kid)).toBe("btw");
    expect(foldRows(rows).map((s) => s.session_id)).toEqual(["review", "btw", "chat", "old"]);
    const card = { id: "q", machine_id: "mac", session_id: "btw", kind: "question", folded_under: "review", folded_as: "side" } as Interaction;
    expect(requestHome(card)).toBe("btw");
    expect(requestRoute(card)).toEqual({ name: "session", mid: "mac", sid: "btw" });
    // and the parent's state ignores the side rollup
    expect(foldedState(row("p", { state: "idle", children: { total: 1, sides: 1, sides_unread: 1 } }))).toBe("idle");
  });
});

describe("with sessions_side_fold", () => {
  it("leaves every side out of the list and folds its parent, a side of a side under the same topmost row", () => {
    setSideFold(true);
    expect([btw, chat, stopped].every(isSide)).toBe(true);
    const listed = listedKeys(rows);
    expect(rows.filter((s) => isFoldedIn(s, listed)).map((s) => s.session_id)).toEqual(["btw", "chat", "old", "kid"]);
    const shown = foldRows(rows);
    expect(shown.map((s) => s.session_id)).toEqual(["review"]);
    expect(shown[0]!.state).toBe("needs_you");
    // a side whose parent is not listed stays a row
    expect(foldRows([btw, chat]).map((s) => s.session_id)).toEqual(["btw", "chat"]);
  });

  it("draws a side's own children in the side's chat, and the sides in the parent's", () => {
    setSideFold(true);
    expect(foldHome(kid)).toBe("btw");
    expect(foldHome(btw)).toBe("review");
    expect(foldedChildren(rows, "mac", "review").map((s) => s.session_id).sort()).toEqual(["btw", "chat", "old"]);
    expect(foldedChildren(rows, "mac", "btw").map((s) => s.session_id)).toEqual(["kid"]);
  });

  it("counts a side's card under its parent and links it to the side, a card of a side's own to that side", () => {
    setSideFold(true);
    const side = { id: "q", machine_id: "mac", session_id: "btw", kind: "question", folded_under: "review", folded_as: "side" } as Interaction;
    const own = { id: "p", machine_id: "mac", session_id: "kid", kind: "permission", folded_under: "review", folded_side: "btw" } as Interaction;
    expect(homeKey(side)).toBe("mac/review");
    expect(homeKey(own)).toBe("mac/review");
    expect(childCards([side, own], "mac", "review")).toHaveLength(2);
    expect(requestRoute(side)).toEqual({ name: "session", mid: "mac", sid: "btw" });
    expect(requestRoute(own)).toEqual({ name: "session", mid: "mac", sid: "btw" });
  });

  it("makes the parent unread for an unread side, but not for an unread Codex child or worker", () => {
    setSideFold(true);
    expect(foldedState(row("p", { children: { total: 1, sides: 1, sides_unread: 1 } }))).toBe("unread");
    expect(foldedState(row("p", { children: { total: 2, workers: 1 } }))).toBe("idle");
    // the existing steps stay first: a working side makes it work, one waiting makes it need you
    expect(foldedState(row("p", { children: { total: 2, working: 1, sides: 2, sides_working: 1, sides_unread: 1 } }))).toBe("working");
    expect(foldedState(row("p", { state: "unread", children: { total: 1, working: 1, sides: 1, sides_working: 1 } }))).toBe("unread");
    expect(foldedState(row("p", { children: { total: 1, needs_you: 1, sides: 1, sides_needs_you: 1 } }))).toBe("needs_you");
  });

  it("takes the side part from the side rows a list holds, so a side seen here shows at once", () => {
    setSideFold(true);
    const p = row("review", { children: { total: 2, sides: 2, sides_unread: 1 } });
    const unread = row("a", { folded_under: "review", folded_as: "side", state: "unread" });
    const other = row("b", { folded_under: "review", folded_as: "side" });
    expect(foldRows([p, unread, other])[0]!.state).toBe("unread");
    // seen here: the store's copy says idle before the hub's rollup does
    const seen = { ...unread, state: "idle" as const };
    const folded = foldRows([p, seen, other]);
    expect(folded[0]!.state).toBe("idle");
    // and a row the list folded keeps that state when a row draws it again from the hub's rollup alone
    expect(foldedRow(folded[0]!)).toBe(folded[0]);
    // the hub's side counts give way to the held rows' on needs_you and working, the rest of children staying
    const busy = row("x", { children: { total: 2, needs_you: 1, working: 1, sides: 1, sides_needs_you: 1 } });
    expect(foldedStateWith(busy, countSides([row("s", { state: "idle" })]))).toBe("working");
    expect(hubSides(busy)).toEqual({ sides: 1, working: 0, needs_you: 1, unread: 0 });
  });

  it("orders the panel needs you first, then working, then by last activity, stopped ones last", () => {
    setSideFold(true);
    const working = row("w", { folded_under: "review", folded_as: "side", state: "working", updated_at: "2026-11-15T09:00:00Z" });
    expect(sidesAmong([stopped, chat, working, btw, kid]).map((s) => s.session_id)).toEqual(["btw", "w", "chat", "old"]);
    expect(sideOrder(chat, chat)).toBe(0);
  });

  it("reads the side a search hit is in (search_hits[<id>].hit_session_id) through one helper", () => {
    expect(searchHitSide({ session: parent, side: "btw" })).toBeUndefined();
    setSideFold(true);
    expect(searchHitSide({ session: parent, side: "btw" })).toBe("btw");
    expect(searchHitSide({ session: parent, side: "review" })).toBeUndefined();
    expect(searchHitSide({ session: parent })).toBeUndefined();
  });

  it("keeps a row that works itself working beside an unread side", () => {
    setSideFold(true);
    expect(foldedState(row("p", { state: "working", status: "working", children: { total: 1, sides: 1, sides_unread: 1 } }))).toBe("working");
  });
});
