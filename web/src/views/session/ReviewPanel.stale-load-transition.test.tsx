// @vitest-environment jsdom
// A big-file request belongs to the review object that started it, even when the old review's Load diff button is
// clicked while its replacement summary is pending. A late file must not become the new review's diff or comment source.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { pendingReview, setPendingReview, type GitReview, type GitReviewFile } from "../../lib/session-review";
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
  files: [bigFile(revision)],
});
const fileResponse = (base: string, revision: string, text: string): GitReview => ({
  root: "/w",
  scope: "commit",
  base: { hash: base, short: base.slice(0, 7) },
  files: [{ ...bigFile(revision), big: false, hunks: [{ old_start: 1, new_start: 1, lines: [`+${text}`] }] }],
});

const loads = vi.hoisted(() => ({
  files: [] as { base: string; paths: string[]; resolve: (review: GitReview) => void }[],
  summaries: [] as { resolve: (review: GitReview) => void }[],
  directFiles: false,
}));
vi.mock("../../lib/api", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/api")>();
  return {
    ...real,
    api: async (route: string) => {
      const url = new URL(route, "http://review.test");
      if (!url.pathname.endsWith("/git/review")) throw new Error(`unexpected request: ${route}`);
      const scope = url.searchParams.get("scope");
      if (url.searchParams.has("path")) {
        expect(scope).toBe("commit");
        return new Promise<GitReview>((resolve) => {
          loads.files.push({ base: url.searchParams.get("base")!, paths: url.searchParams.getAll("path"), resolve });
        });
      }
      if (scope === "uncommitted") return summary("uncommitted", head, oldRevision);
      if (scope === "branch") return new Promise<GitReview>((resolve) => loads.summaries.push({ resolve }));
      throw new Error(`unexpected scope: ${scope}`);
    },
  };
});
vi.mock("../../lib/session-review", async (importOriginal) => {
  const real = await importOriginal<typeof import("../../lib/session-review")>();
  return {
    ...real,
    loadReview: (mid: string, cwd: string, target: import("../../lib/session-review").ReviewTarget, signal?: AbortSignal): Promise<GitReview> => {
      if (!loads.directFiles || target.scope !== "branch") return real.loadReview(mid, cwd, target, signal);
      return new Promise((resolve) => loads.summaries.push({ resolve }));
    },
    loadReviewFile: (mid: string, cwd: string, owner: GitReview, f: GitReviewFile): Promise<GitReviewFile | null> => {
      if (!loads.directFiles) return real.loadReviewFile(mid, cwd, owner, f);
      // Control the promise returned to ReviewPanel, after the real helper's response mapping. This makes the two
      // component continuations, rather than the API's inner continuations, run back-to-back in one task.
      return new Promise((resolve) => {
        loads.files.push({
          base: owner.base.hash!,
          paths: [f.path],
          resolve: (review) => resolve(review.files.find((x) => x.path === f.path) ?? null),
        });
      });
    },
  };
});
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: () => {} }));

import { t } from "../../i18n";
import { ReviewPanel } from "./ReviewPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement;
let root: Root;

beforeEach(async () => {
  loads.files = [];
  loads.summaries = [];
  loads.directFiles = false;
  localStorage.clear();
  setPendingReview("m/s", { comments: [] });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<ReviewPanel mid="m" sid="s" cwd="/w" onClose={() => {}} />));
  await settle();
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const settle = () => act(async () => {
  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
});
const outsideAct = async (run: () => Promise<void>) => {
  const globals = globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean };
  const previous = globals.IS_REACT_ACT_ENVIRONMENT;
  globals.IS_REACT_ACT_ENVIRONMENT = false;
  try {
    await run();
  } finally {
    globals.IS_REACT_ACT_ENVIRONMENT = previous;
  }
};
const button = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
const loadDiff = () => button(t("extensions-codeviews.load_diff"));
const click = async (label: string) => {
  expect(button(label), `missing button ${label}`).toBeTruthy();
  await act(async () => button(label)!.click());
};
const type = async (el: HTMLTextAreaElement, value: string) => {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, value);
    el.dispatchEvent(new Event("input", { bubbles: true }));
  });
};

