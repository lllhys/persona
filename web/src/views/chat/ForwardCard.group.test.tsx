// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import fixture from "../../../../spec/fixtures/forward/several.json";
import { initI18n } from "../../i18n";
import { forwardRef } from "../../lib/forward-block";
import { loadPages } from "../../lib/lazy-page";
import { addContext, getContexts, removeContext } from "../../lib/links";
import { parseUserText } from "../../lib/turns";
import { ForwardBars } from "./ForwardCard";
import { ForwardGroupBlock } from "./ForwardGroup";

// Several forwards of one chat (#3838): one bar over the box with a line each and one × for all of them, one card in the
// bubble with the first three and "N more".

vi.mock("../../lib/router", () => ({ navigate: vi.fn() }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// the group's bar is its own chunk: Forward to, which puts a group there, has loaded it by then
beforeAll(() => loadPages(), 60_000);

let host: HTMLDivElement;
let root: Root;
const key = "test/forward-group";

beforeEach(async () => {
  await initI18n("en");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(async () => {
  act(() => root.unmount());
  host.remove();
  getContexts(key).forEach((r) => removeContext(key, r.id));
  await initI18n("zh-Hans");
});

const source = (item: string, sid = "a1f3c2d4-0000-4000-8000-000000000006") => ({ mid: "macbook", sid, item, role: "agent" as const, author: "Claude", chat: "Network tests", machine: "MacBook Pro" });

describe("several forwards over the box", () => {
  it("are one bar per chat, a line each, and × drops them all", () => {
    for (const [i, words] of ["One", "Two", "Three", "Four"].entries()) addContext(key, { ...forwardRef(words, source(`e#${i}`)), route: "To Codex on Mac Studio" });
    addContext(key, forwardRef("Other chat", source("o#1", "b2e4d6f8-0000-4000-8000-000000000003")));
    act(() => root.render(<ForwardBars draftKey={key} />));
    const bars = [...host.querySelectorAll<HTMLElement>("[data-forward-bar]")];
    expect(bars.map((b) => b.dataset.forwardGroup ?? "1")).toEqual(["4", "1"]);
    expect(bars[0].textContent).toContain("4 forwarded messages · Network tests");
    expect(bars[0].textContent).toContain("Claude: One");
    expect(bars[0].textContent).toContain("1 more");
    expect(bars[0].querySelectorAll("[data-forward-route]")).toHaveLength(1);
    act(() => bars[0].querySelector("button")!.click());
    expect(getContexts(key).map((r) => r.text)).toEqual(["Other chat"]);
  });
});

describe("several forwards in a bubble", () => {
  it("are one card with the first three, and the rest on More", () => {
    const one = (fixture as { read: { message: string }[] }).read[0].message;
    const blocks = parseUserText(one).contexts;
    const group = [...blocks, ...blocks.slice(0, 1)];
    act(() => root.render(<ForwardGroupBlock group={group} />));
    const card = host.querySelector<HTMLElement>("[data-forward-group]")!;
    expect(card.dataset.forwardGroup).toBe("4");
    expect(card.textContent).toContain("4 forwarded messages · Network tests");
    expect(card.textContent).toContain("Me: How many times does it retry?");
    const more = [...card.querySelectorAll("button")].find((b) => b.textContent === "1 more")!;
    expect(card.querySelectorAll("[aria-label]")).toHaveLength(3);
    act(() => more.click());
    expect(card.querySelectorAll("[aria-label]")).toHaveLength(4);
  });
});
