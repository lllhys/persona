// @vitest-environment jsdom
// A big file is loaded on demand. Its response belongs to the review that started it, not to whichever review now has
// the same path: a late response must not put an old diff (or its line metadata) under the new review's comments.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { GitReview, GitReviewFile } from "../../lib/session-review";
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
      if (scope === "branch") return summary("branch", mergeBase, branchRevision);
      throw new Error(`unexpected scope: ${scope}`);
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
  localStorage.clear();
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
const button = (label: string) => [...host.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label);
const loadDiff = () => button(t("extensions-codeviews.load_diff"));

it("ignores a big-file response from the previous review but renders the current review's response", async () => {
  expect(loadDiff()).toBeTruthy();
  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(1);
  expect(loads.files[0]).toMatchObject({ base: head, paths: [path] });

  await act(async () => button(t("review.branch"))!.click());
  await settle();
  expect(host.textContent).toContain(t("review.against_branch", { ref: "main", sha: mergeBase.slice(0, 7) }));
  expect(loadDiff()).toBeTruthy();

  await act(async () => loads.files[0].resolve(fileResponse(head, oldRevision, "OLD-UNCOMMITTED-HUNK")));
  await settle();
  expect(host.textContent).not.toContain("OLD-UNCOMMITTED-HUNK");
  expect(loadDiff()).toBeTruthy();

  await act(async () => loadDiff()!.click());
  expect(loads.files).toHaveLength(2);
  expect(loads.files[1]).toMatchObject({ base: mergeBase, paths: [path] });
  await act(async () => loads.files[1].resolve(fileResponse(mergeBase, branchRevision, "FRESH-BRANCH-HUNK")));
  await settle();

  expect(host.textContent).toContain("FRESH-BRANCH-HUNK");
  expect(host.textContent).not.toContain("OLD-UNCOMMITTED-HUNK");
  expect(loadDiff()).toBeUndefined();
});
