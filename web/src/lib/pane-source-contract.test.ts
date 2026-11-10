import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import { sessionSource } from "./agent-groups";
import type { Session } from "./types";

// The runtime HTTP acceptance test expands this same corpus and compares every
// page/count against its labels. Check those labels against the current client,
// including desktop precedence and the unmarked (unknown) rows.
const cases = JSON.parse(readFileSync(new URL("../../../server/internal/sessionorigin/testdata/pane_sources.json", import.meta.url), "utf8")) as {
  name: string; want: string; session: Partial<Session>;
}[];

it("agrees with the pane-source contract across 8,000 mixed rows", () => {
  // As in the hub's expansion, the last 800 rows are Feishu: the corpus ends with its two Feishu cases.
  expect(cases.slice(-2).map((c) => c.want)).toEqual(["feishu", "feishu"]);
  const counts: Record<string, number> = {};
  for (let n = 0; n < 8000; n++) {
    const c = n < 7200 ? cases[n % (cases.length - 2)] : cases[cases.length - 2 + n % 2];
    const row = { machine_id: `machine-${n % 4}`, session_id: `session-${Math.floor(n / 4)}`, status: "idle", ...c.session } as Session;
    expect(sessionSource(row) ?? "unknown", `${n}: ${c.name}`).toBe(c.want);
    counts[c.want] = (counts[c.want] ?? 0) + 1;
  }
  // Every origin is exercised; the split follows the corpus, which gains a case with each new host (Tern, #2154).
  expect(Object.keys(counts).sort()).toEqual(["desktop", "feishu", "terminal", "unknown"]);
  expect(counts.feishu).toBe(800);
});
