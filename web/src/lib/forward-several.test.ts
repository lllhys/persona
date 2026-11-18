import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fixture from "../../../spec/fixtures/forward/several.json";
import { initI18n, t } from "../i18n";
import type { ForwardPick } from "./forward";
import { forwardGroupHeading, forwardLine, forwardRuns } from "./forward-group";
import { FORWARD_LIMIT, FORWARD_TOTAL, forwardBody, forwardLimit, forwardRef } from "./forward-block";
import { contextXML, withContexts } from "./links";
import { inChatOrder } from "../views/chat/Picking";
import { FORWARD_MOST, messagePick } from "./message-pick";
import { parseUserText } from "./turns";

// router.ts listens to popstate at load (no window in node); links.ts only needs navigate from it.
vi.mock("./router", () => ({ navigate: vi.fn() }));

// Forwarding several messages at once (#3838, docs/protocol/protocol.md "Forwarded messages"): one block per message in
// the chat's order and the note after them, each cut at its share, as spec/fixtures/forward/several.json says (iOS
// ForwardTests reads the same file); forwards of one chat next to each other read as one card; picking them.

interface Block { title: string; url: string; msg: string; attributes: Record<string, string>; text: string }
interface Several {
  version: number;
  budget: { each: number; total: number; most: number; cases: { count: number; limit: number }[] };
  write: { name: string; blocks: Block[]; note: string; message: string }[];
  read: { name: string; message: string; msgs: string[]; rest: string; groups: number[][] }[];
}
const several = fixture as Several;

describe("spec/fixtures/forward/several.json", () => {
  it("is version 1, with the budget this client uses", () => {
    expect(several.version).toBe(1);
    expect({ each: several.budget.each, total: several.budget.total, most: several.budget.most }).toEqual({ each: FORWARD_LIMIT, total: FORWARD_TOTAL, most: FORWARD_MOST });
  });

  it.each(several.budget.cases.map((c) => [c.count, c.limit] as const))("cuts each of %i messages at %i characters", (count, limit) => {
    expect(forwardLimit(count)).toBe(limit);
  });

  it.each(several.write.map((c) => [c.name, c] as const))("writes %s byte for byte: a block per message, then the note", (_, c) => {
    const refs = c.blocks.map((b) => ({ kind: "forward" as const, title: b.title, url: b.url, text: b.text, attrs: { msg: b.msg, ...b.attributes } }));
    expect(withContexts(refs, c.note)).toBe(c.message);
    expect(c.message.startsWith(refs.map(contextXML).join("\n"))).toBe(true);
  });

  it.each(several.read.map((c) => [c.name, c] as const))("reads %s: its blocks in order, the note after them, and the cards they make", (_, c) => {
    const p = parseUserText(c.message);
    expect(p.contexts.map((x) => x.attrs?.msg)).toEqual(c.msgs);
    expect(p.text).toBe(c.rest);
    expect(forwardRuns(p.contexts)).toEqual(c.groups);
  });
});

describe("several forwarded at once", () => {
  beforeEach(() => initI18n("en"));
  afterEach(() => initI18n("zh-Hans"));

  const source = (item: string, role: "user" | "agent" = "agent") => ({
    mid: "macbook",
    sid: "a1f3c2d4-0000-4000-8000-000000000006",
    item,
    role,
    author: "Claude",
    chat: "Network tests",
    machine: "MacBook Pro",
  });

  it("cuts each at its share and says so", () => {
    const limit = forwardLimit(7);
    const cut = forwardBody("y".repeat(limit + 10), undefined, limit);
    expect(cut.startsWith("y".repeat(limit))).toBe(true);
    expect(cut.endsWith(t("chat-forwardto.cut"))).toBe(true);
    expect(forwardRef("y".repeat(limit + 10), source("e5f0#1"), limit).text).toBe(cut);
    expect(forwardBody("short", undefined, limit)).toBe("short");
  });

  it("with no note says to look at them", () => {
    const refs = [forwardRef("a", source("e5f0#1")), forwardRef("b", source("e5f0#2"))];
    expect(withContexts(refs, " ")).toMatch(/<\/persona-context>\n<persona-context [^]*\n\nPlease look at the forwarded messages above\.$/);
    expect(withContexts(refs.slice(0, 1), "")).toMatch(/\n\nPlease look at the forwarded message above\.$/);
  });

  it("heads one card with how many and the chat, and gives each message a line", () => {
    const refs = [forwardRef("How many times\ndoes it retry?", source("e5f0#1", "user")), forwardRef("Three times.", source("e5f0#2"))];
    expect(forwardRuns(refs)).toEqual([[0, 1]]);
    expect(forwardGroupHeading(refs)).toBe("2 forwarded messages · Network tests");
    expect(refs.map(forwardLine)).toEqual(["Me: How many times does it retry?", "Claude: Three times."]);
  });

  it.each([
    ["zh-Hans", "转发的 2 条消息 · Network tests", "Claude：Three times."],
    ["ja", "転送された2件のメッセージ · Network tests", "Claude：Three times."],
  ])("says it in %s", async (lang, heading, line) => {
    await initI18n(lang);
    const refs = [forwardRef("x", source("e5f0#1")), forwardRef("Three times.", source("e5f0#2"))];
    expect(forwardGroupHeading(refs)).toBe(heading);
    expect(forwardLine(refs[1])).toBe(line);
  });
});

describe("picking messages (lib/message-pick.ts)", () => {
  const p = (itemId: string): ForwardPick => ({ itemId, text: itemId, isUser: false });

  it("starts with the message Forward to was opened for, picks and unpicks, and ends with none", () => {
    const pick = messagePick();
    const seen = vi.fn();
    pick.subscribe(seen);
    expect(pick.on()).toBe(false);
    expect(pick.toggle(p("a"))).toBe(false); // not picking: nothing to pick into
    pick.start(p("a"));
    expect([pick.on(), pick.has("a"), pick.count()]).toEqual([true, true, 1]);
    expect(pick.toggle(p("b"))).toBe(true);
    expect(pick.toggle(p("a"))).toBe(true);
    expect(pick.picks().map((x) => x.itemId)).toEqual(["b"]);
    expect(pick.toggle(p("b"))).toBe(true);
    expect([pick.on(), pick.count()]).toEqual([true, 0]); // none picked: still picking, Forward is off
    pick.end();
    expect([pick.on(), pick.count()]).toEqual([false, 0]);
    expect(seen).toHaveBeenCalledTimes(5);
  });

  it(`picks at most ${FORWARD_MOST}`, () => {
    const pick = messagePick();
    pick.start(p("0"));
    for (let i = 1; i < FORWARD_MOST; i++) expect(pick.toggle(p(String(i)))).toBe(true);
    expect(pick.toggle(p("one too many"))).toBe(false);
    expect([pick.count(), pick.refusals()]).toEqual([FORWARD_MOST, 1]);
    expect(pick.toggle(p("0"))).toBe(true); // unpicking still works
  });

  it("puts them in the chat's order, not the order they were picked", () => {
    const items = [{ id: "u#1" }, { id: "r#2" }, { id: "u#3" }, { id: "r#4" }];
    expect(inChatOrder([p("r#4"), p("u#1"), p("gone"), p("r#2")], items).map((x) => x.itemId)).toEqual(["u#1", "r#2", "r#4", "gone"]);
  });
});
