// Inbox folders (docs/assistant/inbox.md "Folders"): paths kept as the hub keeps them, the folder filter (a folder and
// everything under it, or the unfiled), and the tree the folder bar draws.
import { describe, expect, it } from "vitest";
import { childFolders, folderCrumbs, inFolderFilter, knownFolders, normalizeFolder, unfiledCount, UNFILED } from "./inbox-folders";
import { matches, sections, waitingAgents, type InboxItem } from "./inbox";

const item = (id: string, folder?: string, done = false): InboxItem => ({
  id, source: "mail", kind: "mail", account: "a", account_name: "me@example.com", title: id, preview: "", count: 1, tier: "fyi",
  folder, done_at: done ? "2026-11-09T08:00:00Z" : null,
});

describe("inbox folders", () => {
  it("normalizes paths like the hub", () => {
    expect(normalizeFolder(" /work//alerts/ ")).toBe("work/alerts");
    expect(normalizeFolder("work /  build   bot ")).toBe("work/build bot");
    expect(normalizeFolder("../etc/./x")).toBe("etc/x");
    expect(normalizeFolder("a\\b")).toBe("a/b");
    expect(normalizeFolder("a/b/c/d/e/f/g")).toBe("a/b/c/d/e/f");
    expect(normalizeFolder("x".repeat(50))).toBe("x".repeat(40));
    expect(normalizeFolder("/")).toBe("");
    expect(normalizeFolder("工作/告警")).toBe("工作/告警");
  });

  it("filters a folder with everything under it, or the unfiled", () => {
    expect(inFolderFilter({ folder: "work/alerts/ci" }, "work")).toBe(true);
    expect(inFolderFilter({ folder: "work/alerts" }, "work/alerts")).toBe(true);
    expect(inFolderFilter({ folder: "workshop" }, "work")).toBe(false);
    expect(inFolderFilter({}, "work")).toBe(false);
    expect(inFolderFilter({}, UNFILED)).toBe(true);
    expect(inFolderFilter({ folder: "work" }, UNFILED)).toBe(false);
    expect(inFolderFilter({ folder: "work" }, "")).toBe(true);
  });

  it("builds the levels under a folder with their counts", () => {
    const items = [item("1", "work/alerts/ci"), item("2", "work/alerts/engine"), item("3", "work/alerts", true), item("4", "projects/foo"), item("5"), item("6", undefined, true)];
    expect(childFolders(items, "")).toEqual([
      { path: "projects", name: "projects", count: 1, open: 1 },
      { path: "work", name: "work", count: 3, open: 2 },
    ]);
    expect(childFolders(items, "work/alerts").map((f) => f.name)).toEqual(["ci", "engine"]);
    expect(childFolders(items, "work/alerts/ci")).toEqual([]);
    expect(unfiledCount(items)).toEqual({ count: 2, open: 1 });
    expect(folderCrumbs("work/alerts/ci")).toEqual([{ name: "work", path: "work" }, { name: "alerts", path: "work/alerts" }, { name: "ci", path: "work/alerts/ci" }]);
    expect(folderCrumbs(UNFILED)).toEqual([]);
    expect(knownFolders(items)).toEqual(["projects", "projects/foo", "work", "work/alerts", "work/alerts/ci", "work/alerts/engine"]);
  });

  it("the list's filter and sections follow the folder", () => {
    const items = [item("1", "work/alerts/ci"), item("2", "projects/foo"), item("3")];
    const f = { source: "", kind: "", query: "", folder: "work" };
    expect(items.filter((it) => matches(it, f)).map((it) => it.id)).toEqual(["1"]);
    expect(sections(items, { ...f, folder: UNFILED }).tiers.fyi.map((it) => it.id)).toEqual(["3"]);
    // agents have no folder: picking one leaves them out
    expect(waitingAgents([{ id: "ix", kind: "question", machine_id: "m", session_id: "s", created_at: "" } as never], {}, {} as never, f)).toEqual([]);
  });
});
