// @vitest-environment jsdom
// UI version 9 as the web draws it (docs/extensions/extensions.md "UI version 9", #9337): a tile's gauge is a ring
// filled to its ratio in its named color, the tile's value and unit in its centre, the title below; read out as
// "title: value unit, N%". Its pixels at each width are checked in a real browser (web/e2e/extension-gauge.mjs); here,
// what is drawn, what is read out and what a press does.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readView, type UIAction } from "../../lib/extension-ui";
import type { UITile } from "../../lib/extension-ui-v6";
import { INERT, ViewSections } from "./ViewBody";
import { gaugePercent } from "./ViewBody6";
import { GaugeFace } from "./ViewBody9";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

const doc = (html: string) => new DOMParser().parseFromString(`<body>${html}</body>`, "text/html");
const face = (tile: UITile, compact?: boolean) => doc(renderToStaticMarkup(<GaugeFace tile={tile} compact={compact} />));
const arc = (d: Document) => d.querySelector("[data-gauge-arc]");

const QUOTA = {
  sections: [{
    title: "Quota left",
    footer: "5 hours resets in 1 h 49 min",
    items: [{ type: "grid", key: "rings", max_columns: 3, tiles: [
      { key: "a", title: "5 hours", value: "4.69", gauge: { ratio: 0.94, unit: "USD", color: "green" } },
      { key: "b", title: "Fable this week", value: "0.62", badge: { text: "Low", color: "amber" }, gauge: { ratio: 0.12, unit: "USD", color: "amber" }, action: { id: "open_limit", values: { limit: "fable" } } },
      { key: "c", title: "Keys", value: "3", icon: "lock" },
    ] }],
  }],
};

