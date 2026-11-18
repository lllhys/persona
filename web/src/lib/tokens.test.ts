import { describe, expect, it } from "vitest";
import { compactTokens, formatTokens, hitRate, hitRuns, nearestStep, offlineRuns, totalTokens, type MachineUsage } from "./tokens";
import { perMinute, stackLayers, stepMinutes } from "./usage-rate";

describe("formatTokens", () => {
  // The same table as the apps' TokenUsageTests (#4973).
  it.each([
    [0, "0"], [7, "7"], [999, "999"],
    [1_000, "1K"], [1_250, "1.25K"], [1_999, "2K"], [12_340, "12.3K"], [31_000, "31K"], [999_499, "999K"],
    [999_500, "1M"], [1_000_000, "1M"], [2_960_000, "2.96M"], [178_000_000, "178M"], [999_999_999, "1B"],
    [1_170_000_000, "1.17B"], [25_000_000_000, "25B"], [-1_250, "-1.25K"],
  ])("%d → %s", (n, want) => {
    expect(formatTokens(n)).toBe(want);
  });
});

const usage = (points: MachineUsage["points"], step = 60): MachineUsage => ({
  machine_id: "m", online: true, range: "1h", step, points, total: {}, models: [], launchers: [], launchers_known: true,
});

describe("usage charts", () => {
  it("sums and the hit rate", () => {
    const u = { in: 10, cache_write: 20, cache_read: 270, out: 5, req: 3 };
    expect(totalTokens(u)).toBe(305);
    expect(hitRate(u)).toBe(0.9);
    expect(hitRate({ out: 5 })).toBeUndefined();
    expect(perMinute(600, { step: 600 }, 0)).toBe(60);
  });

  // The step under way covers only what had passed when the report came (#9205): 3 minutes into a 10-minute step its
  // sums are 3 minutes' worth, so its rate is the same as a whole step's at the same pace, and as the 1h view's minutes.
  it("divides the step under way by the part that has passed", () => {
    const tenMinutes = { step: 600, at: 6_000 + 180 };
    expect(stepMinutes(tenMinutes, 6_000)).toBe(3); // 3 minutes in
    expect(perMinute(3_000, tenMinutes, 6_000)).toBe(1_000);
    expect(stepMinutes(tenMinutes, 5_400)).toBe(10); // the step before: whole
    expect(stepMinutes({ step: 600, at: 6_000 + 20 }, 6_000)).toBe(1); // at least a minute
    expect(stepMinutes({ step: 600, at: 5_900 }, 6_000)).toBe(1); // this device's clock behind the hub's
    expect(stepMinutes({ step: 3_600, at: 7_200 + 1_800 }, 7_200)).toBe(30); // half an hour of an hourly step
    expect(stepMinutes({ step: 60, at: 6_030 }, 6_000)).toBe(1); // the 1h view's minutes, as before
    expect(stepMinutes({ step: 600 }, 6_000)).toBe(10); // no arrival time: whole steps
    const u = usage([{ t: 5_400, out: 10_000 }, { t: 6_000, out: 3_000 }], 600);
    u.at = 6_000 + 180;
    expect(stackLayers(u, ["out"])[0].points.map((p) => p.y1)).toEqual([1_000, 1_000]);
  });

  it("stacks the kinds from the bottom, per minute", () => {
    const u = usage([{ t: 0, in: 600, out: 1200, cache_write: 0, cache_read: 6000 }], 600);
    const layers = stackLayers(u, ["in", "out", "cache_read"]);
    expect(layers.map((l) => [l.kind, l.points[0].y0, l.points[0].y1])).toEqual([["in", 0, 60], ["out", 60, 180], ["cache_read", 180, 780]]);
  });

  it("finds offline runs and breaks the hit rate where there are too few replies", () => {
    const u = usage([
      { t: 0, in: 10, cache_read: 90, req: 5 },
      { t: 60, in: 10, cache_read: 90, req: 1 },
      { t: 120, gap: true },
      { t: 180, gap: true },
      { t: 240, in: 10, cache_read: 90, req: 3 },
    ]);
    expect(offlineRuns(u)).toEqual([[120, 240]]);
    expect(hitRuns(u).map((r) => r.map((p) => p.t))).toEqual([[0], [240]]);
    expect(nearestStep(u, 130)).toBeUndefined(); // over the offline time
    expect(nearestStep(u, 250)?.t).toBe(240);
  });
});

describe("compactTokens (#5030, a subagent card's tokens)", () => {
  it.each([
    [0, "0"], [999, "999"], [1_000, "1.0k"], [23_456, "23.5k"], [999_949, "999.9k"], [999_960, "1.0M"], [2_500_000, "2.5M"], [3_210_000_000, "3.2B"],
  ])("%d → %s", (n, want) => {
    expect(compactTokens(n)).toBe(want);
  });
});
