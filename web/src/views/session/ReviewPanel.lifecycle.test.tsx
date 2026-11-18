// @vitest-environment jsdom
// Review requests have an explicit lifetime. A response or rejection from a retired review must not clear the current
// review, its Load diff busy state, or show an error for work the reader no longer owns.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { GitReview, GitReviewFile } from "../../lib/session-review";
import type { UIAction } from "../../lib/extension-ui";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  (globalThis as { CSS?: unknown }).CSS ??= { escape: (s: string) => s.replace(/["\\]/g, "\\$&") };
});

const head = "1111111111111111111111111111111111111111";
const mergeBase = "2222222222222222222222222222222222222222";
const oldRevision = "3333333333333333333333333333333333333333";
const branchRevision = "4444444444444444444444444444444444444444";
const path = "src/a.txt";
const bigFile = (revision: string): GitReviewFile => ({ path, status: "modified", added: 1, deleted: 0, big: true, revision });
const summary = (scope: GitReview["scope"], hash: string, revision: string): GitReview => ({
  root: "/w",
  scope,
  base: { hash, short: hash.slice(0, 7), ...(scope === "branch" ? { ref: "main" } : {}) },
  head: { hash: head, short: head.slice(0, 7), branch: scope === "branch" ? "fresh-branch" : "old-branch" },
  files: [bigFile(revision)],
});
const fileDiff = (revision: string, text: string): GitReviewFile => ({
  ...bigFile(revision),
  big: false,
  hunks: [{ old_start: 1, new_start: 1, lines: [`+${text}`] }],
});

const requests = vi.hoisted(() => ({
  files: [] as { base: string; path: string; resolve: (file: GitReviewFile | null) => void; reject: (error: Error) => void }[],
  toasts: [] as { text: string; tone: string }[],
  act: null as null | ((action: UIAction, values: Record<string, string>) => void),
}));
vi.mock("../../lib/session-review", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/session-review")>();
  return {
    ...real,
    // Keep the real loadReview and api in this suite. Only the per-file endpoint is controlled at the component boundary.
    loadReviewFile: (_mid: string, _cwd: string, owner: GitReview, file: GitReviewFile) => new Promise<GitReviewFile | null>((resolve, reject) => {
      requests.files.push({ base: owner.base.hash!, path: file.path, resolve, reject });
    }),
  };
});
vi.mock("../../ui/atoms", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../ui/atoms")>();
  return {
    ...real,
    toast: (text: string, tone: string) => requests.toasts.push({ text, tone }),
  };
});
vi.mock("../extensions/DiffReview", async (importOriginal) => {
  const real = await importOriginal<typeof import("../extensions/DiffReview")>();
  return {
    ...real,
    DiffView: (props: Parameters<typeof real.DiffView>[0]) => {
      requests.act = props.act;
      return <real.DiffView {...props} />;
    },
  };
});
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: () => {} }));

import { t } from "../../i18n";
import { ReviewPanel } from "./ReviewPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;
let mounted = false;
let initialSummary: "ok" | "delayed-failure" | "current-failure";
let delayedBody: ReadableStreamDefaultController<Uint8Array> | null;
let summaries: { scope: string; signal?: AbortSignal | null }[];
const encoder = new TextEncoder();
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

