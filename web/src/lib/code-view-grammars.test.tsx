// @vitest-environment jsdom
// The code views load highlight.js's common grammars only (#5322): `import("lowlight")` used as `m.createLowlight(m.common)`
// kept the entry's `all` re-export, a 783 kB chunk of every grammar, in the bundle; destructuring lets Rollup drop it.
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import { highlightLines, useHighlighter } from "./code-view";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function Probe() {
  useHighlighter(true);
  return null;
}

describe("code view highlighter", () => {
  it("highlights with the common grammars once loaded, and has no others", async () => {
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<Probe />));
    await vi.waitFor(() => expect(highlightLines("x", "typescript")).not.toBeNull(), { timeout: 10_000 });
    expect(highlightLines("const a = 1", "typescript")?.[0].some((t) => t.cls)).toBe(true);
    expect(highlightLines("+++++[>+<-]", "brainfuck")).toBeNull(); // in `all`, not in `common`
    act(() => root.unmount());
  });

  it("imports lowlight destructured, so the build leaves out every-grammar `all`", () => {
    const src = readFileSync(join(__dirname, "lowlight.ts"), "utf8"); // the loader code-view.ts shares with the chat
    expect(src).toMatch(/import\("lowlight"\)\.then\(\(\{ createLowlight, common \}\)/);
  });
});
