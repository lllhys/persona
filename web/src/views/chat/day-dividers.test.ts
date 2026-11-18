// The Agent transcript's day dividers (#8160): one divider at the first line of each new calendar day, keys derived
// from the day alone so pagination inserts and midnight arrivals never duplicate or move a divider.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { chatLineTime, withDayDividers, type ChatDayRow } from "./day-dividers";
import type { ChatLine, Segment } from "../../lib/turns";

// Pin a midday clock: the "today" fixtures use fixed hours, and withDayDividers hides lines dated after now; without
// this the suite flakes in the first minute after midnight (the future-clock guard would hide today's divider).
beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date(2026, 10, 15, 12, 0, 0));
});
afterEach(() => vi.useRealTimers());

// Local calendar times: the divider keys are local YYYY-MM-DD, so the fixtures stay independent of the test zone.
const iso = (dayOffset: number, h: number) => {
  const d = new Date();
  d.setDate(d.getDate() + dayOffset);
  d.setHours(h, 0, 0, 0);
  return d.toISOString();
};
const seg = (id: string, dayOffset: number, h: number): ChatLine => {
  const s = { id, user: { id: id + "u", text: id, images: [], ts: iso(dayOffset, h), contexts: [] }, start: iso(dayOffset, h), steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] } as Segment;
  return { kind: "segment", id, seg: s, live: false };
};
const dividerIds = (rows: ChatDayRow[]) => rows.filter((r) => r.kind === "day").map((r) => r.id);

describe("withDayDividers", () => {
  it("puts one divider over the first line of each later day, not over the first day", () => {
    const rows = withDayDividers([seg("s1", -2, 9), seg("s2", -1, 9), seg("s3", 0, 9)]);
    expect(dividerIds(rows)).toEqual([`day-${new Date(Date.parse(iso(-1, 9))).toLocaleDateString("sv")}`, `day-${new Date(Date.parse(iso(0, 9))).toLocaleDateString("sv")}`]);
    // the lines keep their place and order
    expect(rows.filter((r) => r.kind === "line").map((r) => r.id)).toEqual(["s1", "s2", "s3"]);
  });

  it("adds no divider while the lines stay on the same day", () => {
    const rows = withDayDividers([seg("s1", 0, 8), seg("s2", 0, 14), seg("s3", 0, 23)]);
    expect(dividerIds(rows)).toEqual([]);
  });

  it("keeps day-only keys stable when an older page is inserted above", () => {
    const before = withDayDividers([seg("s2", -1, 9), seg("s3", 0, 9)]);
    const keysBefore = dividerIds(before);
    // a paginated page with the oldest day arrives above the lines already drawn
    const after = withDayDividers([seg("s1", -2, 9), seg("s2", -1, 9), seg("s3", 0, 9)]);
    const keysAfter = dividerIds(after);
    expect(keysBefore.every((k) => keysAfter.includes(k))).toBe(true);
    expect(new Set(keysAfter).size).toBe(keysAfter.length);
    expect(keysAfter).toHaveLength(2);
  });

  it("adds a divider once for a line arriving across midnight", () => {
    const d = new Date();
    d.setHours(23, 50, 0, 0);
    const night = d.toISOString();
    const segAt = (id: string, ts: string): ChatLine => {
      const s = { id, user: { id: id + "u", text: id, images: [], ts, contexts: [] }, start: ts, steps: [], tools: 0, thoughts: 0, replies: 0, folds: [] } as Segment;
      return { kind: "segment", id, seg: s, live: false };
    };
    const now = Date.parse(night) + 30 * 60_000; // 00:20 on the next local day
    const first = withDayDividers([segAt("s1", night)], now);
    expect(dividerIds(first)).toEqual([]);
    const next = new Date(now);
    next.setMinutes(10);
    const both = withDayDividers([segAt("s1", night), segAt("s2", next.toISOString())], now);
    const ids = dividerIds(both);
    expect(ids).toEqual([`day-${next.toLocaleDateString("sv")}`]);
    // re-running on the same lines yields the same keys (no flashing duplicate on re-render)
    expect(dividerIds(withDayDividers([segAt("s1", night), segAt("s2", next.toISOString())], now))).toEqual(ids);
  });

  it("never puts a divider back in time or twice for the same day when the lines are out of order", () => {
    // three days ago, yesterday, a line from two days ago (a clock that was behind), then today
    const rows = withDayDividers([seg("s0", -3, 9), seg("s1", -1, 9), seg("s2", -2, 10), seg("s3", 0, 9)]);
    const ids = dividerIds(rows);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids).toEqual([`day-${new Date(Date.parse(iso(-1, 9))).toLocaleDateString("sv")}`, `day-${new Date(Date.parse(iso(0, 9))).toLocaleDateString("sv")}`]);
  });

  it("opens with a divider for its first line's day only when asked (an earlier stretch's boundary)", () => {
    const rows = withDayDividers([seg("s1", -2, 9), seg("s2", -1, 9)], Date.now(), { includeLeadingDay: true });
    expect(dividerIds(rows)).toEqual([`day-${new Date(Date.parse(iso(-2, 9))).toLocaleDateString("sv")}`, `day-${new Date(Date.parse(iso(-1, 9))).toLocaleDateString("sv")}`]);
  });

  it("draws no divider from a line dated after now (a clock ahead)", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 10, 15, 12, 0, 0));
    try {
      const rows = withDayDividers([seg("s1", 2, 9)]);
      expect(rows).toHaveLength(1);
      expect(rows[0].kind).toBe("line");
      expect(chatLineTime((rows[0] as { line: ChatLine }).line)).toBeTypeOf("number");
    } finally {
      vi.useRealTimers();
    }
  });
});
