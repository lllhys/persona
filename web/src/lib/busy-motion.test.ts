// @vitest-environment node
// Reduced motion keeps the busy signal (lib/busy-motion.ts): the stylesheet exempts `.busy-motion` from the strip, and
// every looping busy animation written in the source carries the class.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";
import { BUSY_MOTION, SPIN_TURNS, spinStyle } from "./busy-motion";

const SRC = join(__dirname, "..");

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((f) => {
    const p = join(dir, f);
    if (statSync(p).isDirectory()) return sources(p);
    return /\.tsx?$/.test(f) && !/\.test\.tsx?$/.test(f) ? [p] : [];
  });
}

// Read once, while the file is collected: two tests each walking and reading the whole source tree inside their 5 s
// timeout hit it under the full parallel suite (#4955).
const SOURCES = sources(SRC).map((file) => ({ file, lines: readFileSync(file, "utf8").split("\n") }));
const CSS = readFileSync(join(SRC, "styles.css"), "utf8").replace(/\r\n/g, "\n");

/** The body of the stylesheet's `@media (prefers-reduced-motion: reduce)` block that strips animations. */
function stripBlock(css: string): string {
  const start = css.indexOf("@media (prefers-reduced-motion: reduce) {\n  *, *::before");
  expect(start).toBeGreaterThan(-1);
  let depth = 0;
  for (let i = css.indexOf("{", start); i < css.length; i++) {
    if (css[i] === "{") depth++;
    if (css[i] === "}" && --depth === 0) return css.slice(start, i + 1);
  }
  throw new Error("unterminated reduced-motion block");
}

describe("busy indicators under reduced motion", () => {
  // a Windows checkout's CRLF read as LF
  const block = stripBlock(CSS);

  it("the strip still takes the other animations to 1ms × 1", () => {
    expect(block).toMatch(/\*, \*::before, \*::after \{[^}]*animation-duration: 1ms !important;[^}]*animation-iteration-count: 1 !important/);
  });

  it("exempts the busy class and runs it at the slow pace, looping", () => {
    expect(BUSY_MOTION).toBe("busy-motion");
    const rule = new RegExp(`\\.${BUSY_MOTION}[^{]*\\{([^}]*)\\}`).exec(block);
    expect(rule).not.toBeNull();
    expect(rule![1]).toContain("animation-duration: calc(2.4s * var(--busy-cycles, 1)) !important");
    expect(rule![1]).toContain("animation-iteration-count: infinite !important");
  });

  it("the breathing dot stays in place: reduced motion has it fade without scaling", () => {
    const m = /@keyframes breathe \{([^}]*\}[^}]*)\}/.exec(block);
    expect(m).not.toBeNull();
    expect(m![1]).toContain("opacity");
    expect(m![1]).not.toContain("transform");
  });

  it("every looping busy animation in the source carries the class", () => {
    // a looping keyframe of the busy kind, written inline or as a Tailwind utility
    const busy = /animation: ["`](?:spin|breathe|pixel-on|shimmer-text)\b|spinStyle\(\d|animate-spin|animate-pulse|animate-\[breathe_/;
    const missing: string[] = [];
    for (const { file, lines } of SOURCES) {
      lines.forEach((line, i) => {
        if (!busy.test(line) || /^\s*(\/\/|\*|\/\*)/.test(line)) return;
        // the mark is on the element: this line or the few above it (a multi-line JSX tag)
        const near = lines.slice(Math.max(0, i - 6), i + 1).join("\n");
        if (!/busy-motion|BUSY_MOTION/.test(near)) missing.push(`${relative(SRC, file)}:${i + 1}`);
      });
    }
    expect(missing).toEqual([]);
  });
});

// A looping CSS animation sends React's root listener an animationiteration event every cycle, and the page a style
// recalc: spinners turn SPIN_TURNS times per cycle, at the same speed.
describe("spinners loop rarely", () => {
  const css = CSS;

  it("the keyframes turn SPIN_TURNS times, and the one-turn spin is gone", () => {
    expect(css).toContain(`@keyframes spin-long{to{transform:rotate(${SPIN_TURNS}turn)}}`);
    expect(css).not.toMatch(/@keyframes spin\b(?!-)/);
    expect(css).toMatch(/@property --busy-cycles \{[^}]*inherits: false;[^}]*initial-value: 1;/);
  });

  it("the working mark turns once per --mark-spin-dur, and 2.4s under reduced motion", () => {
    const arc = /\.status-mark\[data-shape="arc"\] \{([^}]*)\}/.exec(css)![1];
    expect(arc).toContain(`--busy-cycles: ${SPIN_TURNS};`);
    expect(arc).toContain(`animation: spin-long calc(var(--mark-spin-dur) * ${SPIN_TURNS}) linear infinite`);
  });

  it("spinStyle keeps the speed it is given", () => {
    expect(spinStyle(700)).toEqual({ animation: `spin-long ${700 * SPIN_TURNS}ms linear infinite`, "--busy-cycles": SPIN_TURNS });
  });

  it("nothing in the source loops the one-turn keyframes", () => {
    const short = /\bspin [\d.]+m?s\b|animation: spin\b(?!-)|animate-spin\b/;
    const found: string[] = [];
    for (const { file, lines } of [...SOURCES, { file: join(SRC, "styles.css"), lines: CSS.split("\n") }]) {
      lines.forEach((line, i) => {
        if (short.test(line) && !/^\s*(\/\/|\*|\/\*)/.test(line)) found.push(`${relative(SRC, file)}:${i + 1}`);
      });
    }
    expect(found).toEqual([]);
  });
});
