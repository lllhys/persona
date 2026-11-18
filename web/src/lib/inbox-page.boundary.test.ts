/// <reference types="node" />
// lib/inbox.ts is on every first screen (the sidebar's badge, the chat's asks); lib/inbox-page.ts and its words are the
// lazily loaded Inbox page's only, so they stay off it.
import { readdirSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import startup from "../i18n/startup-namespaces.json";

const src = fileURLToPath(new URL("..", import.meta.url));
const sources = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? sources(join(dir, e.name)) : /\.tsx?$/.test(e.name) && !/\.test\.tsx?$/.test(e.name) ? [join(dir, e.name)] : []);
const pageImport = /^import\s+(?!type\b)[^;]*from\s+["'][./]*(lib\/)?inbox-page["']/m;

describe("the Inbox page's helpers", () => {
  it("are imported only by the Inbox page's own views", () => {
    const importers = sources(src).filter((f) => pageImport.test(readFileSync(f, "utf8"))).map((f) => relative(src, f));
    expect(importers.length).toBeGreaterThan(0);
    expect(importers.filter((f) => !f.startsWith("views/inbox/"))).toEqual([]);
  });

  it("keep their words out of the startup namespaces", () => {
    expect(startup).toContain("lib-inbox");
    expect(startup).not.toContain("lib-inbox-page");
  });
});
