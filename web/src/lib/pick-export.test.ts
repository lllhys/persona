// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import type { ForwardPick } from "./forward";
import { FORWARD_MOST, INCLUDE_KEY, messagePick, questionsOf } from "./message-pick";
import { copiedText, exportSubtitle, pageHeight, pagePlan, pickQuestions, type ExportMessage } from "./pick-export";
import type { Item } from "./types";
import { t } from "../i18n";

// Select messages, Include questions, Copy and Export image (#9469; iOS ChatExportTests).

let seq = 0;
function item(role: "user" | "assistant", text: string, extra: Partial<Item> = {}): Item {
  seq++;
  return { id: `${role === "user" ? "u" : "r"}${seq}#0`, seq, role, kind: "text", text, ts: new Date(1_800_000_000_000 + seq * 60_000).toISOString(), ...extra };
}

const p = (itemId: string): ForwardPick => ({ itemId, text: itemId, isUser: false });

afterEach(() => localStorage.removeItem(INCLUDE_KEY));

describe("Include questions: what each reply brings (pickQuestions)", () => {
  it("is the reply's message and those before it that got no reply, in order", () => {
    const items = [item("user", "Why is the widget slow?"), item("user", "and its battery use"), item("assistant", "Found it."),
      item("user", "Would 15 minutes cost more?"), item("assistant", "A little.")];
    const map = pickQuestions(items);
    const [q1, q2, a1, q3, a2] = items.map((i) => i.id);
    expect(map.get(a1)?.map((q) => q.itemId)).toEqual([q1, q2]);
    expect(map.get(a2)?.map((q) => q.itemId)).toEqual([q3]);
    expect(map.get(q1)).toBeUndefined();
    expect(map.get(a1)?.[0]).toMatchObject({ isUser: true, text: "Why is the widget slow?" });
  });

  it("never counts a /loop tick", () => {
    const items = [item("user", "/loop 5m check the build"), item("assistant", "Still green.")];
    expect(pickQuestions(items).get(items[1].id)).toBeUndefined();
  });

  it("brings only what is not picked, each once", () => {
    const questions = new Map([["a1", [p("q1")]], ["a2", [p("q1"), p("q2")]]]);
    expect(questionsOf([p("a1"), p("a2")], questions).map((q) => q.itemId)).toEqual(["q1", "q2"]);
    expect(questionsOf([p("a2"), p("q2")], questions).map((q) => q.itemId)).toEqual(["q1"]);
  });
});

describe("picking with Include questions (lib/message-pick.ts)", () => {
  const questions = new Map(Array.from({ length: 20 }, (_, i) => [`a${i}`, [p(`q${i}`)]] as const));

  it("is on by default, offered while a picked reply's question is not picked, and remembered", () => {
    const pick = messagePick();
    pick.setQuestions(() => questions);
    pick.start(p("a0"));
    expect([pick.include(), pick.offered(), pick.brought().map((q) => q.itemId), pick.isBrought("q0")]).toEqual([true, true, ["q0"], true]);
    pick.toggle(p("q0"));
    expect([pick.offered(), pick.brought()]).toEqual([false, []]);
    pick.toggle(p("q0"));
    expect(pick.setInclude(false)).toBe(true);
    expect([pick.brought(), pick.offered()]).toEqual([[], true]);
    const next = messagePick();
    next.start(p("a1"));
    expect(next.include()).toBe(false);
  });

  it(`never sends more than ${FORWARD_MOST}, the questions brought counted`, () => {
    const pick = messagePick();
    pick.setQuestions(() => questions);
    pick.start(p("a0"));
    let picked = 1;
    for (let i = 1; i < 20; i++) if (pick.toggle(p(`a${i}`))) picked++;
    expect(picked).toBe(FORWARD_MOST / 2);
    expect(pick.count() + pick.brought().length).toBe(FORWARD_MOST);
    // off: the replies alone fit; on again would not
    expect(pick.setInclude(false)).toBe(true);
    for (let i = 15; i < 20; i++) pick.toggle(p(`a${i}`));
    expect(pick.count()).toBe(20);
    expect(pick.setInclude(true)).toBe(false);
    expect(pick.include()).toBe(false);
  });

  it("reads the questions when Forward to's Select messages… started it before the chat said them", () => {
    const pick = messagePick();
    pick.start(p("a3"));
    expect(pick.offered()).toBe(false);
    pick.setQuestions(() => questions);
    expect(pick.brought().map((q) => q.itemId)).toEqual(["q3"]);
  });
});

describe("Copy and Export image's text and pages (lib/pick-export.ts)", () => {
  const m = (role: ExportMessage["role"], author: string, text: string, time?: string): ExportMessage => ({ id: author + text, role, author, text, time, images: [] });

  it("copies who wrote each, then its words, a blank line between", () => {
    const text = copiedText([m("user", "Me", "Why is it slow?"), m("agent", "Claude", "## Found it\n**30 minutes**.")]);
    const me = t("chat-forwardto.copied_author", { author: "Me" }), claude = t("chat-forwardto.copied_author", { author: "Claude" });
    expect(text).toBe(`${me}\nWhy is it slow?\n\n${claude}\n## Found it\n**30 minutes**.`);
    expect(me.startsWith("Me") && !me.includes("copied_author")).toBe(true);
  });

  it("breaks pages between blocks, and cuts one taller than a page", () => {
    expect(pagePlan([100, 300, 300, 50], [0, 10, 10, 10], 500).map((pg) => pg.map((s) => s.block))).toEqual([[0, 1], [2, 3]]);
    const tall = pagePlan([200, 1000], [0, 10], 400).flat().filter((s) => s.block === 1);
    expect(tall.map((s) => [s.from, s.to])).toEqual([[0, 400], [400, 800], [800, 1000]]);
  });

  it("keeps a page within the canvas's pixels and memory", () => {
    const h = pageHeight(390, 3);
    expect(h * 3).toBeLessThanOrEqual(16_000);
    expect(390 * 3 * h * 3 * 4).toBeLessThanOrEqual(64 * 1024 * 1024);
    expect(pageHeight(390, 2) * 2).toBeLessThanOrEqual(16_000);
  });

  it("says the day, or the days, and who answers", () => {
    const one = exportSubtitle([m("user", "Me", "a", "2026-11-17T09:00:00Z")], "Claude", "en");
    expect(one).toMatch(/^November 1[67], 2026 · Claude$/);
    const two = exportSubtitle([m("user", "Me", "a", "2026-11-14T09:00:00Z"), m("agent", "Claude", "b", "2026-11-17T09:00:00Z")], "Claude", "en");
    expect(two).toContain(" – ");
  });
});
