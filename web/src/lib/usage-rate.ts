// A machine's usage chart as rates (#4973, #9205): each step's sums per minute, the step under way over the part of it
// that has passed. Only the machine page draws these, so they live apart from lib/tokens.ts, which the first screen loads.
import type { MachineUsage, UsageKind } from "./tokens";

/** The minutes the sums of the step starting at t cover: the whole step, except the step under way when the report came
 *  (`at`), whose sums cover only the part that had passed (at least a minute). Divided by the whole step, a 10-minute or
 *  hourly step read as a fraction of its rate until it ended (#9205). */
export function stepMinutes(u: Pick<MachineUsage, "step" | "at">, t: number): number {
  const ran = u.at === undefined ? u.step : u.at - t;
  return Math.max(60, Math.min(u.step, ran)) / 60;
}

/** The sum n of the step starting at t, per minute. */
export function perMinute(n: number, u: Pick<MachineUsage, "step" | "at">, t: number): number {
  return Math.round(n / stepMinutes(u, t));
}

/** The stacked chart's layers: for each kind shown, every step's bottom and top (per minute), bottom up. */
export function stackLayers(u: MachineUsage, kinds: UsageKind[]): { kind: UsageKind; points: { t: number; y0: number; y1: number }[] }[] {
  const base = u.points.map(() => 0);
  return kinds.map((kind) => ({
    kind,
    points: u.points.map((p, i) => {
      const y0 = base[i];
      base[i] += perMinute(p[kind] ?? 0, u, p.t);
      return { t: p.t, y0, y1: base[i] };
    }),
  }));
}
