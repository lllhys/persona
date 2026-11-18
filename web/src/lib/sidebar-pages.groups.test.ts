// @vitest-environment jsdom
// The sidebar's groups (#9071): the default layout is Pages alone; an extension's nav_page names its group; the user's
// arrangement overrides it; groups are made, renamed, moved and removed, and removing one never loses a page.
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  addPageGroup, arrangeSections, getSidebarPrefs, movePageGroup, pageGroups, readPrefs, removePageGroup, renamePageGroup, resetSidebarPrefs,
  restoreSidebarPrefs, setPageGroup, type SidebarPrefs,
} from "./sidebar-pages";

const NONE: SidebarPrefs = { order: [], shown: [], hidden: [] };
const WORK = { id: "workbench", title: "工作台" };
// the sidebar's pages in their default order: built-in, then extensions' (Today and Calendar name 工作台, Notes none)
const PAGES = [
  { k: "home" },
  { k: "inbox" },
  { k: "routines" },
  { k: "p:console/today", group: WORK },
  { k: "p:console/calendar", group: WORK },
  { k: "p:notes/notes" },
  { k: "settings" },
];
const drawn = (prefs: SidebarPrefs, pages = PAGES) =>
  arrangeSections(pages, prefs).sections.map((s) => [s.id, s.title ?? "Pages", s.pages.map((p) => p.k)]);
const ids = () => pageGroups(PAGES, getSidebarPrefs()).map((g) => g.id);

beforeEach(() => localStorage.clear());
afterEach(() => resetSidebarPrefs());

describe("the default layout", () => {
  it("is Pages alone, as before, when no extension names a group and the user made none", () => {
    const plain = PAGES.map(({ k }) => ({ k }));
    expect(drawn(NONE, plain)).toEqual([["", "Pages", ["home", "inbox", "routines", "p:console/today", "p:console/calendar", "p:notes/notes", "settings"]]]);
    // extension pages past the first three wait under More as before (EXTENSION_CAP)
    const more = [...plain.slice(0, -1), { k: "p:x/a" }, { k: "p:x/b" }, { k: "settings" }];
    expect(arrangeSections(more, NONE).more.map((p) => p.k)).toEqual(["p:x/a", "p:x/b"]);
  });
});

describe("an extension's group", () => {
  it("places its pages in a group after Pages, titled as the manifest says", () => {
    expect(drawn(NONE)).toEqual([
      ["", "Pages", ["home", "inbox", "routines", "p:notes/notes", "settings"]],
      ["workbench", "工作台", ["p:console/today", "p:console/calendar"]],
    ]);
  });
});

describe("the user's arrangement", () => {
  it("overrides the manifest: a page moved to Pages or to the user's own group stays there", () => {
    setPageGroup("p:console/today", "");
    const life = addPageGroup(ids(), "生活");
    setPageGroup("routines", life);
    setPageGroup("settings", life); // Settings stays in Pages
    expect(drawn(getSidebarPrefs())).toEqual([
      ["", "Pages", ["home", "inbox", "p:console/today", "p:notes/notes", "settings"]],
      ["workbench", "工作台", ["p:console/calendar"]],
      [life, "生活", ["routines"]],
    ]);
  });

  it("renames a group, and a blank name gives an extension's group its own back", () => {
    renamePageGroup(ids(), "workbench", "Work");
    expect(drawn(getSidebarPrefs())[1].slice(0, 2)).toEqual(["workbench", "Work"]);
    renamePageGroup(ids(), "workbench", "  ");
    expect(drawn(getSidebarPrefs())[1].slice(0, 2)).toEqual(["workbench", "工作台"]);
  });

  it("reorders the groups, Pages too", () => {
    const life = addPageGroup(ids(), "生活");
    setPageGroup("routines", life);
    movePageGroup(ids(), "", 1);
    expect(drawn(getSidebarPrefs()).map((s) => s[0])).toEqual(["workbench", "", life]);
    movePageGroup(ids(), life, -1);
    movePageGroup(ids(), life, -1);
    expect(drawn(getSidebarPrefs()).map((s) => s[0])).toEqual([life, "workbench", ""]);
  });

  it("removing the user's group gives its pages back to where they were: the extension's group, else Pages", () => {
    const life = addPageGroup(ids(), "生活");
    setPageGroup("routines", life);
    setPageGroup("p:console/today", life);
    removePageGroup(life);
    expect(drawn(getSidebarPrefs())).toEqual(drawn(NONE));
    // nothing is kept of it: a group made later never takes its pages by surprise
    expect(getSidebarPrefs().assign).toEqual({});
  });

  it("removing an extension's group puts its pages in Pages, and Undo brings it back", () => {
    const before = getSidebarPrefs();
    removePageGroup("workbench");
    const after = drawn(getSidebarPrefs());
    expect(after).toEqual([["", "Pages", ["home", "inbox", "routines", "p:console/today", "p:console/calendar", "p:notes/notes", "settings"]]]);
    // every page is still there
    expect(after.flatMap((s) => s[2]).sort()).toEqual(PAGES.map((p) => p.k).sort());
    restoreSidebarPrefs(before);
    expect(drawn(getSidebarPrefs())).toEqual(drawn(NONE));
  });

  it("a page moved to a group that is gone goes back to where it would be", () => {
    expect(drawn({ ...NONE, assign: { "p:console/today": "u:gone", inbox: "u:gone" } })).toEqual(drawn(NONE));
  });

  it("reads back what it stored, and what the hub sends, ignoring what is not of its shape", () => {
    const life = addPageGroup(ids(), "生活");
    setPageGroup("routines", life);
    expect(readPrefs(JSON.parse(JSON.stringify(getSidebarPrefs())))).toEqual(getSidebarPrefs());
    expect(readPrefs({ order: ["home", 3], groups: [{ id: "u:x", title: "X" }, { title: "no id" }], assign: { a: "u:x", b: 1 }, removed: "nope" })).toEqual({
      order: ["home"], shown: [], hidden: [], groups: [{ id: "u:x", title: "X" }], assign: { a: "u:x" },
    });
    expect(readPrefs(null)).toEqual(NONE);
  });
});
