// @vitest-environment jsdom
// The file behind a modified diff can be larger than one /file preview. Context past that page keeps its real line
// numbers: reading the whole file follows the byte offsets the server returns, rather than treating the prefix as all.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { FileContent } from "../../lib/files";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  (globalThis as { CSS?: unknown }).CSS ??= { escape: (s: string) => s.replace(/["\\]/g, "\\$&") };
});

const fixture = vi.hoisted(() => {
  const pageBytes = 2 * 1024 * 1024;
  const lineBytes = 128;
  const pageLines = pageBytes / lineBytes;
  const firstChange = pageLines + 2;
  const secondChange = pageLines + 6;
  const totalLines = pageLines + 10;
  const sourceLine = (n: number, kind: "ctx" | "old" | "new" = "ctx") => `line-${String(n).padStart(6, "0")}-${kind}`.padEnd(lineBytes - 1, "x") + "\n";
  const lines = Array.from({ length: totalLines }, (_, i) => sourceLine(i + 1, i + 1 === firstChange || i + 1 === secondChange ? "new" : "ctx"));
  return {
    mode: "large" as "large" | "small",
    deferred: false,
    requests: [] as { url: string; resolve: (file: FileContent) => void }[],
    offsets: [] as number[],
    pageBytes,
    pageLines,
    firstChange,
    secondChange,
    totalLines,
    lines,
    pages: [lines.slice(0, pageLines).join(""), lines.slice(pageLines).join("")],
    sourceLine,
  };
});

vi.mock("../../lib/session-review", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/session-review")>();
  return {
    ...real,
    loadReview: () => Promise.resolve({
      root: "/w",
      scope: "uncommitted",
      base: { hash: "a1b2c3d4", short: "a1b2c3d" },
      files: [{
        path: "src/a.txt",
        status: "modified",
        added: fixture.mode === "large" ? 2 : 1,
        deleted: fixture.mode === "large" ? 2 : 1,
        hunks: fixture.mode === "large"
          ? [fixture.firstChange, fixture.secondChange].map((n) => ({ old_start: n, new_start: n, lines: [`-${fixture.sourceLine(n, "old").trimEnd()}`, `+${fixture.sourceLine(n, "new").trimEnd()}`] }))
          : [{ old_start: 2, new_start: 2, lines: ["-two old", "+two new"] }],
      }],
    }),
  };
});

vi.mock("../../lib/api", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/api")>();
  return {
    ...real,
    api: async (url: string) => {
      const parsed = new URL(url, "http://x");
      const offset = Number(parsed.searchParams.get("offset") ?? 0);
      fixture.offsets.push(offset);
      if (fixture.deferred) return new Promise<FileContent>((resolve) => fixture.requests.push({ url, resolve }));
      const text = fixture.mode === "small" ? "one\ntwo new\nthree\nfour\n" : offset === 0 ? fixture.pages[0] : offset === fixture.pageBytes ? fixture.pages[1] : null;
      if (text === null) throw new Error(`unexpected offset ${offset}`);
      return {
        path: "/w/src/a.txt",
        name: "a.txt",
        size: fixture.mode === "large" ? fixture.totalLines * 128 : text.length,
        mime: "text/plain",
        text,
        truncated: fixture.mode === "large" && offset === 0,
        ...(fixture.mode === "large" && offset === 0 ? { next: fixture.pageBytes } : { next: 0 }),
        ...(parsed.searchParams.get("stream") === "1" ? { revision: "r1" } : {}),
        is_dir: false,
        offset,
      };
    },
  };
});
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: () => {} }));

