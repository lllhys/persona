// @vitest-environment jsdom
// UI version 8 as the web draws it (docs/extensions/extensions.md "UI version 8", #9158): a columns view is a grid of cards
// with their titles inside, a section that spans the row, a section's icon and its link beside the title (in every
// layout), and rows side by side in a section. The grid's columns at each width are checked in a real browser
// (web/e2e/extension-columns.mjs); here, what is drawn and what a press does.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { readView, type UIAction } from "../../lib/extension-ui";
import { INERT, ViewSections } from "./ViewBody";
import { Sections8 } from "./ViewBody8";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

const row = (key: string, title: string) => ({ type: "row", key, title, action: { id: "open", values: { row: key } } });
const TODAY = {
  title: "Today",
  layout: "columns",
  sections: [
    { key: "needs", title: "Needs me", span: "full", icon: "bell", icon_color: "orange", columns: 2, link: { title: "All", action: { id: "all", push: { state: "needs" } } },
      items: [{ type: "text", key: "s", text: "4 things wait for you", style: "secondary" }, row("n1", "Approve"), row("n2", "Reply"), row("n3", "Sign")] },
    { key: "calendar", title: "Calendar", icon: "calendar", link: { title: "Calendar", action: { id: "calendar" } }, items: [row("c1", "Weekly sync"), row("c2", "3 more this week")] },
    { key: "lark", title: "Lark today", items: [row("l1", "Design team")], footer: "Updated at 9:00" },
  ],
};
const doc = (html: string) => new DOMParser().parseFromString(`<body>${html}</body>`, "text/html");
const draw = (raw: unknown, extra: Partial<Parameters<typeof Sections8>[0]> = {}) => doc(renderToStaticMarkup(<Sections8 view={readView(raw)} {...INERT} {...extra} />));

describe("UI 8: the columns layout", () => {
  it("draws a grid of cards in reading order, the full one across the row", () => {
    const d = draw(TODAY);
    const grid = d.querySelector("[data-columns]")!;
    expect(grid.className).toContain("grid-cols-1");
    expect(grid.className).toMatch(/@min-\[\d+rem\]:grid-cols-2/);
    expect(grid.className).toMatch(/@min-\[\d+rem\]:grid-cols-3/);
    const cells = [...grid.children];
    expect(cells.map((c) => c.querySelector("h3")?.textContent)).toEqual(["Needs me", "Calendar", "Lark today"]);
    expect(cells[0].className).toContain("col-span-full");
    expect(cells[1].className).not.toContain("col-span-full");
    // each cell is a card (the grid stretches a row's cells to its tallest), its section without a card of its own
    for (const c of cells) expect(c.className).toMatch(/rounded-card.*bg-surface.*shadow-card/);
    expect(cells[1].querySelector(".rounded-card")).toBeNull();
  });

  it("puts a card's title, icon and link inside it, the link's words with ›", () => {
    const d = draw(TODAY);
    const head = d.querySelectorAll("[data-columns] section")[1].firstElementChild!;
    expect(head.querySelector("h3")?.className).toContain("text-meta");
    expect(head.querySelector("svg")).not.toBeNull();
    expect(head.querySelector("button")?.textContent).toBe("Calendar");
    // a card without icon or link still has its title inside, as a card's head
    const lark = d.querySelectorAll("[data-columns] section")[2];
    expect(lark.firstElementChild?.querySelector("h3")?.textContent).toBe("Lark today");
    expect(lark.firstElementChild?.querySelector("button")).toBeNull();
    expect(lark.querySelector("p:last-child")?.className).toContain("text-submeta");
  });

  it("colors the icon with its named color and leaves an unknown icon out", () => {
    const d = draw(TODAY);
    expect(d.querySelector("[data-columns] section svg")?.getAttribute("style")).toContain("var(--hue-orange)");
    const unknown = draw({ layout: "columns", sections: [{ title: "Odd", icon: "cat", items: [row("a", "A")] }] });
    expect(unknown.querySelector("section h3")?.previousElementSibling).toBeNull();
  });

  it("sets a section's rows side by side, everything else across the card", () => {
    const d = draw(TODAY);
    const needs = d.querySelector("[data-columns] section")!;
    const grid = needs.querySelector(".\\@container > div")!;
    expect(grid.className).toContain("@min-[36rem]:grid-cols-2");
    const [summary, ...rows] = [...grid.children];
    expect(summary.className).toContain("col-span-full");
    // side by side, the rows have no hairline between them; one under another (narrow), as any list
    expect(rows.map((r) => r.className)).toEqual(["@min-[36rem]:border-t-0", "border-t border-line @min-[36rem]:border-t-0", "border-t border-line @min-[36rem]:border-t-0"]);
    // a section without columns keeps its rows as before
    expect(d.querySelectorAll("[data-columns] section")[1].querySelector(".\\@container")).toBeNull();
  });

  it("is a stack in a card or inside another page", () => {
    for (const where of [{ compact: true }, { embedded: true }]) {
      const d = draw(TODAY, where);
      expect(d.querySelector("[data-columns]")).toBeNull();
      expect(d.querySelectorAll("section")).toHaveLength(3);
      // the icon and the link are still there, beside the plain title
      expect(d.querySelectorAll("section")[1].querySelector("h3 + button")?.textContent).toBe("Calendar");
    }
  });
});

describe("UI 8: a section's icon and link in other layouts", () => {
  it("draws them where the plain title is, and leaves a plain title as it was", () => {
    const view = { sections: [
      { title: "This week", icon: "chart", link: { title: "History", action: { id: "history" } }, items: [{ type: "metric", label: "Spent", value: "¥42" }] },
      { title: "Plain", items: [{ type: "text", text: "x" }] },
    ] };
    const d = draw(view);
    const [linked, plain] = [...d.querySelectorAll("section")];
    const head = linked.firstElementChild!;
    expect(head.querySelector("svg")).not.toBeNull();
    expect(head.querySelector("h3")?.className).toContain("text-label");
    expect(head.querySelector("button")?.textContent).toBe("History");
    expect(plain.firstElementChild?.outerHTML).toBe('<h3 class="font-medium text-ink-3 mb-1.5 px-1 text-[12px]">Plain</h3>');
  });
});

describe("UI 8: drawn from its own chunk", () => {
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

  it("ViewSections hands a view with UI version 8 to ViewBody8, and the link presses its action", async () => {
    const pressed: UIAction[] = [];
    await act(async () => root.render(<ViewSections view={readView(TODAY)} {...INERT} onPress={(a) => pressed.push(a)} />));
    for (let i = 0; i < 50 && !host.querySelector("[data-columns]"); i++) await act(async () => { await new Promise((r) => setTimeout(r, 20)); });
    expect(host.querySelector("[data-columns]")).not.toBeNull();
    const link = [...host.querySelectorAll("button")].find((b) => b.textContent === "All")!;
    act(() => link.click());
    expect(pressed).toEqual([{ id: "all", push: { state: "needs" } }]);
  }, 60_000);

  it("keeps the link still while an action is on its way, with a spinner on its own", () => {
    const busy = draw(TODAY, { busy: "calendar" });
    const link = busy.querySelectorAll("[data-columns] section")[1].querySelector("h3 + button")!;
    expect(link.hasAttribute("disabled")).toBe(true);
    // the spinner in place of the ›
    expect(link.querySelector("svg")).toBeNull();
    expect(link.querySelector("span[aria-hidden]")?.className).toContain("rounded-full");
  });
});
