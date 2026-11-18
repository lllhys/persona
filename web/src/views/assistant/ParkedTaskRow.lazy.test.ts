/// <reference types="node" />
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "../../i18n/startup-namespaces.json";
import { startupMatcher } from "../../i18n/startup-keys";

describe("wait information stays off the first screen", () => {
  it("loads wait ordering and new detail copy only with the waiting list", () => {
    const source = readFileSync(new URL("./Tasks.tsx", import.meta.url), "utf8");
    expect(source).not.toMatch(/^import[^;]*from\s+["']\.\/ParkedTaskRow["']/m);
    expect(source).toContain('lazyPage(() => import("./ParkedTaskRow").then((m) => m.ParkedTaskRows))');
    for (const name of ["assistant-entries", "todo-triage"]) {
      const shared = readFileSync(new URL(`../../lib/${name}.ts`, import.meta.url), "utf8");
      expect(shared).not.toContain('from "./task-wait"');
    }
    const inStartup = startupMatcher(startup);
    expect(inStartup("task-wait.last_contact")).toBe(false);
    expect(inStartup("assistant-tasks.progress_count")).toBe(true);
  });
});
