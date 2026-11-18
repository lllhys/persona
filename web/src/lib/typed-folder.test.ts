import { describe, expect, it } from "vitest";
import { startingWith, typedFolder } from "./typed-folder";

describe("typedFolder", () => {
  it("follows full paths only", () => {
    expect(typedFolder("")).toBeNull();
    expect(typedFolder("   ")).toBeNull();
    expect(typedFolder("code/app")).toBeNull();
    expect(typedFolder("~code")).toBeNull();
    expect(typedFolder("~")).toEqual({ path: "~" });
    expect(typedFolder(" /srv/work/ ")).toEqual({ path: "/srv/work/" });
  });

  it("looks beside a path's last part when the path names no folder", () => {
    expect(typedFolder("/srv/work/al")).toEqual({ path: "/srv/work/al", near: { path: "/srv/work", prefix: "al" } });
    expect(typedFolder("/sr")).toEqual({ path: "/sr", near: { path: "/", prefix: "sr" } });
    expect(typedFolder("~/co")).toEqual({ path: "~/co", near: { path: "~", prefix: "co" } });
    expect(typedFolder("C:\\Users\\me\\co")).toEqual({ path: "C:\\Users\\me\\co", near: { path: "C:\\Users\\me", prefix: "co" } });
    expect(typedFolder("C:\\co")).toEqual({ path: "C:\\co", near: { path: "C:\\", prefix: "co" } });
  });
});

describe("startingWith", () => {
  const entries = [{ name: "Alpha" }, { name: "alpine" }, { name: "beta" }];
  it("keeps the names starting with the prefix, ignoring case", () => {
    expect(startingWith(entries, "AL").map((e) => e.name)).toEqual(["Alpha", "alpine"]);
    expect(startingWith(entries, "")).toBe(entries);
    expect(startingWith(undefined, "a")).toEqual([]);
  });
});
