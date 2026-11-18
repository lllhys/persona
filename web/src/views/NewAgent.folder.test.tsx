// @vitest-environment jsdom
// "New agent"'s folder list follows the folder field (#9452): a folder filled in (the last start's, a recent one picked)
// lists its subfolders under the recent folders; a path typed there, once typing pauses, lists its subfolders, or for a
// partial name the folder it is in keeping the names that start with it; browsing from the list takes over, and clearing
// the field brings back the recent folders and home.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../lib/types";
import { t } from "../i18n";

const state = vi.hoisted(() => ({ loaded: true, features: [] as string[], machines: [] as Machine[] }));
// one assistant: its computers are the store's (lib/current-assistant.ts reads the roster, which this store stand-in has none of)
vi.mock("../lib/current-assistant", async () => {
  const store = (await import("../lib/store")) as unknown as { useStore: <T>(select: (s: { machines: unknown[] }) => T) => T };
  return { useScopedMachines: () => store.useStore((s) => s.machines) };
});
vi.mock("../lib/store", () => ({ getState: () => state, useStore: (selector: (value: { machines: Machine[] }) => unknown) => selector(state), hubHas: () => false }));
vi.mock("react-dom", async (original) => ({ ...(await original<typeof import("react-dom")>()), createPortal: (children: React.ReactNode) => children }));

import { NewAgent } from "./NewAgent";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
const entry = (dir: string, name: string) => ({ name, path: `${dir}/${name}` });
// the computer's folders, as the node lists them (it cleans a trailing slash away, and every answer carries the recent
// folders); "" is home
const RECENT = [{ path: "/srv/work/recent-repo", name: "recent-repo" }];
const FOLDERS: Record<string, unknown> = {
  "": { path: "/home/me", parent: "/home", entries: [entry("/home/me", "notes")] },
  "/srv/work/recent-repo": { path: "/srv/work/recent-repo", parent: "/srv/work", entries: [entry("/srv/work/recent-repo", "docs")] },
  "/srv/work": { path: "/srv/work", parent: "/srv", entries: [entry("/srv/work", "Alpha"), entry("/srv/work", "alpine"), entry("/srv/work", "beta")] },
  "/srv/work/beta": { path: "/srv/work/beta", parent: "/srv/work", entries: [entry("/srv/work/beta", "src")] },
  "/srv": { path: "/srv", parent: "/", entries: [entry("/srv", "work")] },
};
let asked: string[];
let host: HTMLDivElement;
let root: Root;
const tick = (ms = 0) => act(async () => { await vi.advanceTimersByTimeAsync(ms); });
const field = () => [...host.querySelectorAll<HTMLInputElement>("input")].find((i) => i.placeholder === t("newagent.folder_placeholder"))!;
async function typeFolder(value: string) {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(field(), value);
    field().dispatchEvent(new Event("input", { bubbles: true }));
  });
}
// the folder rows (titled with their paths), in order, the recent ones included
const folderRows = () => [...host.querySelectorAll<HTMLButtonElement>("button[title]")].filter((b) => b.title.startsWith("/"));
const rows = () => folderRows().map((b) => b.querySelector("span")?.textContent ?? "");
const showsRecent = () => host.textContent?.includes(t("newagent.recent")) ?? false;
const upButton = () => host.querySelector<HTMLButtonElement>(`[aria-label="${t("newagent.parent_folder")}"]`);
let opening = 0;
// the sheet opened again, reading the folder filled in afresh
async function reopen() {
  await act(async () => root.render(<NewAgent open onClose={() => {}} key={++opening} />));
  await tick();
}

