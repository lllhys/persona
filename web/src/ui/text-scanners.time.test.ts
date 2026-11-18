// A time guard for the scanners every chat message's text goes through on the main thread (#9795). They run on each
// render and, while a reply streams, on every throttled step, so one that backtracks exponentially freezes the tab for
// good instead of failing anything: #8894's path rule took seconds on a branch name like fix/1234-attendance-followups,
// minutes on a longer extensionless name, and a streaming reply scanned it again at every step.
//
// Each hostile shape grows by at most 15% a step (2 characters while that is less) and stops at the first call over
// budget, so even a pattern whose time doubles with every character fails within a few times the budget instead of
// hanging the run. Linear and quadratic scanners stay far under it at these sizes.
import { describe, expect, it } from "vitest";
import { codePathRef, findPathRefs } from "../lib/path-refs";
import { parseUserText } from "../lib/turns";
import { hasMath, linkify, normalizeMath, splitCode } from "./Markdown";
import { writtenText } from "./PathLinks";
import { replyBreaks } from "./reply-breaks";
import { splitChunks } from "./stream/chunks";
import { hasLanguageFence } from "./stream/highlight";
import { repairStreamingMarkdown } from "./stream/repair";

const SCANNERS: [string, (text: string) => unknown][] = [
  ["findPathRefs", findPathRefs],
  ["codePathRef", codePathRef],
  ["normalizeMath", normalizeMath],
  ["linkify", (text) => linkify(text, { math: true })],
  ["splitCode", splitCode],
  ["hasMath", hasMath],
  ["repairStreamingMarkdown", repairStreamingMarkdown],
  ["replyBreaks", replyBreaks],
  ["splitChunks", splitChunks],
  ["hasLanguageFence", hasLanguageFence],
  ["parseUserText", parseUserText],
  ["writtenText", writtenText],
];

/** `unit` repeated to n characters. */
const cut = (unit: string, n: number) => unit.repeat(Math.ceil(n / unit.length)).slice(0, n);

// What text can hold that a pattern may stumble on: names without an extension (#8894), names in other scripts, deep
// quotes, and markup that opens and never closes.
const SHAPES: [string, (n: number) => string][] = [
  ["a relative name without an extension", (n) => `see src/${cut("a", n)} now`],
  ["a branch name", (n) => `pushed fix/1234-${cut("attendance-followups-", n)} today`],
  ["a home path", (n) => `~/${cut("a", n)}/${cut("b", n)}`],
  ["an absolute path before a #", (n) => `/x/${cut("a", n)}#`],
  ["a name of digits and dots", (n) => `v1/${cut("1.", n)}`],
  ["a non-ASCII name", (n) => `~/${cut("报", n)}/表`],
  ["Chinese right after an extension", (n) => `/tmp/a.md${cut("报", n)}`],
  ["mixed scripts", (n) => `docs/${cut("报a", n)}`],
  ["a run of @", (n) => cut("a@", n)],
  ["a name in backticks", (n) => `\`src/${cut("a", n)}\``],
  ["unmatched backtick runs", (n) => cut("`a``b```c", n)],
  ["unclosed \\[", (n) => cut("\\[ x ", n)],
  ["unclosed \\(", (n) => cut("\\( x ", n)],
  ["lone dollars", (n) => cut("$a ", n)],
  ["math in a table", (n) => cut("| a | $b|c$ |\n", n)],
  ["a URL with open parentheses", (n) => `https://example.com/${cut("(", n)}`],
  ["a URL of dots", (n) => `https://example.com/${cut("a.", n)}`],
  ["unclosed fences", (n) => cut("```\n", n)],
  ["numbered lines", (n) => cut("1. a\n", n)],
  ["a deeply quoted line", (n) => `${cut("> ", n)}quoted`],
  ["context tags", (n) => cut('<persona-context kind="x">', n)],
  ["paragraphs", (n) => cut("a\n\n", n)],
];

const SIZES: number[] = [];
for (let n = 4; n <= 4096; n = Math.max(n + 2, Math.ceil(n * 1.15))) SIZES.push(n);
/** Far over what any of them takes on these inputs (a few milliseconds), far under what a runaway pattern takes. */
const BUDGET_MS = 250;

/** How long one call takes; a call just over budget is timed again and the quicker counts, so a pause of the test
 *  process alone is not a failure (a runaway one is not run twice). */
function time(scan: (text: string) => unknown, text: string): number {
  const once = () => {
    const started = performance.now();
    scan(text);
    return performance.now() - started;
  };
  const first = once();
  return first > BUDGET_MS && first < 4 * BUDGET_MS ? Math.min(first, once()) : first;
}

describe("the chat's text scanners", () => {
  it("stay within budget as hostile text grows", () => {
    const slow: string[] = [];
    for (const [name, scan] of SCANNERS) {
      for (const [shape, make] of SHAPES) {
        for (const n of SIZES) {
          const text = make(n);
          const ms = time(scan, text);
          if (ms > BUDGET_MS) {
            slow.push(`${name} on ${shape} (${text.length} characters): ${Math.round(ms)} ms`);
            break; // the next size would only take longer
          }
        }
      }
    }
    expect(slow).toEqual([]);
  }, 120_000);

  it("read names shaped like the ones that froze a deployed page at once (#9795)", () => {
    const names = ["fix/1234-attendance-followups", "example/payments-service-internal", "provider-api/large-model-thinking-pro"];
    const slow: string[] = [];
    for (const name of names) {
      for (const text of [`分支 ${name} 已推送`, `pushed \`${name}\`, see the PR`]) {
        for (const [scanner, scan] of SCANNERS) {
          const ms = time(scan, text);
          if (ms > 50) slow.push(`${scanner} on ${JSON.stringify(text)}: ${Math.round(ms)} ms`);
        }
      }
    }
    expect(slow).toEqual([]);
  }, 120_000);
});
