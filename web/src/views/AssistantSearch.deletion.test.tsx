// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { History } from "./assistant/History";
import { Palette } from "./Palette";
import { getState, markConversationDeleted, reset } from "../lib/store";
import { ApiError, setToken } from "../lib/api";
import { chooseManager, resetManagerChoice } from "../lib/manager-choice";
import { closePalette, openPalette, setPaletteQuery } from "../lib/palette";
import { navigate, onLinkClick } from "../lib/router";
import { SEARCH_SETTLE_MS, searchAssistantChats, type AssistantSearchAnswer } from "../lib/assistant-search";

vi.hoisted(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true, matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
const calls = vi.hoisted(() => ({ archived: null as Promise<unknown> | null, reads: [] as { resolve: (a: unknown) => void; reject: (e: unknown) => void }[] }));
vi.mock("../lib/api", async (original) => ({
  ...(await original<typeof import("../lib/api")>()),
  api: vi.fn(async (path: string) => {
    if (path.startsWith("/assistant/search?")) return new Promise((resolve, reject) => calls.reads.push({ resolve, reject }));
    if (path === "/assistant?archived=1") { const pending = calls.archived; calls.archived = null; return pending ?? { conversations: [] }; }
    if (path === "/assistant") return { machine_id: "server", main: "main", conversations: [] };
    return {};
  }),
}));
vi.mock("../lib/router", async (original) => ({ ...(await original<typeof import("../lib/router")>()), navigate: vi.fn(), onLinkClick: vi.fn((event: MouseEvent) => event.preventDefault()) }));
let host: HTMLDivElement, root: Root, owner = 0;
const answer = (): AssistantSearchAnswer => ({ partial: false, results: [
  { machine_id: "server", session_id: "private", title: "Private archived title", archived: true, hits: [{ item_id: "private-item", seq: 1, role: "user", snippet: "needle Private snippet", ranges: [[0, 6]] }] },
  { machine_id: "server", session_id: "kept", title: "Kept chat", hits: [{ item_id: "kept-item", seq: 2, role: "assistant", snippet: "needle Kept snippet", ranges: [[0, 6]] }] },
] });
const flush = async () => { await act(async () => vi.dynamicImportSettled()); };
const mount = async (surface: "history" | "palette") => {
  if (surface === "palette") { openPalette("jump"); setPaletteQuery("needle"); }
  await act(async () => root.render(surface === "palette" ? <Palette open onClose={() => {}} onNewAgent={() => {}} /> : <History onToggleSidebar={() => {}} />));
  await flush();
  if (surface === "history") await act(async () => {
    const input = host.querySelector("input")!;
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, "needle");
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
  await act(async () => { await vi.advanceTimersByTimeAsync(SEARCH_SETTLE_MS); }); await flush();
  expect(calls.reads).toHaveLength(1);
};
const noPrivate = () => { expect(document.body.textContent).not.toContain("Private archived title"); expect(document.body.textContent).not.toContain("Private snippet"); };
const changeOwner = (kind: string) => {
  if (kind === "reset") reset();
  else if (kind === "manager") chooseManager("other-manager");
  else if (kind === "storage") { localStorage.setItem("persona.token", "other-owner"); window.dispatchEvent(new StorageEvent("storage", { key: "persona.token" })); }
  else setToken("other-owner");
};
beforeEach(() => {
  reset(); resetManagerChoice(); localStorage.clear(); setToken(`search-owner-${++owner}`); calls.reads = []; calls.archived = null;
  getState().features = ["assistant_search"];
  getState().assistant = { machine_id: "server", main: "main", conversations: [] };
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { value: () => {}, configurable: true });
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] }); vi.mocked(navigate).mockClear(); vi.mocked(onLinkClick).mockClear();
});
afterEach(() => { act(() => root.unmount()); host.remove(); closePalette(); reset(); resetManagerChoice(); vi.useRealTimers(); });
for (const surface of ["history", "palette"] as const) describe(surface, () => {
  it("removes an open search result and refuses its stale navigation", async () => {
    await mount(surface); await act(async () => calls.reads[0].resolve(answer())); await flush();
    expect(document.body.textContent).toContain("Private snippet");
    const row = [...document.querySelectorAll<HTMLElement>(surface === "history" ? "a" : '[role="option"]')].find((e) => e.textContent?.includes("Private archived title"))!;
    expect(row).toBeDefined();
    await act(async () => { markConversationDeleted("server", "private"); row.click(); }); await flush();
    noPrivate(); expect(document.body.textContent).toContain("Kept snippet");
    expect(navigate).not.toHaveBeenCalled(); expect(onLinkClick).not.toHaveBeenCalled();
  });
  it("cannot resurrect a deleted result from a late HTTP answer", async () => {
    await mount(surface); await act(async () => markConversationDeleted("server", "private"));
    await act(async () => calls.reads[0].resolve(answer())); await flush();
    noPrivate(); expect(document.body.textContent).toContain("Kept snippet");
  });
  it.each(["credential", "storage", "reset", "manager"])("hides an open result when %s changes", async (kind) => {
    await mount(surface); await act(async () => calls.reads[0].resolve(answer())); await flush();
    expect(document.body.textContent).toContain("Private snippet");
    await act(async () => changeOwner(kind)); await flush(); noPrivate();
  });
  it.each(["credential", "reset", "manager"])("drops an old response after %s changes", async (kind) => {
    await mount(surface); await act(async () => changeOwner(kind));
    await act(async () => calls.reads[0].resolve(answer())); await flush(); noPrivate();
  });
});

it.each(["credential", "reset"])("does not let an old 404 poison search support after %s changes", async (kind) => {
  const old = searchAssistantChats("needle");
  expect(calls.reads).toHaveLength(1);
  changeOwner(kind);
  calls.reads[0].reject(new ApiError(404, "Unsupported", {}));
  await old;
  const fresh = searchAssistantChats("needle");
  expect(calls.reads).toHaveLength(2);
  calls.reads[1].resolve(answer());
  expect((await fresh)?.results).toHaveLength(2);
});

it.each(["credential", "reset", "manager"])("History discards an archived-list response after %s changes", async (kind) => {
  let release!: (value: unknown) => void;
  calls.archived = new Promise((done) => { release = done; });
  await mount("history");
  await act(async () => changeOwner(kind));
  await act(async () => release({ conversations: [{ machine_id: "server", session_id: "private", title: "Private archived title", last_text: "Private snippet", assistant: true, status: "idle", archived: true }] }));
  await flush(); noPrivate();
});
