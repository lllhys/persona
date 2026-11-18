// @vitest-environment jsdom
// A send refused because the computer's input box holds unsent text (409 `input_draft`, #6409): the composer emptied
// at the send and the words wait in the chat as a failed bubble, so "Clear and send" retries that bubble and never
// leaves the same words in the web box, where Enter would send them twice. What was typed while the dialog was open
// stays in the box, and a retry that fails again keeps the words in the failed bubble.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Item, Session } from "../../lib/types";
import { setQuotes } from "../../lib/drafts";
import { storedValue } from "../../lib/compatibility";
import { outboxEntry } from "../../lib/outbox";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
  g.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} };
  g.IntersectionObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } };
});

const captured = vi.hoisted(() => ({
  session: { machine_id: "hub", session_id: "c1", title: "Conversation", cwd: "/code", status: "idle", state: "idle" } as Session,
}));
// one transcript object for every render, as the hook keeps one
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

const DRAFT_KEY = "draft:hub/c1";
const TERMINAL_DRAFT = "half-typed at the computer";

let host: HTMLDivElement;
let root: Root;
let fetcher: ReturnType<typeof vi.fn>;
const flush = async () => {
  for (let i = 0; i < 4; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
};
const box = () => host.querySelector<HTMLTextAreaElement>("textarea")!;
const typeInto = (text: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(box(), text);
    box().dispatchEvent(new Event("input", { bubbles: true }));
  });
const sendButton = () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.send")}"]`)!;
const pressEnter = () => act(() => void box().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true })));
// open, not still leaving: a closing dialog keeps its body for the exit animation
const openDialog = () => document.body.querySelector<HTMLElement>('[role="dialog"][aria-modal="true"]');
const dialogOpen = () => openDialog() !== null;
const buttonIn = (scope: ParentNode | null, label: string) => [...(scope?.querySelectorAll<HTMLButtonElement>("button") ?? [])].find((b) => b.textContent === label);
// the dialog's button; the failed bubble in the chat offers its own (OutboxBubbles)
const clearAndSend = () => buttonIn(openDialog(), t("chat-chatview.clear_and_send"));
const bubbleClearAndSend = () => buttonIn(host, t("chat-outboxbubbles.clear_and_send"));
const posts = () => fetcher.mock.calls.filter(([url]) => String(url).endsWith("/sessions/hub/c1/messages")).map(([, init]) => JSON.parse(String((init as RequestInit).body)));
// past the composer's 250 ms debounce that saves what is typed
const saved = () => act(async () => { await new Promise((r) => setTimeout(r, 300)); });

const refused = () =>
  new Response(JSON.stringify({ error: "There is a draft in the terminal.", code: "input_draft", delivered: "none", draft: TERMINAL_DRAFT, clearable: TERMINAL_DRAFT }), { status: 409, headers: { "Content-Type": "application/json" } });
const ok = () => new Response(JSON.stringify({ ok: true }), { status: 200, headers: { "Content-Type": "application/json" } });

/** The hub's answers to the conversation's sends, in order; anything else answers `{}`. */
const answering = (...replies: (() => Response)[]) =>
  fetcher.mockImplementation(async (url) => {
    if (String(url).endsWith("/sessions/hub/c1/messages")) return (replies.shift() ?? ok)();
    return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
  });

/** Types `text`, sends it, and waits for the refusal's dialog. */
const sendRefused = async (text: string) => {
  act(() => root.render(<ChatView mid="hub" sid="c1" />));
  await flush();
  typeInto(text);
  act(() => sendButton().click());
  await flush();
  expect(posts()).toHaveLength(1);
  expect(dialogOpen()).toBe(true);
  expect(clearAndSend()).toBeDefined();
};

beforeEach(() => {
  localStorage.clear();
  setQuotes("hub/c1", []);
  captured.session = { machine_id: "hub", session_id: "c1", title: "Conversation", cwd: "/code", status: "idle", state: "idle", agent: "claude" } as Session;
  transcript.items = [];
  fetcher = vi.fn();
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

describe("Clear and send after the computer's box refused a send (input_draft, #6409)", () => {
  it.each(["dialog", "failed bubble"])("sends once from the %s, clearing the computer's draft, and leaves the web box and its saved draft empty", async (from) => {
    answering(refused, ok);
    await sendRefused("hello");
    // the refused words wait in the chat, not in the box
    expect(box().value).toBe("");
    expect(storedValue(DRAFT_KEY)).toBeNull();
    expect(bubbleClearAndSend()).toBeDefined();

    if (from === "failed bubble") {
      // "Not now" first: the bubble's own button is what is left
      await act(async () => buttonIn(openDialog(), t("chat-chatview.not_now"))!.click());
      await flush();
      expect(dialogOpen()).toBe(false);
      await act(async () => bubbleClearAndSend()!.click());
    } else await act(async () => clearAndSend()!.click());
    await flush();
    await saved();

    const [first, retry] = posts();
    expect(posts()).toHaveLength(2);
    expect(retry.text).toBe("hello");
    expect(retry.clear_draft).toBe(TERMINAL_DRAFT);
    // the hub answered the refused one: the retry is another operation
    expect(retry.client_msg_id).not.toBe(first.client_msg_id);
    expect(outboxEntry("hub", "c1", retry.client_msg_id)?.state).toBe("sent");
    expect(dialogOpen()).toBe(false);
    expect(box().value).toBe("");
    expect(storedValue(DRAFT_KEY)).toBeNull();

    // Enter in the box now has nothing to send: no second copy
    pressEnter();
    await flush();
    expect(posts()).toHaveLength(2);
  });

  it("keeps what was typed in the box while the dialog was open", async () => {
    answering(refused, ok);
    await sendRefused("hello");
    typeInto("a follow-up I am still writing");
    await saved();

    await act(async () => clearAndSend()!.click());
    await flush();
    await saved();

    expect(posts()).toHaveLength(2);
    expect(posts()[1].text).toBe("hello");
    expect(box().value).toBe("a follow-up I am still writing");
    expect(storedValue(DRAFT_KEY)).toBe("a follow-up I am still writing");
  });

  it("keeps the words when the retry fails too: a failed bubble with Retry, never put back for a second send", async () => {
    answering(refused, () => new Response(JSON.stringify({ error: "The computer is offline.", code: "machine_offline", delivered: "none" }), { status: 502, headers: { "Content-Type": "application/json" } }));
    await sendRefused("hello");

    await act(async () => clearAndSend()!.click());
    await flush();

    const retry = posts()[1];
    expect(posts()).toHaveLength(2);
    const kept = outboxEntry("hub", "c1", retry.client_msg_id);
    expect(kept?.state).toBe("failed");
    expect(kept?.text).toBe("hello");
    expect(host.textContent).toContain("hello");
    expect(box().value).toBe("");
    pressEnter();
    await flush();
    expect(posts()).toHaveLength(2);
  });
});