describe("UI 9: a gauge's face", () => {
  it("fills the ring to the ratio in its color, the value and unit in the centre, the title below", () => {
    const d = face({ title: "5 hours", value: "4.69", gauge: { ratio: 0.94, unit: "USD", color: "green" } });
    const ring = arc(d)!;
    expect(ring.getAttribute("stroke")).toBe("var(--hue-green)");
    expect(ring.getAttribute("pathLength")).toBe("100");
    expect(ring.getAttribute("stroke-dashoffset")).toBe("6");
    expect(ring.getAttribute("stroke-linecap")).toBe("round");
    // the track is the color's tint, and the ring's drawing is hidden: the tile reads it out
    expect(d.querySelector("svg circle")?.getAttribute("stroke")).toContain("var(--hue-green) 14%");
    expect(d.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
    const texts = [...d.querySelectorAll("[data-gauge] span")].filter((s) => !s.children.length).map((s) => s.textContent);
    expect(texts).toEqual(["4.69", "USD", "5 hours"]);
    // design-system type: the value in a named size, the unit and the title in the tokens, no pixel sizes
    expect(d.body.innerHTML).not.toMatch(/text-\[\d/);
    expect(d.querySelector("[data-gauge]")?.innerHTML).toMatch(/text-xl[^"]*tabular-nums|tabular-nums[^"]*text-xl/);
  });

  it("eases to a new ratio, and not with Reduce Motion", () => {
    const cls = arc(face({ title: "t", gauge: { ratio: 0.5 } }))!.getAttribute("class")!;
    expect(cls).toContain("transition-[stroke-dashoffset]");
    expect(cls).toContain("motion-reduce:transition-none");
  });

  it("shows the percentage without a value, and no unit under it", () => {
    const d = face({ title: "Disk", subtitle: "of 512 GB", gauge: { ratio: 0.5, unit: "GB" } });
    const texts = [...d.querySelectorAll("[data-gauge] span")].filter((s) => !s.children.length).map((s) => s.textContent);
    expect(texts).toEqual(["50%", "Disk", "of 512 GB"]);
  });

  it("draws no arc at 0 and a whole one at 1, and holds a ratio past them to them", () => {
    expect(arc(face({ title: "t", gauge: { ratio: 0 } }))).toBeNull();
    expect(face({ title: "t", gauge: { ratio: 0 } }).querySelectorAll("svg circle")).toHaveLength(1);
    expect(arc(face({ title: "t", gauge: { ratio: 1 } }))?.getAttribute("stroke-dashoffset")).toBe("0");
    expect(arc(face({ title: "t", gauge: { ratio: 1.5 } }))?.getAttribute("stroke-dashoffset")).toBe("0");
    expect(arc(face({ title: "t", gauge: { ratio: -1 } }))).toBeNull();
  });

  it("leaves out a unit that is not a word", () => {
    const d = face({ title: "t", value: "3", gauge: { ratio: 0.5, unit: { x: 1 } } });
    expect([...d.querySelectorAll("[data-gauge] span")].filter((s) => !s.children.length).map((s) => s.textContent)).toEqual(["3", "t"]);
  });

  it("takes the tile's color without one of its own, else the accent", () => {
    expect(arc(face({ title: "t", color: "indigo", gauge: { ratio: 0.5 } }))?.getAttribute("stroke")).toBe("var(--hue-indigo)");
    expect(arc(face({ title: "t", gauge: { ratio: 0.5 } }))?.getAttribute("stroke")).toBe("var(--accent)");
    // an unknown name is no color: never drawn as given
    expect(arc(face({ title: "t", gauge: { ratio: 0.5, color: "#ff0000" } }))?.getAttribute("stroke")).toBe("var(--accent)");
  });

  it("is smaller in a card", () => {
    const d = face({ title: "t", value: "1", gauge: { ratio: 0.5 } }, true);
    expect(d.querySelector("[data-gauge] > span")?.className).toContain("size-20");
    expect(d.querySelector("[data-gauge]")?.innerHTML).toContain("text-lg");
  });

  it("rounds the percentage as the hub does", () => {
    expect([0, 0.004, 0.005, 0.5, 0.999, 1, 1.5, -1].map(gaugePercent)).toEqual([0, 0, 1, 50, 100, 100, 100, 0]);
  });
});

describe("UI 9: a gauge tile in a grid", () => {
  let host: HTMLDivElement;
  let root: Root;
  beforeEach(() => {
    host = document.createElement("div");
    document.body.append(host);
    root = createRoot(host);
  });
  afterEach(() => {
    act(() => root.unmount());
    host.remove();
  });

  it("draws the ring from its own chunk, reads out as a meter, and a pressable one presses its action", async () => {
    const pressed: UIAction[] = [];
    await act(async () => root.render(<ViewSections view={readView(QUOTA)} {...INERT} onPress={(a) => pressed.push(a)} />));
    await vi.waitFor(() => expect(host.querySelectorAll("[data-gauge]")).toHaveLength(2), { timeout: 30_000 });
    const meter = host.querySelector("[role=meter]")!;
    expect(meter.getAttribute("aria-label")).toBe("5 hours：4.69 USD，94%");
    expect([meter.getAttribute("aria-valuenow"), meter.getAttribute("aria-valuemin"), meter.getAttribute("aria-valuemax")]).toEqual(["94", "0", "100"]);
    const button = [...host.querySelectorAll("button")].find((b) => b.getAttribute("aria-label")?.startsWith("Fable this week"))!;
    expect(button.getAttribute("aria-label")).toBe("Fable this week：0.62 USD，12%");
    // its badge stays on the tile's corner
    expect(button.parentElement?.textContent).toContain("Low");
    act(() => button.click());
    expect(pressed).toEqual([{ id: "open_limit", values: { limit: "fable" } }]);
    // a tile without a gauge is drawn and read out as before
    const keys = host.querySelector("[role=group][aria-label^=Keys]")!;
    expect(keys.getAttribute("aria-label")).toBe("Keys，3");
    expect(keys.querySelector("[data-gauge]")).toBeNull();
    // the section's footer is the card's
    expect(host.textContent).toContain("5 hours resets in 1 h 49 min");
  }, 60_000);
});
