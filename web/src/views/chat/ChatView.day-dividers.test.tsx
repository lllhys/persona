// @vitest-environment jsdom
// Day separators in the Agent transcript (#8160): one divider at the first message of each new calendar day, with
// day-only keys so an older page inserted above and a message arriving across midnight neither move nor duplicate a
// divider. The chat-start center line and the engine/model dividers are separate.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Item, Session } from "../../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  g.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
  (g.CSS as { escape?: (s: string) => string } | undefined) ?? (g.CSS = {});
  (g.CSS as { escape?: (s: string) => string }).escape ??= (s: string) => s;
});

const transcript = vi.hoisted(() => ({ items: [] as Item[], loading: false, error: "", hasMore: false, streaming: "", loadingEarlier: false, loadEarlier: async () => {}, expandFold: async () => {}, reload: () => {} }));
vi.mock("./useTranscript", () => ({ useTranscript: () => transcript }));
vi.mock("../../lib/panel", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/panel")>(), usePanel: () => null }));

const { ChatView } = await import("./ChatView");
const { getState, reset, upsertSession } = await import("../../lib/store");
const { forgetExpanded } = await import("./expanded");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

// Local calendar times: the divider keys are local YYYY-MM-DD, so the fixtures stay independent of the test zone.
const at = (dayOffset: number, h: number, m = 0) => {
  const d = new Date();
  d.setDate(d.getDate() + dayOffset);
  d.setHours(h, m, 0, 0);
  return d.toISOString();
};
const dayKey = (dayOffset: number) => new Date(Date.parse(at(dayOffset, 9))).toLocaleDateString("sv");
let seq = 0;
const pair = (dayOffset: number, h: number, word: string): Item[] => {
  const n = ++seq;
  return [
    { id: `u${n}#0`, seq: n * 2 - 1, role: "user", kind: "text", text: `${word}-q`, ts: at(dayOffset, h) },
    { id: `a${n}#0`, seq: n * 2, role: "assistant", kind: "text", text: `${word}-a`, ts: at(dayOffset, h, 30) },
  ];
};

let host: HTMLDivElement;
let root: Root;
const render = () => act(() => root.render(<ChatView mid="m" sid="s1" onToggleSidebar={() => {}} />));
const dividerIds = () => [...host.querySelectorAll("[data-day-divider]")].map((el) => el.getAttribute("data-day-divider"));
/** The transcript's reading order: user bubbles, replies and the day dividers between them. */
const reading = () =>
  [...host.querySelectorAll("[data-bubble-text], [data-answer], [data-day-divider]")].map((el) =>
    el.hasAttribute("data-day-divider") ? `— ${el.getAttribute("data-day-divider")}` : (el.textContent ?? "").trim(),
  );

beforeEach(() => {
  // Pin a midday clock: the "today" fixtures use fixed hours, and withDayDividers hides lines dated after now; without
  // this the suite flakes in the first minute after midnight (the future-clock guard would hide today's divider).
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 10, 15, 12, 0, 0));
  localStorage.clear();
  forgetExpanded();
  seq = 0;
  transcript.items = [];
  transcript.loading = false;
  transcript.hasMore = false;
  Object.assign(getState(), { loaded: true });
  vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ items: [] }), { status: 200, headers: { "Content-Type": "application/json" } })));
  upsertSession({ machine_id: "m", session_id: "s1", title: "Multi-day", cwd: "/w", status: "idle", state: "idle", agent: "claude" } as Session);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("day separators in the Agent transcript", () => {
  it("shows exactly one divider per boundary across four days, labelled full date, yesterday and today", async () => {
    transcript.items = [...pair(-3, 9, "old"), ...pair(-2, 9, "older"), ...pair(-1, 9, "yesterday"), ...pair(0, 9, "today")];
    await render();
    expect(dividerIds()).toEqual([`day-${dayKey(-2)}`, `day-${dayKey(-1)}`, `day-${dayKey(0)}`]);
    const labels = [...host.querySelectorAll("[data-day-divider]")].map((el) => el.textContent ?? "");
    expect(labels[0]).toMatch(/月.*日/); // the full date, not the Today/Yesterday wording
    expect(labels[0].startsWith("今天") || labels[0].startsWith("昨天")).toBe(false);
    expect(labels[1].startsWith("昨天")).toBe(true);
    expect(labels[2].startsWith("今天")).toBe(true);
    // each divider stands immediately before the first message of its day, after the previous day's reply
    const order = reading();
    expect(order.indexOf(`— day-${dayKey(-1)}`)).toBe(order.indexOf("yesterday-q") - 1);
    expect(order.indexOf(`— day-${dayKey(0)}`)).toBe(order.indexOf("today-q") - 1);
    expect(order.indexOf("older-a")).toBeLessThan(order.indexOf(`— day-${dayKey(-1)}`));
    // the chat-start center line is still there once, separate from the dividers (this session names its cwd)
    expect(host.textContent?.match(/在 \/w 开始/g)).toHaveLength(1);
    expect(host.querySelector("[data-engine-switch]")).toBeNull();
  });

  it("inserts the older day's divider when an earlier page loads, without shifting or duplicating existing ones", async () => {
    transcript.items = [...pair(-1, 9, "yesterday"), ...pair(0, 9, "today")];
    await render();
    const before = dividerIds();
    expect(before).toEqual([`day-${dayKey(0)}`]);
    // the older page arrives above the lines already on screen
    transcript.items = [...pair(-3, 9, "old"), ...pair(-2, 9, "older"), ...transcript.items];
    await render();
    const after = dividerIds();
    expect(after).toEqual([`day-${dayKey(-2)}`, `day-${dayKey(-1)}`, `day-${dayKey(0)}`]);
    expect(new Set(after).size).toBe(after.length);
    for (const id of before) expect(after.filter((x) => x === id)).toEqual([id]); // stable, still once
  });

  it("adds a divider once when a message arrives across midnight, and keeps it stable on re-render", async () => {
    transcript.items = [
      { id: "u1#0", seq: 1, role: "user", kind: "text", text: "night-q", ts: at(-1, 23, 50) },
      { id: "a1#0", seq: 2, role: "assistant", kind: "text", text: "night-a", ts: at(-1, 23, 55) },
    ];
    await render();
    expect(dividerIds()).toEqual([]);
    // the next exchange lands after midnight, on the new day
    transcript.items = [...transcript.items,
      { id: "u2#0", seq: 3, role: "user", kind: "text", text: "morning-q", ts: at(0, 0, 10) },
      { id: "a2#0", seq: 4, role: "assistant", kind: "text", text: "morning-a", ts: at(0, 0, 15) },
    ];
    await render();
    expect(dividerIds()).toEqual([`day-${dayKey(0)}`]);
    const order = reading();
    expect(order.indexOf(`— day-${dayKey(0)}`)).toBeLessThan(order.indexOf("morning-q"));
    // a re-render with the same lines flashes no second divider and keeps the same key
    await render();
    expect(dividerIds()).toEqual([`day-${dayKey(0)}`]);
  });

  it("draws no divider for a transcript that stays on one day", async () => {
    transcript.items = [...pair(0, 8, "early"), ...pair(0, 14, "noon"), ...pair(0, 23, "late")];
    await render();
    expect(dividerIds()).toEqual([]);
    expect(reading()).toEqual(["early-q", "early-a", "noon-q", "noon-a", "late-q", "late-a"]);
  });
});
