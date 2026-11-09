import { describe, expect, it } from "vitest";
import { DOC_RULES, MESSAGE_RULES, groupSelectionRule, moveSelectionLevel, selectionItemKey, validSelection, type KBSelection } from "./kb-selection";

export function selectionFixture(): KBSelection {
  return {
    version: 1,
    messages: { order: [["mentions", "mine"], ...MESSAGE_RULES.slice(2).map((r) => [r])], rules: Object.fromEntries(MESSAGE_RULES.map((name) => [name, { on: true, days: null, attachment_days: null, ...(name === "mentions" ? { include_all: false } : name === "mine" ? { context: 0 } : {}) }])) },
    docs: { order: [["mentions", "comment_mentions"], ["authored"], ["viewed"]], rules: Object.fromEntries(DOC_RULES.map((name) => [name, { on: true, days: null }])) },
    items: [{ source: "example-chat", kind: "chat", id: "chat/one", name: "Project notes", read: true, store: false, days: null }],
    history: { "example-chat": { back_days: 90 }, "example-docs": { back_days: null } },
  };
}

describe("knowledge selection document", () => {
  it("retains nulls and optional settings in a valid whole document", () => {
    expect(validSelection(selectionFixture())).toBe(true);
    expect(validSelection({ ...selectionFixture(), items: null, history: null })).toBe(true);
  });
  it.each([0, -1, 36501, 1.5, "3"])("rejects invalid days %s", (days) => {
    const s = selectionFixture();
    Object.assign(s.docs.rules.viewed, { days });
    expect(validSelection(s)).toBe(false);
  });
  it("refuses missing/duplicate rules and unsupported reads", () => {
    const s = selectionFixture();
    s.messages.order[0] = ["mine", "mine"];
    expect(validSelection(s)).toBe(false);
    for (const value of [null, {}, { ...selectionFixture(), version: 2 }, { ...selectionFixture(), items: undefined }]) expect(validSelection(value)).toBe(false);
  });
  it("validates context boundaries and stable composite item identity", () => {
    const s = selectionFixture();
    s.messages.rules.mine.context = 200;
    expect(validSelection(s)).toBe(true);
    s.messages.rules.mine.context = 201;
    expect(validSelection(s)).toBe(false);
    const item = selectionFixture().items![0];
    expect(selectionItemKey(item)).not.toBe(selectionItemKey({ ...item, source: "another-source" }));
    expect(selectionItemKey(item)).not.toBe(selectionItemKey({ ...item, kind: "doc" }));
    expect(validSelection({ ...selectionFixture(), items: [item, { ...item, name: "Renamed" }] })).toBe(false);
    expect(validSelection({ ...selectionFixture(), items: [{ ...item, read: false, store: true }] })).toBe(false);
  });
  it("reorders and groups without losing or duplicating a rule", () => {
    const order = selectionFixture().messages.order;
    const split = groupSelectionRule(order, "mentions", false);
    expect(split.slice(0, 2)).toEqual([["mine"], ["mentions"]]);
    const joined = groupSelectionRule(split, "mentions", true);
    expect(joined[0]).toEqual(["mine", "mentions"]);
    const moved = moveSelectionLevel(joined, 1, -1);
    expect(moved[0]).toEqual(["dm"]);
    expect(moved.flat().sort()).toEqual([...MESSAGE_RULES].sort());
    expect(order[0]).toEqual(["mentions", "mine"]);
    expect(moveSelectionLevel(order, 0, -1)).toEqual(order);
  });
});
