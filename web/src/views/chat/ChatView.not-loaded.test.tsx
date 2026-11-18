// @vitest-environment jsdom
// Opening a chat whose first page is refused as not_loaded (#8321): an agent Paseo let go is read only while its chat
// is watched (#8132), and this page reads its first page before it subscribes. The subscribe loads the agent and its
// backlog brings the page a second or three later. Until then the chat shows its usual loading state, never the
// refusal; the refusal shows only when nothing comes within a read's time. The real store, useTranscript and
// ChatView; the hub is the sync cases' (sync/fake-hub.ts), with its backlog sent when the test says.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FakeSocket, SyncHub, type SyncCase } from "./sync/fake-hub";
import type { Item } from "../../lib/types";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = (q: string) => ({ matches: /pointer: *fine|hover: *hover/.test(q), addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  g.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
});
vi.mock("../../lib/panel", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/panel")>(), usePanel: () => null }));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }), PersonaAvatar: () => null }));

const store = await import("../../lib/store");
const { setToken, READ_TIMEOUT_MS } = await import("../../lib/api");
const { t } = await import("../../i18n");
const { ChatView } = await import("./ChatView");
const { resetStalePullsForTests } = await import("./useTranscript");
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const MID = "computer-a";
const SID = "paseo-agent-1";
/** The hub's words for the refusal (rpc-errors.not_loaded). */
const REFUSAL = "This agent isn’t running in Paseo. Its conversation loads while its chat is open.";
const QUESTION = "Sort the holiday photos by place";
const ANSWER = "Done: 212 photos in 9 albums, one per town.";
const history: Item[] = [
  { id: "u1", seq: 1, ts: "2026-01-02T03:00:00Z", role: "user", kind: "text", text: QUESTION },
  { id: "a2", seq: 2, ts: "2026-01-02T03:01:00Z", role: "assistant", kind: "text", text: ANSWER },
];
const chat: SyncCase = {
  name: "not loaded", description: "", chat: { machine_id: MID, session_id: SID }, devices: ["web"],
  start: { hub: { history: "h1", lines: history, to_seq: 2, interactions: [], session: { state: "idle" }, event_stream: "s1", event_seq: 1 } },
  steps: [], end: {},
};

interface Refusal { status: number; body: Record<string, unknown> }
const NOT_LOADED: Refusal = { status: 409, body: { error: REFUSAL, code: "not_loaded" } };

/** The sync cases' hub, but its agent is not loaded: every read of the chat is refused with `refuse`, as the bridge
 *  refuses a read no watch asked for, until `sendBacklog` (the subscribe loaded the agent and the hub sends its backlog,
 *  a second or three after the subscribe) or `failBacklog` (the hub could not read it). */
class LoadingHub extends SyncHub {
  subscribes = 0;
  firstPages = 0;
  loaded = false;
  refuse: Refusal = NOT_LOADED;

  override fromPage(s: FakeSocket, raw: string) {
    const m = JSON.parse(raw) as Record<string, unknown>;
    if (s === this.socket && m.type === "subscribe" && m.session_id === SID) this.subscribes++;
  }

  sendBacklog() {
    this.loaded = true;
    this.push({ type: "items", machine_id: MID, session_id: SID, backlog: true, ...this.truth.latest(2) });
  }

  failBacklog(error: string) {
    this.push({ type: "error", machine_id: MID, session_id: SID, code: "internal", error });
  }

  /** The page's fetch: the chat's reads as the bridge answers them, everything else the sync hub's. */
  serve = async (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
    const url = String(input instanceof Request ? input.url : input);
    if (url.includes(`/sessions/${MID}/${SID}/items?`)) {
      if (url.includes("turns=2") && !url.includes("before_seq")) this.firstPages++;
      if (!this.loaded) return new Response(JSON.stringify(this.refuse.body), { status: this.refuse.status, headers: { "Content-Type": "application/json" } });
    }
    return this.fetch(input, init);
  };
}

let host: HTMLDivElement;
let root: Root;
let hub: LoadingHub;
/** Every text the page showed, as it changed: the refusal must never be one of them while the backlog is on its way. */
let shown: string[] = [];
let watcher: MutationObserver;

beforeEach(() => {
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date", "requestAnimationFrame", "cancelAnimationFrame"] });
  vi.setSystemTime(new Date("2026-01-02T03:05:00Z"));
  vi.stubGlobal("WebSocket", FakeSocket);
  setToken("tok");
  host = document.createElement("div");
  document.body.appendChild(host);
  shown = [];
  watcher = new MutationObserver(() => shown.push(host.textContent ?? ""));
  watcher.observe(host, { subtree: true, childList: true, characterData: true });
});

afterEach(async () => {
  watcher.disconnect();
  await act(async () => root?.unmount());
  host.remove();
  store.disconnect();
  store.reset();
  resetStalePullsForTests();
  FakeSocket.hub = null;
  vi.unstubAllGlobals();
  vi.useRealTimers();
  localStorage.clear();
});

/** `ms` of the page's clock, then a few frames (the store batches its notices per frame). */
async function settle(ms = 0) {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(i === 0 ? ms : 17);
    });
  }
}

