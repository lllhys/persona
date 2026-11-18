import { describe, expect, it } from "vitest";
import { historyBack, historyForward, historyGo, HISTORY_LIMIT, modeOctal, modeSymbolic, parentPath, pathSegments, tildePath, writableHere, type FilesHistory } from "./files-nav";

// The file page's navigation (#5827): the same cases as the iPhone's FilesNavigationTests.
describe("files navigation", () => {
  it("walks back and forward like a browser", () => {
    let h: FilesHistory = { current: "/w/app", back: [], forward: [] };
    h = historyGo(historyGo(h, "/w/app/src"), "/w/app/src/lib");
    h = historyBack(h);
    expect(h.current).toBe("/w/app/src");
    expect(h.forward).toEqual(["/w/app/src/lib"]);
    h = historyBack(h);
    expect(h.current).toBe("/w/app");
    expect(h.back).toEqual([]);
    h = historyForward(h);
    expect(h.current).toBe("/w/app/src");
    h = historyGo(h, "/w");
    expect(h.forward).toEqual([]);
    expect(h.back).toEqual(["/w/app", "/w/app/src"]);
    expect(historyGo(h, "/w")).toBe(h);
    expect(historyBack({ current: "/a", back: [], forward: [] }).current).toBe("/a");
  });

  it("keeps a bounded history", () => {
    let h: FilesHistory = { current: "/0", back: [], forward: [] };
    for (let i = 1; i <= HISTORY_LIMIT + 10; i++) h = historyGo(h, `/${i}`);
    expect(h.back).toHaveLength(HISTORY_LIMIT);
    expect(h.back[0]).toBe("/10");
  });

  it("goes up to /", () => {
    expect(parentPath("/Users/me/code")).toBe("/Users/me");
    expect(parentPath("/Users")).toBe("/");
    expect(parentPath("/")).toBeNull();
  });

  it("starts the path bar at the home inside it", () => {
    const inHome = pathSegments("/Users/me/code/app", "/Users/me");
    expect(inHome.map((s) => s.title)).toEqual(["~", "code", "app"]);
    expect(inHome.map((s) => s.path)).toEqual(["/Users/me", "/Users/me/code", "/Users/me/code/app"]);
    expect(inHome[0].home).toBe(true);
    expect(pathSegments("/Users/meg/x", "/Users/me").map((s) => s.title)).toEqual(["/", "Users", "meg", "x"]);
    expect(pathSegments("/", undefined).map((s) => s.path)).toEqual(["/"]);
  });

  it("shortens the home only", () => {
    expect(tildePath("/Users/me/a", "/Users/me")).toBe("~/a");
    expect(tildePath("/Users/me", "/Users/me")).toBe("~");
    expect(tildePath("/Users/meg", "/Users/me")).toBe("/Users/meg");
  });

  it("takes changes in the home but its private folders", () => {
    const home = "/Users/me";
    expect(writableHere("/Users/me/code", home)).toBe(true);
    expect(writableHere("/tmp", home)).toBe(false);
    expect(writableHere("/Users/meg", home)).toBe(false);
    expect(writableHere("/Users/me/.persona/x", home)).toBe(false);
    expect(writableHere("/Users/me/Library", home)).toBe(false);
    expect(writableHere("/Users/me/Library2", home)).toBe(true);
    expect(writableHere("/anything", undefined)).toBe(true);
  });

  it("writes permission bits", () => {
    expect(modeSymbolic(0o644)).toBe("rw-r--r--");
    expect(modeSymbolic(0o4755)).toBe("rwsr-xr-x");
    expect(modeSymbolic(0o1777)).toBe("rwxrwxrwt");
    expect(modeSymbolic(0o2640)).toBe("rw-r-S---");
    expect(modeOctal(0o4755)).toBe("4755");
  });
});
