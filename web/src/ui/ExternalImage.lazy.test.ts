/// <reference types="node" />
// Only an off-hub image needs the consent gate's code and words; same-origin attachments stay synchronous.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "../i18n/startup-namespaces.json";

const source = readFileSync(new URL("./ExternalImage.tsx", import.meta.url), "utf8");

describe("the external-image gate boundary", () => {
  it("loads the gate dynamically and keeps its namespace out of startup", () => {
    expect(source).not.toMatch(/^import[^;]*from\s+["']\.\/ExternalImageGate["']/m);
    expect(source).toMatch(/import\("\.\/ExternalImageGate"\)/);
    expect(source).toContain("isOwnImageURL");
    expect(startup).not.toContain("ui-externalimage");
  });

  it("resolves the gate component", async () => {
    const m = await import("./ExternalImageGate");
    expect(typeof m.ExternalImageGate).toBe("function");
  });
});