async function open(refuse = NOT_LOADED) {
  hub = new LoadingHub(chat, "web");
  hub.refuse = refuse;
  FakeSocket.hub = hub;
  vi.stubGlobal("fetch", hub.serve);
  store.connect();
  await settle();
  root = createRoot(host);
  await act(async () => root.render(<ChatView mid={MID} sid={SID} />));
  await settle(100);
}

const text = () => host.textContent ?? "";
const loading = () => text().includes(t("chat-chatview.loading_chat"));
const retry = () => [...host.querySelectorAll("button")].find((b) => b.textContent?.includes(t("chat-chatview.retry")));

describe("ChatView: a first page refused as not_loaded", () => {
  it("keeps loading until the watch's backlog brings the history, never showing the refusal", async () => {
    await open();
    expect(hub.firstPages).toBe(1);
    expect(hub.subscribes, "the refusal still subscribes: that is what loads the agent").toBe(1);
    expect(loading()).toBe(true);
    // Paseo takes a while to load the agent: the chat keeps loading all along.
    await settle(2_500);
    expect(loading()).toBe(true);
    expect(retry()).toBeUndefined();
    await act(async () => hub.sendBacklog());
    await settle();
    expect(text()).toContain(QUESTION);
    expect(text()).toContain(ANSWER);
    expect(loading()).toBe(false);
    // nothing comes back after the read's time either
    await settle(READ_TIMEOUT_MS);
    expect(shown.filter((s) => s.includes(REFUSAL)), "the refusal was shown").toEqual([]);
    expect(text()).toContain(ANSWER);
  });

  it("keeps loading when the hub could not send the backlog either, and shows the refusal after a read's time", async () => {
    await open();
    await settle(1_000);
    // the hub's own failure for the session, which this page does not read: the wait runs its course
    await act(async () => hub.failBacklog("Paseo could not load this agent."));
    await settle(READ_TIMEOUT_MS - 2_000);
    expect(loading()).toBe(true);
    expect(text()).not.toContain(REFUSAL);
    await settle(1_000);
    expect(text()).toContain(REFUSAL);
    expect(loading()).toBe(false);
    expect(retry()).toBeDefined();
  });

  it("shows the refusal with Retry when nothing comes within a read's time, and the history if it comes later", async () => {
    await open();
    await settle(READ_TIMEOUT_MS - 1_000);
    expect(loading()).toBe(true);
    expect(text()).not.toContain(REFUSAL);
    await settle(1_000);
    expect(text()).toContain(REFUSAL);
    expect(loading()).toBe(false);
    expect(retry()).toBeDefined();
    // recoverable: a backlog that comes late still fills the chat and takes the refusal away
    await act(async () => hub.sendBacklog());
    await settle();
    expect(text()).toContain(ANSWER);
    expect(text()).not.toContain(REFUSAL);
  });

  it("shows any other refusal of the first page at once, as before", async () => {
    const away = "The engine on this computer is not connected.";
    await open({ status: 409, body: { error: away, code: "runtime_away" } });
    expect(text()).toContain(away);
    expect(loading()).toBe(false);
    expect(retry()).toBeDefined();
  });
});
