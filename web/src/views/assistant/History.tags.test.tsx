// @vitest-environment jsdom
// User-defined conversation tags (issue #9719): the in-use tag chips narrow the list, every row shows its labels, and
// the "Edit tags" dialog replaces the whole set with one PUT — shown at once from the optimistic overlay, rolled back
// when the hub refuses, with the hub's length and count limits refused in the dialog before any request.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Session } from "../../lib/types";

const hub = vi.hoisted(() => ({
  fetched: [] as Session[],
  state: { assistant: null as unknown, sessions: {} as Record<string, unknown[]> },
  listeners: new Set<() => void>(),
  puts: [] as { path: string; tags: string[] }[],
  /** When set, the hub is unreachable: every request fails, the tag PUT and the reload after it. */
  down: false,
}));
vi.mock("../../lib/api", async (original) => {
  const real = await original<typeof import("../../lib/api")>();
  return {
    ...real,
    api: vi.fn(async (path: string, init?: { method?: string; json?: unknown }) => {
      if (path === "/assistant?archived=1") {
        if (hub.down) throw new real.ApiError(0, "offline", {});
        return { conversations: hub.fetched };
      }
      if (init?.method === "PUT" && path.endsWith("/tags")) {
        hub.puts.push({ path, tags: (init.json as { tags: string[] }).tags }); // record first: a refused write still happened
        if (hub.down) throw new real.ApiError(0, "offline", {});
        return { ok: true, changed: true };
      }
      throw new real.ApiError(404, "not found", {});
    }),
  };
});
vi.mock("../../lib/store", async (original) => {
  const real = await original<typeof import("../../lib/store")>();
  const { useSyncExternalStore } = await import("react");
  return {
    ...real,
    refreshAssistant: async () => {},
    useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T): T =>
      useSyncExternalStore(
        (l) => (hub.listeners.add(l), () => void hub.listeners.delete(l)),
        () => select({ ...real.getState(), ...hub.state } as ReturnType<typeof real.getState>),
      ),
  };
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { History } from "./History";
import { toast } from "../../ui/atoms";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.matchMedia = (q: string) => ({ matches: q.includes("min-width"), addEventListener() {}, removeEventListener() {} });
});

const conv = (sid: string, p: Partial<Session> = {}): Session => ({
  machine_id: "server", session_id: sid, title: `对话 ${sid}`, cwd: "/home/me/.persona/assistant", status: "idle", assistant: true,
  updated_at: "2026-11-01T09:00:00Z", ...p,
} as Session);

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const mount = async () => {
  root = createRoot(host);
  act(() => root.render(<History onToggleSidebar={() => {}} />));
  await flush();
};
/** The session ids of the chat rows currently on the page, in page order. The main row's title is "Persona", so rows
 *  are identified by their /persona/<id> href rather than their text. */
const rowAnchor = (sid: string) =>
  [...host.querySelectorAll("a")].find((a) => a.getAttribute("href")?.includes(`/persona/${sid}`));
const shownIds = () =>
  [...host.querySelectorAll("a[href*='/persona/']")]
    .map((a) => /\/persona\/([^?]+)/.exec(a.getAttribute("href") ?? "")?.[1])
    .filter((x): x is string => !!x)
    .map((x) => decodeURIComponent(x));
const rowTags = (sid: string) => rowAnchor(sid)?.querySelector("[data-testid='row-tags']")?.textContent ?? "";
const chipGroup = () => host.querySelector('[aria-label="按标签筛选"]');
const chip = (label: string) =>
  [...(chipGroup()?.querySelectorAll("button") ?? [])].find((b) => b.textContent?.includes(label)) as HTMLButtonElement | undefined;
/** Opens `label` in the row menu of the chat titled `对话 <sid>`. */
const choose = async (sid: string, label: string) => {
  const row = rowAnchor(sid)!.parentElement!;
  await act(async () => (row.querySelector('button[aria-label="更多"]') as HTMLButtonElement).click());
  const item = [...document.querySelectorAll('[role="menuitem"], button')].find((b) => b.textContent?.trim() === label) as HTMLElement;
  await act(async () => item.click());
  await flush();
};
const dialog = () => document.querySelector('[role="dialog"]') as HTMLElement;
const tagInput = () => dialog().querySelector("input") as HTMLInputElement;
const setDraft = (value: string) => {
  const input = tagInput();
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, value);
  input.dispatchEvent(new Event("input", { bubbles: true }));
};
const enterTag = async (value: string) => {
  setDraft(value);
  act(() => tagInput().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true })));
  await flush();
};
const dialogTags = () => [...dialog().querySelectorAll("li")].map((li) => li.textContent ?? "");
const dialogError = () => dialog().querySelector('[role="alert"]')?.textContent ?? "";
const saveButton = () => [...dialog().querySelectorAll("button")].find((b) => b.textContent?.trim() === "保存") as HTMLButtonElement;

