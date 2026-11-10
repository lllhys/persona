// @vitest-environment jsdom
// Another assistant's conversation opened while another one is current (docs/assistant/managers.md "Clients"): a hand-off
// from an inbox item, a link or the palette opens it as itself, on the computer it runs on, and the current assistant
// stays; the current one's offline line is not put on it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { setToken } from "../lib/api";
import { chooseManager, chosenManager, resetManagerChoice, setRosterDefault } from "../lib/manager-choice";
import { setRoster } from "../lib/managers";
import { adoptConversation } from "../lib/outbox";
import { getState, reset } from "../lib/store";
import type { Session } from "../lib/types";
import { PersonaAssistant, elsewhere } from "./PersonaAssistant";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
vi.hoisted(() => {
  Object.assign(globalThis, {
    matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }),
    ResizeObserver: class { observe() {} unobserve() {} disconnect() {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} takeRecords() { return []; } },
  });
});
vi.mock("./chat/ChatView", () => ({
  ChatView: (p: { mid: string; sid: string; offline?: string }) => <div data-chat={`${p.mid}/${p.sid}`} data-offline={p.offline ?? ""} />,
}));
vi.mock("./assistant/Readiness", () => ({
  useReadinessGate: () => ({ readiness: null }), ReadinessCard: () => null, ChatOnlyNote: () => null, FailureCard: () => null,
}));

let host: HTMLDivElement;
let root: Root;
const render = (sid: string) => act(() => root.render(<PersonaAssistant sid={sid} onToggleSidebar={() => {}} />));
const chat = () => host.querySelector("[data-chat]");
const conversation = (mid: string, sid: string, manager: string): Session => ({ machine_id: mid, session_id: sid, manager, assistant: true, state: "idle" } as Session);

beforeEach(() => {
  localStorage.clear();
  resetManagerChoice();
  reset();
  setToken("test-credential");
  setRosterDefault("assistant");
  setRoster({
    default: "assistant",
    managers: [
      { id: "assistant", name: "Persona", online: true },
      { id: "m-work", name: "Work", online: true },
      { id: "m-home", name: "Home", online: false, last_seen: new Date(Date.now() - 3_600_000).toISOString() },
    ],
  });
  vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
  reset();
  resetManagerChoice();
  vi.unstubAllGlobals();
});

it("opens a conversation adopted for another assistant on its own computer, the current one staying", () => {
  Object.assign(getState(), { assistant: { machine_id: "hub", main: "main-a", conversations: [] } });
  adoptConversation("m-work", "work-chat", "macbook");
  render("work-chat");
  expect(chat()?.getAttribute("data-chat")).toBe("macbook/work-chat");
  expect(chosenManager()).toBeNull();
});

it("opens one the hub lists under another assistant on its computer (a link, the palette)", () => {
  Object.assign(getState(), { assistant: { machine_id: "hub", main: "main-a", conversations: [] }, sessions: { macbook: [conversation("macbook", "listed", "m-work")] } });
  render("listed");
  expect(chat()?.getAttribute("data-chat")).toBe("macbook/listed");
});

it("does not put the current assistant's offline line on another assistant's conversation", () => {
  act(() => chooseManager("m-home"));
  Object.assign(getState(), { assistant: null, assistantError: "offline" });
  adoptConversation(undefined, "main-a", "hub");
  render("main-a");
  expect(chat()?.getAttribute("data-chat")).toBe("hub/main-a");
  expect(chat()?.getAttribute("data-offline")).toBe("");
  expect(chosenManager()).toBe("m-home");
});

it("keeps the current assistant's own conversations on its computer", () => {
  Object.assign(getState(), { assistant: { machine_id: "hub", main: "main-a", conversations: [{ session_id: "mine" }] } });
  adoptConversation(undefined, "mine", "elsewhere");
  render("mine");
  expect(chat()?.getAttribute("data-chat")).toBe("hub/mine");
  // listed as the current one's, or adopted for it: here
  expect(elsewhere("mine", "assistant", getState().assistant, {})).toBe("");
  expect(elsewhere("unknown", "assistant", getState().assistant, {})).toBe("");
  expect(elsewhere("x", "assistant", null, { mac: [conversation("mac", "x", "assistant")] })).toBe("");
});
