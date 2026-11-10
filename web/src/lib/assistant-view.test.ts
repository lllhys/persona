import { describe, expect, it } from "vitest";
import { assistantView, cardNumber, cardText, cardView } from "./assistant-view";

// The cases every app is held to (schema/extension/examples/persona-card/*.json, from sdk/go/wire/personacard.go): a card
// and the view it becomes, or null when it stays a code block.
const CASES = import.meta.glob("../../../schema/extension/examples/persona-card/*.json", { query: "?raw", import: "default", eager: true }) as Record<string, string>;

describe("persona-card → 组件（和每个 App 同一套）", () => {
  it("has the shared cases", () => {
    expect(Object.keys(CASES).length).toBeGreaterThanOrEqual(5);
  });
  for (const [path, text] of Object.entries(CASES)) {
    const name = path.split("/").pop();
    it(`turns ${name} into the same view`, () => {
      const c = JSON.parse(text) as { card: unknown; view: unknown };
      expect(cardView(c.card)).toEqual(c.view ?? null);
    });
  }

  it("writes numbers grouped, one decimal at most", () => {
    expect([2250, 7.5, -1200.34, 0, -0.01, 8432, 999.96, 1234567].map(cardNumber)).toEqual(["2,250", "7.5", "-1,200.3", "0", "0", "8,432", "1,000", "1,234,567"]);
  });

  it("a card that does not read stays code", () => {
    expect(cardView({ type: "stat" })).toBeNull();
    expect(cardView({ type: "pie" })).toBeNull();
    expect(assistantView("persona-card", '{"type":"stat","value":')).toBeNull(); // still being written
  });
});

describe("persona-view", () => {
  it("draws a view or one component, without what would act", () => {
    const v = assistantView("persona-view", JSON.stringify({
      title: "本周",
      sections: [{ items: [
        { type: "stat_grid", stats: [{ label: "今天", value: "1.2M" }] },
        { type: "button", title: "刷新", action: { id: "r" } },
        { type: "row", title: "详情", action: { id: "open" } },
        { type: "link", title: "文档", url: "https://example.com" },
      ] }],
    }));
    expect(v?.title).toBe("本周");
    expect(v?.sections[0].items.map((i) => i.type)).toEqual(["stat_grid", "row", "link"]);
    expect(v?.sections[0].items[1].action).toBeUndefined();
    expect(assistantView("persona-view", JSON.stringify({ type: "markdown", text: "**hi**" }))?.sections[0].items[0].type).toBe("markdown");
    expect(assistantView("persona-view", "[]")).toBeNull();
  });

  it("draws persona-card blocks as cards too", () => {
    expect(assistantView("persona-card", JSON.stringify({ type: "stat", value: 3 }))?.sections[0].items[0]).toEqual({ type: "metric", label: "数值", value: "3" });
  });

  it("a card no app draws reads as words, never JSON", () => {
    const text = cardText('{"type":"pie","title":"今天的热量","total":{"value":2000,"unit":"kcal"},"segments":[{"label":"早餐","value":480,"note":"燕麦"},{"label":"午餐","value":720}],"note":"还能吃 800 kcal"}');
    expect(text).toBe("今天的热量\n合计: 2,000 kcal\n早餐: 480 kcal (燕麦)\n午餐: 720 kcal\n还能吃 800 kcal");
    expect(text).not.toContain("{");
    expect(cardText('{"type":"stat","value":')).toBeNull(); // still being written: stays code
    expect(cardText("[1,2]")).toBeNull();
  });

  it("draws the ring it declares (persona_card_ring)", () => {
    expect(cardView({ type: "ring", total: { value: 2000 }, segments: [{ label: "a", value: 500 }] })).not.toBeNull();
  });
});
