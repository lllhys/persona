/// <reference types="node" />
// Archived agents is a secondary route: its implementation and exclusive catalog must not ride on every first screen.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "./i18n/startup-namespaces.json";

const source = readFileSync(new URL("./App.tsx", import.meta.url), "utf8");

describe("the archived-agents route", () => {
  it("loads only on intent or at its route, with its strings outside the startup catalog", () => {
    expect(source).not.toMatch(/^import[^;]*from\s+["']\.\/views\/agents\/AgentArchives["']/m);
    expect(source).toMatch(/lazyPage\(\(\) => import\("\.\/views\/agents\/AgentArchives"\)\.then\(\(m\) => m\.AgentArchives\), \{ routes: \["agent-archives"\] \}\)/);
    expect(startup).not.toContain("agents-archives");
    expect(startup).toContain("app");
  });

  it("resolves the dynamic import to the existing component", async () => {
    const m = await import("./views/agents/AgentArchives");
    expect(typeof m.AgentArchives).toBe("function");
  }, 60_000);
});