beforeEach(async () => {
  hub.puts = [];
  hub.down = false;
  hub.fetched = [
    conv("main"),
    conv("alpha", { tags: ["trip"] }),
    conv("beta", { tags: ["trip", "planning"] }),
    conv("gamma"),
  ];
  hub.state = {
    assistant: { machine_id: "server", main: "main", conversations: hub.fetched },
    sessions: { server: hub.fetched },
  };
  host = document.createElement("div");
  document.body.append(host);
  await mount();
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("全部对话的标签", () => {
  it("shows one chip per tag in use and each row's labels", () => {
    expect([...(chipGroup()?.querySelectorAll("button") ?? [])].map((b) => b.textContent?.trim())).toEqual(["planning", "trip"]);
    expect(rowTags("alpha")).toContain("trip");
    expect(rowTags("beta")).toContain("trip");
    expect(rowTags("beta")).toContain("planning");
    expect(rowTags("main")).toBe("");
  });

  it("narrows the list to chats carrying the chip's tag, and toggles back", async () => {
    expect(shownIds()).toEqual(["main", "alpha", "beta", "gamma"]);
    await act(async () => chip("trip")!.click());
    await flush();
    expect(shownIds()).toEqual(["alpha", "beta"]);
    expect(chip("trip")?.getAttribute("aria-pressed")).toBe("true");
    await act(async () => chip("trip")!.click());
    await flush();
    expect(shownIds()).toEqual(["main", "alpha", "beta", "gamma"]);
  });

  it("replaces the whole set with one PUT and shows the new tag before the reload settles", async () => {
    await choose("alpha", "编辑标签…");
    expect(dialogTags()).toEqual(["trip"]);
    expect(saveButton().disabled).toBe(true); // nothing changed yet
    await enterTag("new-tag");
    expect(dialogTags().join("")).toContain("new-tag");
    await act(async () => saveButton().click());
    await flush();
    expect(hub.puts).toEqual([{ path: "/assistant/conversations/alpha/tags", tags: ["trip", "new-tag"] }]);
    expect(rowTags("alpha")).toContain("new-tag"); // the optimistic overlay survives the re-read that still lacks it
    expect(toast).toHaveBeenLastCalledWith("已保存「对话 alpha」的标签", "ok");
    expect(dialog()).toBeNull(); // a successful save closes the dialog
  });

  it("collapses a duplicate and refuses the hub's length and count limits before any request", async () => {
    await choose("gamma", "编辑标签…");
    await enterTag("trip");
    await enterTag("trip"); // a repeat just collapses, as the hub's normalization would
    expect(dialogTags()).toEqual(["trip"]);
    expect(dialogError()).toBe("");
    for (let i = 1; i <= 9; i++) await enterTag(`t${String(i).padStart(2, "0")}`);
    expect(dialogTags()).toHaveLength(10);
    await enterTag("t10");
    expect(dialogError()).toContain("10");
    expect(hub.puts).toEqual([]);
    await enterTag("好".repeat(33));
    expect(dialogError()).toContain("32");
    expect(hub.puts).toEqual([]);
  });

  it("rolls the optimistic change back and keeps the dialog open when the hub refuses the save", async () => {
    hub.down = true;
    await choose("alpha", "编辑标签…");
    await enterTag("x");
    await act(async () => saveButton().click());
    await flush();
    expect(hub.puts).toEqual([{ path: "/assistant/conversations/alpha/tags", tags: ["trip", "x"] }]);
    expect(dialog()).not.toBeNull(); // onSave closes only after the hub took it
    expect(rowTags("alpha")).toBe("trip");
    expect(toast).toHaveBeenLastCalledWith("offline", "error");
  });
});
