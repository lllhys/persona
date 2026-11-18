// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GitCommit, GitStatus } from "../../lib/files";
import { t } from "../../i18n";

const live = vi.hoisted(() => ({ changed: () => {} }));
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: (_mid: string, _cwd: string, _paths: string[], _git: boolean, changed: (value: { git: boolean }) => void) => { live.changed = () => changed({ git: true }); } }));

import { GitPanel } from "./GitPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type Pending = { path: string; answer: (value: unknown) => void };
let pending: Pending[];
let host: HTMLDivElement;
let root: Root;
const commit = (name: string): GitCommit => ({ hash: name, short: name, subject: name, author: "Author" });
const status = (name: string): GitStatus => ({ is_repo: true, branch: "main", head: commit(name), files: [], commits: Array.from({ length: 30 }, (_, index) => commit(`${name}-${index}`)) });
const moreButton = () => [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === t("session-gitpanel.load_older_commits"));

beforeEach(() => {
  pending = [];
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL) => new Promise<Response>((resolve) => pending.push({ path: String(input), answer: (value) => resolve(new Response(JSON.stringify(value))) }))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<GitPanel mid="computer" sid="session" cwd="/projects/demo" onClose={() => {}} />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("GitPanel older history", () => {
  it("drops a pending page after HEAD changes and allows another load", async () => {
    await act(async () => pending[0]!.answer(status("old-head")));
    act(() => moreButton()!.click());
    const oldPage = pending.at(-1)!;
    expect(oldPage.path).toContain("/log?");
    expect(oldPage.path).toContain("skip=30");
    act(() => live.changed());
    expect(pending.at(-1)!.path).toContain("/status?");
    await act(async () => pending.at(-1)!.answer(status("new-head")));
    await act(async () => oldPage.answer({ commits: [commit("stale-commit")], has_more: false }));
    expect(host.textContent).toContain("new-head-0");
    expect(host.textContent).not.toContain("stale-commit");
    expect(moreButton()).toBeDefined();
    expect(moreButton()!.disabled).toBe(false);
    act(() => moreButton()!.click());
    await act(async () => pending.at(-1)!.answer({ commits: [commit("current-older-commit")], has_more: false }));
    expect(host.textContent).toContain("current-older-commit");
    expect(moreButton()).toBeUndefined();
  });

  it("keeps a pending page when a silent status refresh has the same HEAD", async () => {
    await act(async () => pending[0]!.answer(status("same-head")));
    act(() => moreButton()!.click());
    const page = pending.at(-1)!;
    act(() => live.changed());
    await act(async () => pending.at(-1)!.answer(status("same-head")));
    await act(async () => page.answer({ commits: [commit("valid-older-commit")], has_more: false }));
    expect(host.textContent).toContain("valid-older-commit");
    expect(moreButton()).toBeUndefined();
  });
});

describe("GitPanel commit file contents", () => {
  it.each([
    ["plain.ts", "b/plain.ts"],
    ["my docs/a b.ts", "b/my docs/a b.ts"],
    ["你好.ts", "b/你好.ts"],
    ['say"hello.ts', String.raw`"b/say\"hello.ts"`],
    ["a\tb.ts", String.raw`"b/a\tb.ts"`],
    ["a\\b.ts", String.raw`"b/a\\b.ts"`],
  ])("opens and copies the full diff for %j", async (path, target) => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: { writeText } });
    try {
      await act(async () => pending[0]!.answer(status("head")));
      act(() => [...host.querySelectorAll("button")].find((button) => button.textContent?.includes("head-0"))!.click());
      expect(pending.at(-1)!.path).toContain("/show?");
      const diff = `diff --git a/old.ts ${target}\n--- a/old.ts\n+++ ${target}\n@@ -1 +1 @@\n-before\n+after_unique_line\n`;
      await act(async () => pending.at(-1)!.answer({ commit: commit("head-0"), files: [{ path, index: "M", worktree: ".", added: 1, deleted: 1 }], diff, truncated: false }));
      act(() => [...host.querySelectorAll("button")].find((button) => button.textContent?.includes(path.split("/").at(-1)!))!.click());
      await act(async () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("session-gitpanel.copy_diff")}"]`)!.click());
      expect.soft(host.textContent).toContain("after_unique_line");
      expect.soft(writeText).toHaveBeenCalledExactlyOnceWith(diff);
    } finally {
      if (original) Object.defineProperty(navigator, "clipboard", original);
      else Reflect.deleteProperty(navigator, "clipboard");
    }
  });
});