import { ReviewPanel } from "./ReviewPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
beforeEach(() => {
  fixture.mode = "large";
  fixture.deferred = false;
  fixture.requests = [];
  fixture.offsets = [];
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const draw = () => act(async () => {
  root.render(<ReviewPanel mid="m" sid="s" cwd="/w" onClose={() => {}} />);
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});
const expand = (button: HTMLButtonElement) => act(async () => {
  button.click();
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});
const showAll = () => [...host.querySelectorAll<HTMLButtonElement>('button[aria-label="展开全部"]')];
const visibleNewLines = () => [...host.querySelectorAll<HTMLElement>('[data-ln="new"]')].map((el) => Number(el.dataset.n));
const answer = (index: number, revision = "r1", text?: string) => act(async () => {
  const request = fixture.requests[index];
  const url = new URL(request.url, "http://x");
  const offset = Number(url.searchParams.get("offset") ?? 0);
  request.resolve({
    path: "/w/src/a.txt",
    name: "a.txt",
    size: fixture.totalLines * 128,
    mime: "text/plain",
    text: text ?? (offset === 0 ? fixture.pages[0] : fixture.pages[1]),
    truncated: offset === 0,
    next: offset === 0 ? fixture.pageBytes : 0,
    ...(url.searchParams.get("stream") === "1" ? { revision } : {}),
    is_dir: false,
    offset,
  });
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});

describe("review context across file preview pages", () => {
  it("reads every byte offset before rendering later gaps and the trailing context", async () => {
    expect(fixture.pages[0]).toHaveLength(fixture.pageBytes); // ASCII fixture: string length is also its byte length
    expect(fixture.pages[1].length).toBeGreaterThan(0);
    await draw();
    const gaps = showAll();
    expect(gaps).toHaveLength(3);

    await expand(gaps[2]);
    await expand(gaps[1]);

    expect({
      offsets: fixture.offsets,
      visible: visibleNewLines(),
      between: host.textContent?.includes(fixture.lines[fixture.firstChange + 1].trimEnd()),
      trailing: host.textContent?.includes(fixture.lines.at(-1)!.trimEnd()),
    }).toEqual({
      offsets: [0, fixture.pageBytes],
      visible: Array.from({ length: fixture.totalLines - fixture.firstChange + 1 }, (_, i) => fixture.firstChange + i),
      between: true,
      trailing: true,
    });
  });

  it("keeps a complete small file to one read and caches it for another gap", async () => {
    fixture.mode = "small";
    await draw();
    const gaps = showAll();
    expect(gaps).toHaveLength(2);

    await expand(gaps[1]);
    await expand(gaps[0]);

    expect(fixture.offsets).toEqual([0]);
    expect(visibleNewLines()).toEqual([1, 2, 3, 4]);
    expect(host.textContent).toContain("one");
    expect(host.textContent).toContain("four");
  });

  it("requests revisions and rejects a delayed later page from another revision", async () => {
    fixture.deferred = true;
    await draw();
    await expand(showAll()[2]);
    expect(fixture.requests).toHaveLength(1);

    await answer(0, "old");
    expect(fixture.requests).toHaveLength(2);
    await answer(1, "new");

    expect({
      streamed: fixture.requests.every((r) => new URL(r.url, "http://x").searchParams.get("stream") === "1"),
      mixedTailShown: host.textContent?.includes(fixture.lines.at(-1)!.trimEnd()),
    }).toEqual({ streamed: true, mixedTailShown: false });
  });

  it("does not let a final page from before reload populate the new review", async () => {
    fixture.deferred = true;
    await draw();
    await expand(showAll()[2]);
    await answer(0, "old");
    expect(fixture.requests).toHaveLength(2);

    await act(async () => {
      host.querySelector<HTMLButtonElement>('button[aria-label="刷新"]')!.click();
      for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
    });
    const staleTail = fixture.pages[1].replace(fixture.lines.at(-1)!, "stale-old-review\n");
    await answer(1, "old", staleTail);

    await expand(showAll()[2]);
    expect(fixture.requests).toHaveLength(3);
    await answer(2, "fresh");
    await answer(3, "fresh");

    expect(fixture.offsets).toEqual([0, fixture.pageBytes, 0, fixture.pageBytes]);
    expect(host.textContent).toContain(fixture.lines.at(-1)!.trimEnd());
    expect(host.textContent).not.toContain("stale-old-review");
  });

  it("shares one whole-file read between gaps opened concurrently", async () => {
    fixture.deferred = true;
    await draw();
    const gaps = showAll();
    await act(async () => {
      gaps[2].click();
      gaps[1].click();
      for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
    });

    expect(fixture.offsets).toEqual([0]);
    await answer(0);
    expect(fixture.offsets).toEqual([0, fixture.pageBytes]);
    await answer(1);

    expect(fixture.requests).toHaveLength(2);
    expect(host.textContent).toContain(fixture.lines[fixture.firstChange + 1].trimEnd());
    expect(host.textContent).toContain(fixture.lines.at(-1)!.trimEnd());
  });
});
