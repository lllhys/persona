// @vitest-environment jsdom
// Inbox folders (docs/assistant/inbox.md "Folders"): the folder bar's crumbs and subfolders, "Move to folder…" sending the
// path (or handing the item back to AI), and the folder settings.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { InboxItem } from "../../lib/inbox";
import { resetFolderSettings } from "../../lib/inbox-folders";

vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { FolderBar, FolderSettingsDialog, MoveFolderDialog } from "./InboxFolders";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const item = (id: string, folder?: string, over: Partial<InboxItem> = {}): InboxItem => ({
  id, source: "mail", kind: "mail", account: "a", account_name: "me@example.com", title: `示例 ${id}`, preview: "", count: 1, tier: "fyi", folder, ...over,
});
const items = [item("mail:1", "work/alerts/ci"), item("mail:2", "work/alerts/engine"), item("mail:3", "projects/foo"), item("mail:4")];

let host: HTMLDivElement;
let root: Root;
let fetcher: ReturnType<typeof vi.fn>;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const button = (text: string) => [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === text);
const call = (method: string, url: string) => fetcher.mock.calls.find(([u, init]) => u === url && (init?.method ?? "GET") === method);

beforeEach(() => {
  resetFolderSettings();
  fetcher = vi.fn().mockImplementation(async (url: string, init?: { method?: string; body?: string }) => {
    let body: unknown = {};
    if (url.startsWith("/api/v1/inbox?")) body = { items };
    else if (url === "/api/v1/inbox/folders") body = init?.method === "PUT" ? { enabled: true, rules: "规则", folders: [] } : { enabled: false, rules: "", folders: [] };
    else if (url.endsWith("/folder")) body = { ...items[3], ...JSON.parse(init?.body ?? "{}"), folder_by: "user" };
    return { ok: true, status: 200, text: async () => JSON.stringify(body) };
  });
  vi.stubGlobal("fetch", fetcher);
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("收件箱文件夹", () => {
  it("shows the top folders and the unfiled, then the crumbs down to the one picked", () => {
    const onPick = vi.fn();
    act(() => root.render(<FolderBar items={items} folder="" onPick={onPick} />));
    expect(button("projects1")).toBeTruthy();
    expect(button("work2")).toBeTruthy();
    act(() => button("未归档1")!.click());
    expect(onPick).toHaveBeenLastCalledWith("/");
    act(() => button("work2")!.click());
    expect(onPick).toHaveBeenLastCalledWith("work");

    act(() => root.render(<FolderBar items={items} folder="work/alerts" onPick={onPick} />));
    const crumbs = [...document.querySelectorAll("ol button")].map((b) => b.textContent);
    expect(crumbs).toEqual(["全部文件夹", "work", "alerts"]);
    expect(document.querySelector('ol button[aria-current="page"]')?.textContent).toBe("alerts");
    expect(button("ci1")).toBeTruthy();
    expect(button("未归档1")).toBeUndefined(); // only at the top
    act(() => button("work")!.click());
    expect(onPick).toHaveBeenLastCalledWith("work");
  });

  it("moves an item to the folder typed, normalized, or hands it back to AI", async () => {
    const onClose = vi.fn();
    act(() => root.render(<MoveFolderDialog item={items[3]} onClose={onClose} />));
    await flush();
    const input = document.querySelector<HTMLInputElement>("input")!;
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
    act(() => {
      setValue.call(input, " work // alerts ");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    // the folders in use that match are offered
    expect(button("work/alerts/ci")).toBeTruthy();
    act(() => button("移动")!.click());
    await flush();
    const put = call("PUT", "/api/v1/inbox/mail:4/folder");
    expect(JSON.parse(put![1].body)).toEqual({ folder: "work/alerts" });
    expect(onClose).toHaveBeenCalled();

    act(() => root.render(<MoveFolderDialog key="2" item={{ ...items[0], folder_by: "model" }} onClose={onClose} />));
    await flush();
    act(() => button("交给 AI")!.click());
    await flush();
    expect(JSON.parse(call("PUT", "/api/v1/inbox/mail:1/folder")![1].body)).toEqual({ folder: null });
  });

  it("turns automatic folders on with the user's rules", async () => {
    const onClose = vi.fn();
    act(() => root.render(<FolderSettingsDialog onClose={onClose} />));
    await flush();
    const sw = document.querySelector<HTMLButtonElement>('[role="switch"]')!;
    expect(sw.getAttribute("aria-checked")).toBe("false");
    act(() => sw.click());
    const area = document.querySelector<HTMLTextAreaElement>("textarea")!;
    const setValue = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!;
    act(() => {
      setValue.call(area, "规则");
      area.dispatchEvent(new Event("input", { bubbles: true }));
    });
    act(() => button("保存")!.click());
    await flush();
    expect(JSON.parse(call("PUT", "/api/v1/inbox/folders")![1].body)).toEqual({ enabled: true, rules: "规则" });
    expect(onClose).toHaveBeenCalled();
  });
});
