import { describe, expect, it } from "vitest";
import {
  canRecall, composing, HISTORY_MAX, opensSearch, readHistory, recallIndexOf, recallStep, recordSent, withSent,
  type KeyLike, type SentEntry,
} from "./send-history";
import { moveActive, readAllHistory, searchAction, searchSent } from "./send-history-search";

function memory() {
  const m = new Map<string, string>();
  return {
    getItem: (k: string) => m.get(k) ?? null,
    setItem: (k: string, v: string) => void m.set(k, v),
    get length() {
      return m.size;
    },
    key: (i: number) => [...m.keys()][i] ?? null,
  };
}

const key = (k: string, extra: Partial<KeyLike> = {}): KeyLike => ({ key: k, ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, ...extra });
const ctrl = (k: string, extra: Partial<KeyLike> = {}) => key(k, { ctrlKey: true, ...extra });

describe("the sent history store", () => {
  it("keeps the last 100 messages of a conversation, oldest first", () => {
    const store = memory();
    for (let i = 0; i < HISTORY_MAX + 20; i++) recordSent("m/s", `message ${i}`, store, i);
    const list = readHistory("m/s", store);
    expect(list).toHaveLength(HISTORY_MAX);
    expect(list[0].text).toBe("message 20");
    expect(list.at(-1)?.text).toBe(`message ${HISTORY_MAX + 19}`);
  });

  it("does not keep blank messages and moves a repeated one to the end", () => {
    const a: SentEntry = { text: "a", at: 1 };
    expect(withSent([a], "   ", 2)).toEqual([a]);
    expect(withSent([a, { text: "b", at: 2 }], "a", 3)).toEqual([{ text: "b", at: 2 }, { text: "a", at: 3 }]);
  });

  it("keeps each conversation apart", () => {
    const store = memory();
    recordSent("m/one", "hello", store, 1);
    recordSent("m/two", "world", store, 2);
    expect(readHistory("m/one", store).map((e) => e.text)).toEqual(["hello"]);
    expect(readHistory("m/two", store).map((e) => e.text)).toEqual(["world"]);
  });

  it("reads past damaged storage and survives a storage that refuses writes", () => {
    const store = memory();
    store.setItem("persona.sent.m/s", "{not json");
    expect(readHistory("m/s", store)).toEqual([]);
    store.setItem("persona.sent.m/s", JSON.stringify([{ text: 1, at: 1 }, { text: "ok", at: 2 }, null, { text: " ", at: 3 }]));
    expect(readHistory("m/s", store).map((e) => e.text)).toEqual(["ok"]);
    const full = { ...store, setItem: () => { throw new Error("quota"); } };
    expect(() => recordSent("m/s", "x", full, 5)).not.toThrow();
  });

  it("lists every conversation's messages for the search, newest first", () => {
    const store = memory();
    recordSent("m/one", "old one", store, 1);
    recordSent("m/two", "newer two", store, 5);
    recordSent("m/one", "newest one", store, 9);
    store.setItem("draft:m/one", "unrelated");
    expect(readAllHistory(store).map((e) => e.text)).toEqual(["newest one", "newer two", "old one"]);
  });
});

describe("searching sent messages", () => {
  const list: SentEntry[] = [
    { text: "Deploy the web build to staging", at: 5 },
    { text: "部署 web 到测试环境", at: 4 },
    { text: "fix the flaky test", at: 3 },
    { text: "deploy the hub", at: 2 },
    { text: "deploy the hub", at: 1 },
  ];

  it("needs every space-separated word, in any order, ignoring case", () => {
    expect(searchSent(list, "DEPLOY web")).toEqual(["Deploy the web build to staging"]);
    expect(searchSent(list, "web deploy")).toEqual(["Deploy the web build to staging"]);
    expect(searchSent(list, "部署 测试")).toEqual(["部署 web 到测试环境"]);
    expect(searchSent(list, "deploy nothing")).toEqual([]);
  });

  it("lists the newest first, one row per text, for an empty query too", () => {
    expect(searchSent(list, "")).toEqual(["Deploy the web build to staging", "部署 web 到测试环境", "fix the flaky test", "deploy the hub"]);
    expect(searchSent(list, "  ")).toHaveLength(4);
  });

  it("stops at 100 results", () => {
    const many = Array.from({ length: 250 }, (_, i): SentEntry => ({ text: `item ${i}`, at: 250 - i }));
    expect(searchSent(many, "item")).toHaveLength(100);
    expect(searchSent(many, "item", 5)).toEqual(["item 0", "item 1", "item 2", "item 3", "item 4"]);
  });
});

