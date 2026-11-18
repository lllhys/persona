// @vitest-environment jsdom
// The Agents-page composer (views/agents/AgentsView.tsx AgentComposer) keeps its words in the shared draft store
// (lib/drafts.ts): typing is stored, a box that unmounts (navigation) or a page reload gets the words back, and a
// successful send removes them. Its key is not one the conversation sidebar's draft indicator reads.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

import { AgentComposer } from "./AgentsView";
import { draftListenerKeysForTests, storeDraft, useLeftDraft } from "../../lib/drafts";
import { draftLine } from "../../lib/row-line";
import { t } from "../../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const AGENT_ID = "lark:a";
const STORED_KEY = `draft:agent:${AGENT_ID}`;
// the key shape a conversation sidebar row actually asks the draft indicator for (`<machine>/<session>`)
const SIDEBAR_KEY = "hub/main";

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const mount = (onSend: (text: string) => void = vi.fn(), disabled = false) =>
  act(async () => root.render(<AgentComposer id={AGENT_ID} disabled={disabled} reason="" onSend={onSend} />));

const area = () => host.querySelector<HTMLTextAreaElement>("textarea")!;
const type = (value: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(area(), value);
    area().dispatchEvent(new Event("input", { bubbles: true }));
  });
const pressEnter = () =>
  act(async () => {
    area().dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
  });
const sendButton = () =>
  [...host.querySelectorAll("button")].find((b) => b.getAttribute("aria-label") === t("agents-agentsview.send"))!;

// Reads the sidebar draft indicator the way a row does, outside the composer's own React tree.
let leftDraft: { text: string; quoted: boolean } = { text: "", quoted: false };
function DraftProbe({ k }: { k: string | null }) {
  leftDraft = useLeftDraft(k);
  return null;
}

describe("the Agents-page composer draft", () => {
  it("stores words while typing and restores them after the box unmounts and comes back", async () => {
    await mount();
    await type("half-written instructions");
    expect(localStorage.getItem(STORED_KEY)).toBe("half-written instructions");

    act(() => root.unmount());
    root = createRoot(host);
    await mount();
    expect(area().value).toBe("half-written instructions");
  });

  it("restores the stored words on a fresh mount, as it does after a reload", async () => {
    localStorage.setItem(STORED_KEY, "still here after reload");
    await mount();
    expect(area().value).toBe("still here after reload");
  });

  it("trims the sent words, empties the box and removes the stored draft on send", async () => {
    const onSend = vi.fn();
    await mount(onSend);
    await type("  send me  ");
    await act(async () => sendButton().click());
    expect(onSend).toHaveBeenCalledExactlyOnceWith("send me");
    expect(area().value).toBe("");
    expect(localStorage.getItem(STORED_KEY)).toBeNull();

    // a box mounted afterwards starts empty: the send cleared the draft, not just the box
    act(() => root.unmount());
    root = createRoot(host);
    await mount();
    expect(area().value).toBe("");
  });

  it("keeps the trim guard: whitespace alone never sends and stays blocked", async () => {
    const onSend = vi.fn();
    await mount(onSend);
    await type("   ");
    expect(sendButton().disabled).toBe(true);
    await pressEnter();
    expect(onSend).not.toHaveBeenCalled();
  });

  it("registers no conversation draft listener, so the sidebar indicator cannot pick its key", async () => {
    const before = draftListenerKeysForTests();
    await mount();
    await type("unsent words");
    expect(draftListenerKeysForTests()).toBe(before);

    // the box goes away: the state in which a sidebar row would show a conversation's leftover draft
    act(() => root.unmount());
    const probeHost = document.createElement("div");
    document.body.append(probeHost);
    const probeRoot = createRoot(probeHost);
    try {
      act(() => probeRoot.render(<DraftProbe k={SIDEBAR_KEY} />));
      expect(leftDraft).toEqual({ text: "", quoted: false });
      expect(draftLine(leftDraft)).toBeUndefined();

      // positive control: a real conversation draft still reaches the indicator through the same hook
      act(() => storeDraft(SIDEBAR_KEY, "conversation words"));
      expect(leftDraft.text).toBe("conversation words");
      expect(draftLine(leftDraft)?.text).toContain("conversation words");
    } finally {
      act(() => probeRoot.unmount());
      probeHost.remove();
      localStorage.removeItem(`draft:${SIDEBAR_KEY}`);
    }
  });
});
