// highlight.js 11's common grammars through lowlight, loaded the first time something colors code and then shared by
// all of it: the chat's code blocks (ui/stream/highlight.ts) and the code and diff views (code-view.ts). One instance
// sets the grammars up and compiles each language once; two cost about 16 ms and 1 MB more once both are used.
import type { createLowlight } from "lowlight";

export type CommonLowlight = ReturnType<typeof createLowlight>;

let shared: Promise<CommonLowlight> | null = null;

/** The shared lowlight, loaded once; a failed chunk (the hub was redeployed under the page) is loaded again next time. */
export function commonLowlight(): Promise<CommonLowlight> {
  // Destructured so Rollup drops the entry's `all` re-export (every grammar, ~780 kB) and keeps the common-grammar chunk.
  shared ??= import("lowlight").then(({ createLowlight, common }) => createLowlight(common)).catch((e: unknown) => {
    shared = null;
    throw e;
  });
  return shared;
}
