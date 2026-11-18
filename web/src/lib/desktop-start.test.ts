// New agent in a desktop app (#5480, #5726): the same rules as iOS's DesktopStartTests.
import { describe, expect, it } from "vitest";
import { isCodexChatFolder, newFolderPath, projectsFolder, startsInDesktopApp } from "./desktop-start";
import type { Session } from "./types";

const row = (cwd: string, agent = "claude", agent_surface: string | undefined = "desktop"): Session =>
  ({ machine_id: "mac", session_id: cwd, title: "", cwd, status: "idle", agent, agent_surface }) as Session;

describe("startsInDesktopApp", () => {
  it("is the launcher by its source", () => {
    expect(startsInDesktopApp({ source: "claude_desktop" })).toBe(true);
    expect(startsInDesktopApp({ source: "codex_app" })).toBe(true);
    expect(startsInDesktopApp({ source: "runtime" })).toBe(false);
    expect(startsInDesktopApp({})).toBe(false);
    expect(startsInDesktopApp(undefined)).toBe(false);
  });
});

describe("projectsFolder", () => {
  it("is the folder holding most of the desktop app's sessions' folders", () => {
    const list = [row("/Users/me/code/a"), row("/Users/me/code/b"), row("/Users/me/notes/c")];
    expect(projectsFolder(list)).toBe("/Users/me/code");
  });
  it("counts Claude's desktop sessions only, not the terminal's or the Codex App's", () => {
    const list = [row("/Users/me/notes/a"), row("/Users/me/code/b", "claude", "cli"), row("/Users/me/code/c", "claude", ""), row("/Users/me/code/d", "codex")];
    expect(projectsFolder(list)).toBe("/Users/me/notes");
  });
  it("counts the scratch folder where it is", () => {
    const list = [row("/Users/me/Documents/ClaudeCode/临时会话"), row("/Users/me/Documents/ClaudeCode/web")];
    expect(projectsFolder(list)).toBe("/Users/me/Documents/ClaudeCode");
  });
  it("breaks a tie by name", () => {
    expect(projectsFolder([row("/Users/me/zeta/a"), row("/Users/me/alpha/b")])).toBe("/Users/me/alpha");
  });
  it("leaves out the root and paths that are not absolute", () => {
    expect(projectsFolder([row("/a"), row("relative/b"), row("")])).toBeUndefined();
  });
  it("with codex counts the Codex App's projects, not its chats in no project", () => {
    const list = [row("/Users/me/Documents/Codex/2026-10-27/pei-z", "codex"), row("/Users/me/Documents/Codex/2026-10-28/x", "codex"), row("/Users/me/code/app", "codex"), row("/Users/me/notes/a")];
    expect(projectsFolder(list, true)).toBe("/Users/me/code");
  });
});

describe("isCodexChatFolder", () => {
  it("is <home>/Documents/Codex/<date>/<name>", () => {
    expect(isCodexChatFolder("/Users/me/Documents/Codex/2026-10-27/pei-z")).toBe(true);
    expect(isCodexChatFolder("C:\\Users\\me\\Documents\\Codex\\2026-10-27\\pei-z")).toBe(true);
    expect(isCodexChatFolder("/Users/me/Documents/Codex/notes/pei-z")).toBe(false);
    expect(isCodexChatFolder("/Codex/2026-10-27/x")).toBe(false);
  });
});

describe("newFolderPath", () => {
  it("is one folder's name in the folder browsed", () => {
    expect(newFolderPath(" web ", "/Users/me/code")).toBe("/Users/me/code/web");
    expect(newFolderPath("web", "/Users/me/code/")).toBe("/Users/me/code/web");
  });
  it("refuses what is not one name", () => {
    for (const bad of ["", "  ", ".", "..", "a/b"]) expect(newFolderPath(bad, "/Users/me/code")).toBeUndefined();
    expect(newFolderPath("web", "relative")).toBeUndefined();
  });
});
