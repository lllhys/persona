// @vitest-environment jsdom
// Right after a send, before the hub says the agent is working (awaiting, #3654), the run bar already says Working…: the
// composer offers Interrupt and Esc stops it too, as during the turn (#2450). With nothing on its way, Esc sends nothing.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Item, Session } from "../../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  g.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
});

const captured = vi.hoisted(() => ({
  session: { machine_id: "hub", session_id: "c1", title: "Conversation", cwd: "/code", status: "idle", state: "idle" } as Session,
}));
// one transcript object for every render, as the hook keeps one (a new one each time re-runs the echo checks forever)
const transcript = vi.hoisted(() => ({ items: [] as Item[], loading: false, error: "", hasMore: false, streaming: "" }));
vi.mock("./useTranscript", () => ({ useTranscript: () => transcript }));
vi.mock("../../lib/store", async (importOriginal) => {
  const store = await importOriginal<typeof import("../../lib/store")>();
  return {
    ...store,
    getSession: () => captured.session,
    useStore: <T,>(select: (s: ReturnType<typeof store.getState>) => T) => select({ ...store.getState(), sessions: { hub: [captured.session] } }),
  };
});
vi.mock("../../lib/panel", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/panel")>(), usePanel: () => null }));
vi.mock("../../lib/router", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/router")>(), useRoute: () => ({ name: "session", mid: "hub", id: "c1" }) }));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }) }));

const { ChatView } = await import("./ChatView");
const { t } = await import("../../i18n");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let fetcher: ReturnType<typeof vi.fn>;
const flush = async () => {
  for (let i = 0; i < 4; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const interrupts = () => fetcher.mock.calls.filter(([url]) => String(url).endsWith("/sessions/hub/c1/interrupt"));
const stopButton = () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.interrupt")}"]`);
const stopControl = () => host.querySelector(`button[aria-label="${t("chat-composer.interrupt")}"], button[aria-label="${t("chat-composer.stopping")}"]`);
const esc = () => act(() => void window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true })));

beforeEach(() => {
  localStorage.clear();
  fetcher = vi.fn(async () => new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } }));
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

describe("a message waiting for its turn to start (#2450)", () => {
  it("can be interrupted: the composer shows Interrupt, Esc sends it, and the wait ends there", async () => {
    transcript.items = [{ id: "u1", seq: 1, role: "user", kind: "text", text: "Write a status line", ts: new Date().toISOString() }];
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    expect(stopButton()).not.toBeNull();
    await esc();
    await flush();
    expect(interrupts()).toHaveLength(1);
    expect(interrupts()[0][1]).toMatchObject({ method: "POST" });
    // the hub took it: the wait ends now instead of running out, so no stop button (nor its "Stopping" form) stays
    expect(stopControl()).toBeNull();
  });

  it("with nothing on its way, Esc sends nothing and there is no Interrupt", async () => {
    transcript.items = [
      { id: "u1", seq: 1, role: "user", kind: "text", text: "Write a status line", ts: new Date(Date.now() - 60_000).toISOString() },
      { id: "a1", seq: 2, role: "assistant", kind: "text", text: "Done.", ts: new Date().toISOString() },
    ];
    act(() => root.render(<ChatView mid="hub" sid="c1" />));
    await flush();
    expect(stopButton()).toBeNull();
    await esc();
    await flush();
    expect(interrupts()).toHaveLength(0);
  });
});