describe("walking the history with the arrows", () => {
  it("walks only from an empty box or from the message it put there", () => {
    const base = { shown: null, firstLine: true, lastLine: true };
    expect(canRecall({ ...base, draft: "", dir: "up" })).toBe(true);
    expect(canRecall({ ...base, draft: "half typed", dir: "up" })).toBe(false);
    expect(canRecall({ draft: "sent before", shown: "sent before", dir: "up", firstLine: true, lastLine: true })).toBe(true);
    // edited after recalling: it is a draft again
    expect(canRecall({ draft: "sent before!", shown: "sent before", dir: "up", firstLine: true, lastLine: true })).toBe(false);
  });

  it("leaves the arrows to a multi-line message until the caret is on its first or last line", () => {
    const o = { draft: "a\nb", shown: "a\nb" };
    expect(canRecall({ ...o, dir: "up", firstLine: false, lastLine: true })).toBe(false);
    expect(canRecall({ ...o, dir: "up", firstLine: true, lastLine: false })).toBe(true);
    expect(canRecall({ ...o, dir: "down", firstLine: true, lastLine: false })).toBe(false);
    expect(canRecall({ ...o, dir: "down", firstLine: false, lastLine: true })).toBe(true);
  });

  it("steps older with up (stopping at the oldest) and newer with down (ending in an empty box)", () => {
    expect(recallStep(3, -1, "up")).toBe(2);
    expect(recallStep(3, 2, "up")).toBe(1);
    expect(recallStep(3, 0, "up")).toBe(0);
    expect(recallStep(3, 0, "down")).toBe(1);
    expect(recallStep(3, 2, "down")).toBe(-1);
    expect(recallStep(3, -1, "down")).toBe(-1);
    expect(recallStep(0, -1, "up")).toBe(-1);
  });

  it("goes on from the last message being edited (#4503): ↑ to the one before it, ↓ to an empty box", () => {
    const list: SentEntry[] = [{ text: "first", at: 1 }, { text: "second", at: 2 }, { text: "last", at: 3 }];
    // sent from here: the walk stands on it, as if ↑ had recalled it
    expect(recallIndexOf(list, "last")).toBe(2);
    expect(recallStep(list.length, recallIndexOf(list, "last"), "up")).toBe(1);
    expect(recallStep(list.length, recallIndexOf(list, "last"), "down")).toBe(-1);
    // sent from another device: just past the newest, so ↑ still shows the newest sent from here
    expect(recallIndexOf(list, "from the phone")).toBe(3);
    expect(recallStep(list.length, recallIndexOf(list, "from the phone"), "up")).toBe(2);
    expect(recallStep(list.length, recallIndexOf(list, "from the phone"), "down")).toBe(-1);
    // nothing sent from here
    expect(recallIndexOf([], "last")).toBe(0);
    expect(recallStep(0, 0, "down")).toBe(-1);
  });
});

describe("the search keys", () => {
  it("opens with Ctrl+R alone", () => {
    expect(opensSearch(ctrl("r"))).toBe(true);
    expect(opensSearch(ctrl("R"))).toBe(true);
    expect(opensSearch(key("r"))).toBe(false);
    expect(opensSearch(key("r", { metaKey: true }))).toBe(false); // ⌘R is the browser's reload
    expect(opensSearch(ctrl("r", { shiftKey: true }))).toBe(false); // the hard reload
    expect(opensSearch(ctrl("r", { altKey: true }))).toBe(false);
  });

  it("moves, picks and cancels like Moshi's panel", () => {
    expect(searchAction(key("ArrowUp"))).toBe("up");
    expect(searchAction(ctrl("r"))).toBe("up");
    expect(searchAction(ctrl("p"))).toBe("up");
    expect(searchAction(key("ArrowDown"))).toBe("down");
    expect(searchAction(ctrl("s"))).toBe("down");
    expect(searchAction(ctrl("n"))).toBe("down");
    expect(searchAction(key("Enter"))).toBe("pick");
    expect(searchAction(key("Tab"))).toBe("pick");
    expect(searchAction(key("Escape"))).toBe("cancel");
    expect(searchAction(ctrl("g"))).toBe("cancel");
  });

  it("leaves typing, Shift+Enter and other chords to the search box", () => {
    expect(searchAction(key("a"))).toBeNull();
    expect(searchAction(key("Enter", { shiftKey: true }))).toBeNull();
    expect(searchAction(key("Tab", { shiftKey: true }))).toBeNull();
    expect(searchAction(ctrl("a"))).toBeNull();
    expect(searchAction(key("p", { metaKey: true }))).toBeNull();
  });

  it("ignores every key while an input method composes", () => {
    expect(composing({ isComposing: true })).toBe(true);
    expect(composing({ keyCode: 229 })).toBe(true);
    expect(composing({ isComposing: false, keyCode: 13 })).toBe(false);
    expect(searchAction(key("Enter", { isComposing: true }))).toBeNull();
    expect(searchAction(key("ArrowDown", { keyCode: 229 }))).toBeNull();
    expect(opensSearch(ctrl("r", { isComposing: true }))).toBe(false);
  });

  it("wraps the highlighted row", () => {
    expect(moveActive(0, 3, "up")).toBe(2);
    expect(moveActive(2, 3, "down")).toBe(0);
    expect(moveActive(1, 3, "down")).toBe(2);
    expect(moveActive(0, 0, "down")).toBe(0);
  });
});
