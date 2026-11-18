// @vitest-environment jsdom
// The Compare runs page (#8916) over a group in this browser, the store's sessions and synthetic git/review answers.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { getState } from "../lib/store";
import { setToken } from "../lib/api";
import { addGroup, resetGroupsCache, type CompareGroup } from "../lib/compare-runs";
import { resetRunner } from "../lib/compare-runner";
import { closePanel, getPanel } from "../lib/panel";
import { REVIEW_FEATURE } from "../lib/session-review";
import { STATE_LABEL, stateName } from "../lib/state-labels";
import type { Session } from "../lib/types";
import { CompareRuns } from "./CompareRuns";
import { t } from "../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const before = { ...getState() };
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
let root: Root;
let host: HTMLDivElement;
let reviews: string[];
const flush = () => act(async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); });
const card = (n: number) => document.querySelector<HTMLElement>(`[data-run="${n}"]`)!;
const buttonIn = (el: Element, text: string) => [...el.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === text);
const session = (over: Partial<Session>): Session => ({ machine_id: "computer", session_id: "s1", title: "Synthetic run one", cwd: "/projects/demo-run-1", status: "idle", ...over }) as Session;
const group: CompareGroup = {
  id: "g1", at: Date.now() - 120_000, mid: "computer", cwd: "/projects/demo", spec: { launcher: "claude" }, attachments: false,
  runs: [
    { n: 1, branch: "persona/task-1", phase: "launched", path: "/projects/demo-run-1", sid: "s1" },
    { n: 2, branch: "persona/task-2", phase: "launched", path: "/projects/demo-run-2" },
    { n: 3, branch: "persona/task-3", phase: "failed", error: "Synthetic worktree refusal" },
    { n: 4, branch: "persona/task-4", phase: "waiting" },
  ],
};
async function render(id = "g1") {
  await act(async () => root.render(<CompareRuns id={id} onToggleSidebar={() => {}} />));
  await flush();
}

beforeEach(() => {
  localStorage.clear();
  resetGroupsCache();
  resetRunner();
  closePanel();
  setToken("synthetic-credential-a");
  reviews = [];
  Object.assign(getState(), {
    loaded: true, features: [REVIEW_FEATURE], launches: {}, machines: [{ id: "computer", name: "Synthetic computer", online: true }],
    sessions: { computer: [session({}), session({ session_id: "s2", title: "Synthetic run two", cwd: "/projects/demo-run-2", status: "working" })] },
  });
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes("/git/review?")) {
      const cwd = new URL(url, "http://hub").searchParams.get("cwd")!;
      reviews.push(cwd);
      return cwd.endsWith("-1")
        ? json({ root: cwd, scope: "branch", base: {}, files: [{ path: "a.ts", status: "modified", added: 4, deleted: 1 }, { path: "b.ts", status: "added", added: 1, deleted: 0 }] })
        : json({ root: cwd, scope: "branch", base: {}, files: [] });
    }
    return json({});
  }));
  history.replaceState({}, "", "/compare/g1");
  host = document.createElement("div"); document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  Object.assign(getState(), before);
  closePanel();
  resetGroupsCache();
  localStorage.clear();
  vi.unstubAllGlobals();
  history.replaceState({}, "", "/");
});

describe("Compare runs page (#8916)", () => {
  it("says a comparison this browser does not keep is not here", async () => {
    await render("missing");
    expect(document.body.textContent).toContain(t("compare-runs.not_found_title"));
    expect(document.querySelector("[data-compare-runs]")).toBeNull();
  });

  it("shows each run: a session with the sidebar's title and state and its changes, a failed one's reason, one not begun stopped", async () => {
    addGroup(group);
    await render();
    expect(document.body.textContent).toContain(t("compare-runs.summary", { count: 4, computer: "Synthetic computer" }));
    // run 1, recorded; run 2, found by its worktree
    expect(card(1).textContent).toContain("Synthetic run one");
    expect(card(1).textContent).toContain(STATE_LABEL.idle);
    expect(card(1).querySelector("[data-run-changes]")!.textContent).toBe(t("compare-runs.changes", { count: 2, added: 5, deleted: 1 }));
    expect(card(2).textContent).toContain("Synthetic run two");
    expect(card(2).textContent).toContain(STATE_LABEL.working);
    expect(card(2).querySelector("[data-run-changes]")!.textContent).toBe(t("compare-runs.no_changes"));
    // run 3: its reason, and no retry once the tab no longer holds the task
    expect(card(3).dataset.runStatus).toBe("failed");
    expect(card(3).textContent).toContain(stateName("failed"));
    expect(card(3).querySelector('[role="alert"]')!.textContent).toBe("Synthetic worktree refusal");
    expect(buttonIn(card(3), t("compare-runs.retry"))).toBeUndefined();
    // run 4: this tab is not working through it
    expect(card(4).dataset.runStatus).toBe("stopped");
    expect(card(4).textContent).toContain(t("compare-runs.stopped_gone"));
    expect(card(4).textContent).toContain("persona/task-4");
  });

  it("opens a run, or its review at the Branch scope, without merging or deleting anything", async () => {
    addGroup(group);
    await render();
    await act(async () => buttonIn(card(1), t("compare-runs.review"))!.click());
    expect(getPanel()).toEqual({ kind: "review", mid: "computer", sid: "s1", cwd: "/projects/demo-run-1", scope: "branch" });
    expect(location.pathname).toBe("/s/computer/s1");
    history.replaceState({}, "", "/compare/g1");
    await act(async () => buttonIn(card(2), t("compare-runs.open"))!.click());
    expect(location.pathname).toBe("/s/computer/s2");
    expect((fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.some(([u]) => /worktree\/remove|git\/(merge|write)/.test(String(u)))).toBe(false);
  });

  it("reads the changes again on refresh and when a run's state moves, and hides them on a hub without reviews", async () => {
    addGroup(group);
    await render();
    expect(reviews.sort()).toEqual(["/projects/demo-run-1", "/projects/demo-run-2"]);
    await act(async () => document.querySelector<HTMLButtonElement>(`button[aria-label="${t("compare-runs.refresh")}"]`)!.click());
    await flush();
    expect(reviews).toHaveLength(4);
    await act(async () => Object.assign(getState(), { sessions: { computer: [session({}), session({ session_id: "s2", cwd: "/projects/demo-run-2", status: "idle" })] } }));
    await render();
    expect(reviews.filter((c) => c.endsWith("-2"))).toHaveLength(3);
    await act(async () => Object.assign(getState(), { features: [] }));
    await render();
    expect(document.querySelector("[data-run-changes]")).toBeNull();
    expect(buttonIn(card(1), t("compare-runs.review"))).toBeUndefined();
  });
});
