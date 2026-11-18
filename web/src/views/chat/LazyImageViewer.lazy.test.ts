/// <reference types="node" />
// The chat's thumbnails are on the first screen and the extension view is in the idle tier; the image viewer they open
// is in neither: its code and words load on the first open (LazyImageViewer.tsx).
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import startup from "../../i18n/startup-namespaces.json";

const read = (file: string) => readFileSync(new URL(file, import.meta.url), "utf8");
const staticViewer = /^import\s+(?!type\b)[^;]*from\s+["'](\.\/|\.\.\/chat\/)ImageViewer["']/m;

describe("the image viewer boundary", () => {
  it("keeps the viewer out of the thumbnails' static imports and its namespace out of startup", () => {
    for (const file of ["./TurnImages.tsx", "./AttachedPhotos.tsx", "./Steps.tsx"]) {
      const source = read(file);
      expect(source, file).not.toMatch(staticViewer);
      expect(source, file).toMatch(/from\s+["']\.\/LazyImageViewer["']/);
    }
    const lazy = read("./LazyImageViewer.tsx");
    expect(lazy).toMatch(/lazyOverlay\(\(\) => import\("\.\/ImageViewer"\)\.then\(\(m\) => m\.ImageViewer\)\)/);
    expect(lazy).toMatch(/lazyOverlay\(\(\) => import\("\.\/ImageViewer"\)\.then\(\(m\) => m\.ChatImageViewer\)\)/);
    expect(startup).not.toContain("chat-imageviewer");
    expect(startup).toContain("chat-attachedphotos");
  });

  it("keeps the viewer out of the idle extension view", () => {
    expect(read("../extensions/ViewBody6.tsx")).not.toMatch(staticViewer);
    expect(read("../extensions/ViewBody6.tsx")).toMatch(/lazyPage\(\(\) => import\("\.\.\/chat\/ImageViewer"\)\.then\(\(m\) => m\.ImageViewer\)\)/);
  });

  it("leaves the image editor's geometry without the viewer's zoom", () => {
    expect(read("../../lib/image-edit.ts")).not.toMatch(/from\s+["']\.\/image-zoom["']/);
    expect(read("../../lib/image-fit.ts")).not.toMatch(/^import\s+(?!type\b)/m);
  });

  it("resolves the dynamic imports to the existing components", async () => {
    const m = await import("./ImageViewer");
    expect(typeof m.ImageViewer).toBe("function");
    expect(typeof m.ChatImageViewer).toBe("function");
  });
});
