// @vitest-environment jsdom
// A link to a side session (#7944: `/s/<machine>/<side>`, its card, a notification, a search hit naming it) opens its
// parent's page, which is asked to show that side in its Side sessions panel; a link to a row of a side's own (its Codex
// child, `folded_side`) opens the parent too, the side shown and its chat asked for the child's card. Without
// `sessions_side_fold` a side's link opens the side itself, as before.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});
const drawn = vi.hoisted(() => ({ chatSid: "" }));
vi.mock("./views/Sidebar", () => ({ Sidebar: () => null, SidebarRail: () => null }));
vi.mock("./views/chat/ChatView", () => ({
  ChatView: ({ sid }: { sid: string }) => {
    drawn.chatSid = sid;
    return null;
  },
}));
vi.mock("./views/Palette", () => ({ Palette: () => null }));
vi.mock("./views/NewAgent", () => ({ NewAgent: () => null }));
vi.mock("./views/NewWorktreeHost", () => ({ NewWorktreeHost: () => null }));
vi.mock("./views/SidePanel", () => ({ SidePanel: () => null }));
vi.mock("./views/ShortcutHelp", () => ({ ShortcutHelp: () => null }));
vi.mock("./lib/store", async (load) => ({ ...(await load<typeof import("./lib/store")>()), connect: () => {}, disconnect: () => {} }));
import { refresh, reset } from "./lib/store";
import { setSideFold } from "./lib/side-fold";
import { useSideAsk, type SideAsk } from "./lib/side-panel";
import { childCall } from "./lib/child-cards";
import { useSubagentLink } from "./lib/subagent-link";
import { setToken } from "./lib/api";
import type { Machine, Session } from "./lib/types";
import { App } from "./App";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const machines: Machine[] = [{ id: "mac", name: "MacBook", online: true }];
const row = (session_id: string, extra: Partial<Session> = {}): Session => ({ machine_id: "mac", session_id, title: `Task ${session_id}`, cwd: "/code", status: "idle", state: "idle", ...extra });
const sessions: Session[] = [
  row("review", { children: { total: 2, sides: 1, needs_you: 1, sides_needs_you: 1 } }),
  row("btw", { folded_under: "review", folded_as: "side", parent_agent_id: "review", state: "needs_you", status: "blocked" }),
  row("kid", { folded_under: "review", folded_side: "btw", is_subagent: true, parent_session_id: "btw" }),
];
const answers: Record<string, unknown> = { "/api/v1/sessions": { sessions }, "/api/v1/machines": { machines } };

let host: HTMLDivElement;
let root: Root;
let asked: SideAsk | undefined;
let linked: string | undefined;
function Probe() {
  asked = useSideAsk("mac", "review");
  linked = useSubagentLink("mac", "btw");
  return null;
}
const probe = createRoot(document.createElement("div"));
const flush = async () => {
  for (let i = 0; i < 5; i++) await act(async () => { await Promise.resolve(); });
};
const open = async (path: string) => {
  history.replaceState(null, "", path);
  await act(() => refresh());
  act(() => root.render(<App />));
  await flush();
};

beforeEach(() => {
  localStorage.clear();
  setToken("tok");
  drawn.chatSid = "";
  vi.stubGlobal("fetch", vi.fn((url: string) => Promise.resolve(new Response(JSON.stringify(answers[new URL(url, "http://x").pathname] ?? {}), { headers: { "Content-Type": "application/json" } }))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => probe.render(<Probe />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  setSideFold(false);
  vi.unstubAllGlobals();
});

describe("a link to a side session", () => {
  it("opens its parent's page, which is asked to show the side", async () => {
    setSideFold(true);
    await open("/s/mac/btw");
    expect(location.pathname).toBe("/s/mac/review");
    expect(drawn.chatSid).toBe("review");
    expect(asked).toEqual({ side: "btw", at: undefined });
  });

  it("opens a row of a side's own at the side, its chat asked for the row's card", async () => {
    setSideFold(true);
    await open("/s/mac/kid");
    expect(location.pathname).toBe("/s/mac/review");
    expect(asked?.side).toBe("btw");
    expect(linked).toBe(childCall("kid"));
  });

  it("opens the side itself on a hub that does not fold sides", async () => {
    await open("/s/mac/btw");
    expect(location.pathname).toBe("/s/mac/btw");
    expect(drawn.chatSid).toBe("btw");
  });
});
