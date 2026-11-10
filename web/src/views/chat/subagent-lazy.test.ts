/// <reference types="node" />
// Subagent disclosures only download when a reader manually opens their work summary.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";

const source = readFileSync(new URL("./WorkGroup.tsx", import.meta.url), "utf8");

describe("subagent report loading", () => {
  it("keeps the report card outside the first screen's static imports", () => {
    expect(source).not.toMatch(/^import[^;]*from\s+["']\.\/SubagentCard["']/m);
    expect(source).toMatch(/lazy\(\(\) => import\("\.\/SubagentCard"\)/);
  });

  it("preserves the existing card and its props behind Suspense", () => {
    expect(source).toMatch(/<Suspense fallback=\{null\}><SubagentCard step=\{step\} status=\{p\.agentStatus\(step\)\}/);
  });
});