beforeEach(async () => {
  vi.useFakeTimers();
  state.machines = [{ id: "computer", name: "Computer", online: true }];
  localStorage.clear();
  asked = [];
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "http://hub.invalid");
    if (!url.pathname.endsWith("/dirs")) return json({});
    const path = url.searchParams.get("path") ?? "";
    asked.push(path);
    const listed = FOLDERS[path.length > 1 ? path.replace(/\/+$/, "") : path];
    return listed ? json({ ...listed, recent: RECENT }) : json({ error: `open ${path}: no such file or directory`, code: "internal" }, 502);
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<NewAgent open onClose={() => {}} />));
  await tick();
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("NewAgent folder list follows the folder field", () => {
  it("lists a typed folder's subfolders once typing pauses, and goes back to the recent folders when cleared", async () => {
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "notes"]);

    await typeFolder("/srv/work/");
    await tick(100);
    expect(asked).not.toContain("/srv/work/"); // not on every keystroke
    await tick(200);
    expect(asked.at(-1)).toBe("/srv/work/");
    expect(showsRecent()).toBe(false);
    expect(rows()).toEqual(["Alpha", "alpine", "beta"]);
    expect(host.querySelector(`[aria-label="${t("newagent.parent_folder")}"]`)).not.toBeNull();

    await typeFolder("");
    await tick(300);
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "notes"]);
  });

  it("lists the folders starting with a partial name beside it, ignoring case", async () => {
    await typeFolder("/srv/work/al");
    await tick(300);
    expect(asked.slice(-2)).toEqual(["/srv/work/al", "/srv/work"]);
    expect(rows()).toEqual(["Alpha", "alpine"]);
    expect(field().value).toBe("/srv/work/al"); // the list never rewrites what was typed

    // picking a row fills the field and keeps the list where it is
    await act(async () => folderRows().find((b) => b.title === "/srv/work/alpine")!.click());
    await tick(300);
    expect(field().value).toBe("/srv/work/alpine");
    expect(rows()).toEqual(["Alpha", "alpine"]);
  });

  it("lets browsing from the list take over until the next keystroke", async () => {
    await typeFolder("/srv/work");
    await tick(300);
    await act(async () => host.querySelector<HTMLButtonElement>(`[aria-label="${t("newagent.open_folder", { name: "beta" })}"]`)!.click());
    await tick();
    expect(rows()).toEqual(["src"]);
    await act(async () => host.querySelector<HTMLButtonElement>(`[aria-label="${t("newagent.parent_folder")}"]`)!.click());
    await tick();
    expect(rows()).toEqual(["Alpha", "alpine", "beta"]);
    await act(async () => host.querySelector<HTMLButtonElement>(`[aria-label="${t("newagent.parent_folder")}"]`)!.click());
    await tick();
    expect(rows()).toEqual(["work"]);

    await typeFolder("/srv/work/b");
    await tick(300);
    expect(rows()).toEqual(["beta"]);
  });

  it("leaves a relative path alone: it names no folder the computer would agree on", async () => {
    await typeFolder("work/al");
    await tick(300);
    expect(asked.every((p) => p === "")).toBe(true);
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "notes"]);
  });
});

describe("NewAgent folder list follows a folder filled in", () => {
  it("lists the last start's folder under the recent folders when the sheet opens", async () => {
    localStorage.setItem("new.cwd.computer", "/srv/work/beta");
    await reopen();
    expect(field().value).toBe("/srv/work/beta");
    expect(asked.at(-1)).toBe("/srv/work/beta");
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "src"]);
    await act(async () => upButton()!.click());
    await tick();
    expect(showsRecent()).toBe(false); // browsing by hand, as before
    expect(rows()).toEqual(["Alpha", "alpine", "beta"]);
  });

  it("follows a recent folder picked, keeping the recent folders", async () => {
    await act(async () => folderRows().find((b) => b.title === "/srv/work/recent-repo")!.click());
    await tick();
    expect(field().value).toBe("/srv/work/recent-repo");
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "docs"]);
  });

  it("starts where browsing starts when the folder filled in is gone", async () => {
    localStorage.setItem("new.cwd.computer", "/srv/work/gone");
    await reopen();
    expect(asked.slice(-2)).toEqual(["/srv/work/gone", ""]);
    expect(showsRecent()).toBe(true);
    expect(rows()).toEqual(["recent-repo", "notes"]);
  });
});
