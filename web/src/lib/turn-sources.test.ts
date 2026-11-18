import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { turnSources } from "./turn-sources";
import type { Step } from "./turns";
import type { FoldSource, Item } from "./types";

// The cases the node reads too (server/internal/transcript/fold_sources_test.go): the calls on the page name the same
// pages as their folded run, so the Sources row says the same before and after a reload.
interface Case { name: string; input: { items: Item[] }; expected: { sources: FoldSource[] } }
const doc = JSON.parse(readFileSync(new URL("../../../spec/fixtures/fold-sources/cases.json", import.meta.url), "utf8")) as { cases: Case[] };

/** The items as an exchange's tool steps: each call with its result, when the run has it. */
function steps(items: Item[]): Step[] {
  const results = new Map(items.filter((i) => i.kind === "tool_result").map((i) => [i.tool_use_id, i]));
  return items.filter((i) => i.kind === "tool_use").map((use) => ({ kind: "tool", id: use.id, use, result: results.get(use.tool_use_id), images: [] }));
}

describe("turnSources", () => {
  it.each(doc.cases.map((c) => [c.name, c] as const))("%s", (_, c) => {
    const got = turnSources(steps(c.input.items));
    expect(got ?? []).toEqual(c.expected.sources);
    if (!c.expected.sources.length) expect(got).toBeNull();
  });

  it("reads a folded run by its fold's sources, before the calls on the page", () => {
    const fold = { kind: "fold", id: "f", item: { id: "fold:1", kind: "fold", fold: { items: 4, tools: 2, thoughts: 0, through_seq: 2, sources: [{ url: "https://folded.example.com/", title: "Folded" }] } } } as Step;
    const live = steps(doc.cases.find((c) => c.name === "claude_links")!.input.items);
    expect(turnSources([fold, ...live])?.map((s) => s.url)).toEqual(["https://folded.example.com/", "https://weather.example.com/kyoto", "https://travel.example.org/kyoto"]);
  });
});