it("rejects an old review load clicked during the transition after its replacement summary arrives", async () => {
  await click(t("review.branch"));
  expect(loads.summaries).toHaveLength(1);
  expect(loadDiff()).toBeTruthy();

  // This is still the old Uncommitted review's real DiffView and Load diff action.
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(1);
  expect(loads.files[0]).toMatchObject({ base: head, paths: [path] });

  await act(async () => loads.summaries[0].resolve(summary("branch", mergeBase, branchRevision)));
  await settle();
  expect(host.textContent).toContain(t("review.against_branch", { ref: "main", sha: mergeBase.slice(0, 7) }));
  expect(loadDiff()).toBeTruthy();

  await act(async () => loads.files[0].resolve(fileResponse(head, oldRevision, "OLD-TRANSITION-HUNK")));
  await settle();
  expect(host.textContent).not.toContain("OLD-TRANSITION-HUNK");
  expect(loadDiff()).toBeTruthy();

  // A fresh load of the displayed review still renders, and real DiffView comments use that file rather than the old one.
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(2);
  expect(loads.files[1]).toMatchObject({ base: mergeBase, paths: [path] });
  await act(async () => loads.files[1].resolve(fileResponse(mergeBase, branchRevision, "FRESH-BRANCH-HUNK")));
  await settle();
  expect(host.textContent).toContain("FRESH-BRANCH-HUNK");
  expect(host.textContent).not.toContain("OLD-TRANSITION-HUNK");

  const line = host.querySelector<HTMLButtonElement>(`[data-diff-path="${path}"] [data-ln="new"][data-n="1"]`);
  expect(line).toBeTruthy();
  await act(async () => line!.click());
  const box = host.querySelector<HTMLTextAreaElement>(`[data-diff-path="${path}"] textarea`);
  expect(box).toBeTruthy();
  await type(box!, "Current branch comment");
  await click(t("extensions-review.start_review"));

  expect(pendingReview("m/s").comments).toHaveLength(1);
  expect(pendingReview("m/s").comments[0]).toMatchObject({
    path,
    code: ["+FRESH-BRANCH-HUNK"],
    diff: { kind: "review", scope: "branch", base: mergeBase.slice(0, 7) },
  });
  expect(pendingReview("m/s").comments[0].code).not.toContain("+OLD-TRANSITION-HUNK");
});

it("rejects an old review load resolved in the same task as its replacement summary", async () => {
  loads.directFiles = true;
  await click(t("review.branch"));
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(1);

  // Resolve both component-facing promises in one task, outside act's render batching. The summary callback clears
  // loaded, then the old file continuation runs before React's queued render can update the owner ref.
  await outsideAct(async () => {
    loads.summaries[0].resolve(summary("branch", mergeBase, branchRevision));
    loads.files[0].resolve(fileResponse(head, oldRevision, "OLD-TRANSITION-HUNK"));
    for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
  });
  expect(host.textContent).toContain(t("review.against_branch", { ref: "main", sha: mergeBase.slice(0, 7) }));
  expect(host.textContent).not.toContain("OLD-TRANSITION-HUNK");
  expect(loadDiff()).toBeTruthy();

  // A fresh Branch load still works, and a comment made through the real DiffView cannot quote the old response.
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(2);
  expect(loads.files[1]).toMatchObject({ base: mergeBase, paths: [path] });
  await act(async () => loads.files[1].resolve(fileResponse(mergeBase, branchRevision, "FRESH-BRANCH-HUNK")));
  await settle();
  expect(host.textContent).toContain("FRESH-BRANCH-HUNK");
  expect(host.textContent).not.toContain("OLD-TRANSITION-HUNK");

  const line = host.querySelector<HTMLButtonElement>(`[data-diff-path="${path}"] [data-ln="new"][data-n="1"]`);
  expect(line).toBeTruthy();
  await act(async () => line!.click());
  const box = host.querySelector<HTMLTextAreaElement>(`[data-diff-path="${path}"] textarea`);
  expect(box).toBeTruthy();
  await type(box!, "Same-task branch comment");
  await click(t("extensions-review.start_review"));

  expect(pendingReview("m/s").comments).toHaveLength(1);
  expect(pendingReview("m/s").comments[0]).toMatchObject({
    path,
    code: ["+FRESH-BRANCH-HUNK"],
    diff: { kind: "review", scope: "branch", base: mergeBase.slice(0, 7) },
  });
  expect(pendingReview("m/s").comments[0].code).not.toContain("+OLD-TRANSITION-HUNK");
});

it("clears an old review file that completes before its replacement summary", async () => {
  await click(t("review.branch"));
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(1);

  await act(async () => loads.files[0].resolve(fileResponse(head, oldRevision, "OLD-EARLY-HUNK")));
  await settle();
  expect(host.textContent).toContain("OLD-EARLY-HUNK");

  await act(async () => loads.summaries[0].resolve(summary("branch", mergeBase, branchRevision)));
  await settle();
  expect(host.textContent).not.toContain("OLD-EARLY-HUNK");
  expect(loadDiff()).toBeTruthy();
});
