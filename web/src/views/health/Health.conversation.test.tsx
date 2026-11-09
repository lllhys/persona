// @vitest-environment jsdom
// One health conversation (docs/assistant/health.md) runs on the machine its space's list names: a list that can't be read says
// why with Retry instead of loading for ever, and is read again on the next hello.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({ listeners: new Map<string, (() => void)[]>(), answers: [] as (() => Promise<unknown>)[], started: 0 }));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string) => (path.startsWith("/assistant?space=health") && hub.answers.length ? hub.answers.shift()!() : Promise.resolve({})),
}));
vi.mock("../../lib/store", async (orig) => ({
  ...(await orig<typeof import("../../lib/store")>()),
  onHubEvent: (type: string, l: () => void) => {
    hub.listeners.set(type, [...(hub.listeners.get(type) ?? []), l]);
    return () => hub.listeners.set(type, (hub.listeners.get(type) ?? []).filter((x) => x !== l));
  },
}));
vi.mock("./reports", async (orig) => ({
  ...(await orig<typeof import("./reports")>()),
  startHealthConversation: () => (hub.started++, new Promise(() => {})),
}));
vi.mock("../chat/ChatView", () => ({ ChatView: ({ mid, sid }: { mid: string; sid: string }) => <p data-chat>{`${mid}/${sid}`}</p> }));

import { Health } from "./Health";
import { t } from "../../i18n";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};
const failing = () => Promise.reject(new Error("hub away"));
const listed = () => Promise.resolve({ machine_id: "mac", conversations: [] });

beforeEach(() => {
  hub.listeners.clear();
  hub.answers = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("the health page's box", () => {
  it("takes no second message while a conversation is starting", async () => {
    hub.answers.push(listed);
    localStorage.setItem("draft:health-new", "first question");
    act(() => root.render(<Health onToggleSidebar={() => {}} />));
    await flush();
    const send = host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.send")}"]`)!;
    await act(async () => send.click());
    await flush();
    expect(hub.started).toBe(1);
    expect(host.querySelector("textarea")!.disabled).toBe(true);
  });
});

describe("a health conversation", () => {
  it("says why when its space's list can't be read, and opens on Retry", async () => {
    hub.answers.push(failing, listed);
    act(() => root.render(<Health sid="h1" onToggleSidebar={() => {}} />));
    await flush();
    expect(host.querySelector("[role=alert]")?.textContent).toContain("hub away");
    const retry = [...host.querySelectorAll("button")].find((b) => b.textContent?.includes("重试"))!;
    await act(async () => retry.click());
    await flush();
    expect(host.querySelector("[data-chat]")?.textContent).toBe("mac/h1");
  });

  it("reads the list again on the next hello", async () => {
    hub.answers.push(failing, listed);
    act(() => root.render(<Health sid="h1" onToggleSidebar={() => {}} />));
    await flush();
    expect(host.querySelector("[data-chat]")).toBeNull();
    act(() => hub.listeners.get("hello")?.forEach((l) => l()));
    await flush();
    expect(host.querySelector("[data-chat]")?.textContent).toBe("mac/h1");
  });
});
