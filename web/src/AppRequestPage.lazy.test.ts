/// <reference types="node" />
// A script request is a secondary route, not a reason to download its page and body strings on every first screen.
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "./i18n/startup-namespaces.json";

const source = readFileSync(new URL("./App.tsx", import.meta.url), "utf8");

describe("the script-request route", () => {
  it("loads on intent or at its route and keeps request-page in client-rest", () => {
    expect(source).not.toMatch(/^import[^;]*from\s+["']\.\/views\/RequestPage["']/m);
    expect(source).toMatch(/lazyPage\(\(\) => import\("\.\/views\/RequestPage"\)\.then\(\(m\) => m\.RequestPage\), \{ routes: \["request"\] \}\)/);
    expect(startup).not.toContain("request-page");
    expect(startup).toContain("app");
  });

  it("resolves the dynamic import to the existing component", async () => {
    const m = await import("./views/RequestPage");
    expect(typeof m.RequestPage).toBe("function");
  });
});
