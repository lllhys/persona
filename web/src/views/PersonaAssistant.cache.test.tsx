// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setToken } from "../lib/api";
import { feedCacheScope, writeFeedCache } from "../lib/feed-cache";
import { chooseManager, resetManagerChoice } from "../lib/manager-choice";
import { getState, reset } from "../lib/store";
import { PersonaAssistant } from "./PersonaAssistant";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
vi.hoisted(() => {
  Object.assign(globalThis, {
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } },
  });
});
vi.mock("./chat/ChatView", () => ({
  ChatView: (p: { mid: string; sid: string; mainCache?: unknown; refreshError?: string; onRefresh?: () => void }) => <div data-chat={`${p.mid}/${p.sid}`} data-cache={Boolean(p.mainCache)}>{p.refreshError}<button onClick={p.onRefresh}>Reload</button></div>,
}));
vi.mock("./assistant/Readiness", () => ({
  useReadinessGate: () => ({ readiness: null }), ReadinessCard: () => null, ChatOnlyNote: () => null, FailureCard: () => null,
}));
let host: HTMLDivElement;
let root: Root;
const render = (props: { sid?: string; fresh?: boolean } = {}) => act(() => root.render(<PersonaAssistant {...props} onToggleSidebar={() => {}} />));
beforeEach(() => {
  localStorage.clear();
  resetManagerChoice();
  reset();
  setToken("test-credential");
  writeFeedCache(feedCacheScope()!, { machineID: "hub", sessionID: "cached-main", items: [{ id: "one", seq: 1, role: "user", kind: "text", text: "Last visit" }], hasMore: true });
  vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(() => { act(() => root.unmount()); host.remove(); reset(); resetManagerChoice(); vi.unstubAllGlobals(); });

it("renders the cached main conversation before any hub response", () => {
  render();
  expect(host.querySelector('[data-chat="hub/cached-main"]')?.getAttribute("data-cache")).toBe("true");
  expect(getState().assistant).toBeNull();
});

it("keeps the cached main on failure and retries the assistant read", async () => {
  Object.assign(getState(), { assistantError: "Unavailable" });
  render();
  expect(host.querySelector('[data-chat="hub/cached-main"]')).not.toBeNull();
  expect(host.textContent).toContain("Unavailable");
  vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(Response.json({ machine_id: "new-hub", main: "new-main", conversations: [] }))));
  await act(async () => host.querySelector("button")!.click());
  expect(host.querySelector('[data-chat="new-hub/new-main"]')).not.toBeNull();
});

it("uses the hub's main reference even while the other startup reads are pending", () => {
  Object.assign(getState(), { assistant: { machine_id: "new-hub", main: "new-main", conversations: [] } });
  render();
  expect(host.querySelector('[data-chat="new-hub/new-main"]')).not.toBeNull();
  Object.assign(getState(), { assistant: { machine_id: "hub", main: "", conversations: [] } });
  render();
  expect(host.querySelector('[data-chat]')).toBeNull();
});

it("loads an explicit conversation over HTTP before the socket reconnects", async () => {
  const fetcher = vi.fn((_input: RequestInfo | URL) => Promise.resolve(Response.json({ machine_id: "hub", conversations: [] })));
  vi.stubGlobal("fetch", fetcher);
  await act(async () => render({ sid: "restored-chat" }));
  expect(fetcher).toHaveBeenCalledTimes(1);
  expect(String(fetcher.mock.calls[0][0])).toBe("/api/v1/assistant");
  expect(host.querySelector('[data-chat="hub/restored-chat"]')).not.toBeNull();
  expect(getState().conn).not.toBe("online");
});

it("does not reuse the cache for another conversation, a new chat, or a different manager", () => {
  render({ sid: "other" });
  expect(host.querySelector('[data-chat]')).toBeNull();
  render({ fresh: true });
  expect(host.querySelector('[data-chat]')).toBeNull();
  act(() => chooseManager("other-manager"));
  render();
  expect(host.querySelector('[data-chat]')).toBeNull();
});

it("does not render cached content after logout", () => {
  setToken("");
  render();
  expect(host.querySelector('[data-chat]')).toBeNull();
});