beforeEach(() => {
  requests.files = [];
  requests.toasts = [];
  requests.act = null;
  initialSummary = "ok";
  delayedBody = null;
  summaries = [];
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  mounted = false;
  vi.stubGlobal("fetch", vi.fn((input: string, init?: RequestInit) => {
    const url = new URL(input, "http://review.test");
    if (!url.pathname.endsWith("/git/review")) throw new Error(`unexpected request: ${input}`);
    const scope = url.searchParams.get("scope")!;
    summaries.push({ scope, signal: init?.signal });
    if (scope === "uncommitted" && initialSummary === "delayed-failure") {
      initialSummary = "ok";
      return Promise.resolve(new Response(new ReadableStream<Uint8Array>({
        start(controller) {
          delayedBody = controller;
        },
      }), { status: 500, headers: { "Content-Type": "application/json", "X-Request-Id": "retired-summary" } }));
    }
    if (scope === "uncommitted" && initialSummary === "current-failure") {
      return Promise.resolve(json({ error: "CURRENT-SUMMARY-FAILURE" }, 500));
    }
    if (scope === "uncommitted") return Promise.resolve(json(summary("uncommitted", head, oldRevision)));
    if (scope === "branch") return Promise.resolve(json(summary("branch", mergeBase, branchRevision)));
    throw new Error(`unexpected scope: ${scope}`);
  }));
});
afterEach(() => {
  if (mounted) act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

const settle = () => act(async () => {
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});
const draw = async () => {
  await act(async () => root.render(<ReviewPanel mid="m" sid="s" cwd="/w" onClose={() => {}} />));
  mounted = true;
  await settle();
};
const button = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
const loadDiff = () => button(t("extensions-codeviews.load_diff"));
const click = async (label: string) => {
  expect(button(label), `missing button ${label}`).toBeTruthy();
  await act(async () => button(label)!.click());
};
const clickLoad = async () => {
  expect(loadDiff(), "missing Load diff").toBeTruthy();
  await act(async () => loadDiff()!.click());
};
const finishDelayedSummary = async (message: string) => act(async () => {
  delayedBody!.enqueue(encoder.encode(JSON.stringify({ error: message })));
  delayedBody!.close();
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});

it("ignores a retired summary HTTP 500 whose body arrives after the Branch review", async () => {
  initialSummary = "delayed-failure";
  await draw();
  expect(summaries).toHaveLength(1);
  expect(delayedBody).toBeTruthy();

  await click(t("review.branch"));
  await settle();
  expect(summaries[0].signal?.aborted).toBe(true);
  expect(host.textContent).toContain("fresh-branch");
  await finishDelayedSummary("RETIRED-SUMMARY-FAILURE");

  expect(host.textContent).toContain("fresh-branch");
  expect(host.textContent).toContain(t("review.against_branch", { ref: "main", sha: mergeBase.slice(0, 7) }));
  expect(host.textContent).not.toContain("RETIRED-SUMMARY-FAILURE");
  expect(loadDiff()).toBeTruthy();
});

it("still shows the current summary HTTP 500", async () => {
  initialSummary = "current-failure";
  await draw();
  expect(host.textContent).toContain("CURRENT-SUMMARY-FAILURE");
  expect(host.textContent).not.toContain("fresh-branch");
});

it("enables a fresh Branch Load diff while the old review's file request is pending", async () => {
  await draw();
  await clickLoad();
  expect(requests.files).toHaveLength(1);
  expect(loadDiff()?.disabled).toBe(true);

  await click(t("review.branch"));
  await settle();

  expect(host.textContent).toContain("fresh-branch");
  expect(loadDiff()).toBeTruthy();
  expect(loadDiff()?.disabled).toBe(false);
});

it("does not let the old file request's finally clear a fresh load's busy state", async () => {
  await draw();
  await clickLoad();
  await click(t("review.branch"));
  await settle();

  // The preceding regression owns the real button's enabled-state assertion. Invoke the same component action directly
  // here so the old implementation also reaches the overlapping-finally race instead of stopping at that first defect.
  await act(async () => requests.act!({ id: "load" }, { path }));
  expect(requests.files).toHaveLength(2);
  expect(requests.files[1]).toMatchObject({ base: mergeBase, path });
  expect(loadDiff()?.disabled).toBe(true);

  await act(async () => requests.files[0].resolve(fileDiff(oldRevision, "OLD-FINALLY-HUNK")));
  await settle();
  expect(loadDiff()?.disabled).toBe(true);
  expect(host.textContent).not.toContain("OLD-FINALLY-HUNK");

  await act(async () => requests.files[1].resolve(fileDiff(branchRevision, "FRESH-AFTER-OLD-FINALLY")));
  await settle();
  expect(host.textContent).toContain("FRESH-AFTER-OLD-FINALLY");
  expect(loadDiff()).toBeUndefined();
});

it("silences a retired file rejection after Branch is accepted", async () => {
  await draw();
  await clickLoad();
  await click(t("review.branch"));
  await settle();

  await act(async () => requests.files[0].reject(new Error("RETIRED-FILE-FAILURE")));
  await settle();
  expect(requests.toasts).toEqual([]);
  expect(host.textContent).toContain("fresh-branch");
});

it("silences a file rejection after unmount", async () => {
  await draw();
  await clickLoad();
  await act(async () => root.unmount());
  mounted = false;

  await act(async () => requests.files[0].reject(new Error("UNMOUNTED-FILE-FAILURE")));
  await settle();
  expect(requests.toasts).toEqual([]);
});

it("still toasts a current file rejection and releases its busy state", async () => {
  await draw();
  await clickLoad();
  await act(async () => requests.files[0].reject(new Error("CURRENT-FILE-FAILURE")));
  await settle();

  expect(requests.toasts).toEqual([{ text: "CURRENT-FILE-FAILURE", tone: "error" }]);
  expect(loadDiff()?.disabled).toBe(false);
});
