// @vitest-environment jsdom
// Back from an Inbox item (#3329): a row opened from the list remembers the list under it, so ‹ and Done on the item go
// back to that entry, as the browser's Back would, instead of stacking another copy of the list on top of the item.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

const calls = vi.hoisted(() => ({ items: [] as unknown[] }));
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string) => {
    if (path.startsWith("/inbox?")) return Promise.resolve({ items: calls.items });
    if (path === "/mail/accounts") return Promise.resolve({ accounts: [] });
    return Promise.resolve({});
  },
}));
vi.mock("../../lib/store", async (orig) => {
  const m = await orig<typeof import("../../lib/store")>();
  return { ...m, useStore: <T,>(sel: (s: ReturnType<typeof m.getState>) => T) => m.useStore((s) => sel({ ...s, loaded: true })) };
});
vi.mock("../../lib/extension-slots", () => ({ useInboxItemPlace: () => undefined }));
vi.mock("../../lib/translate", () => ({ useTranslatePrefs: () => ({ enabled: false }), hubTranslation: () => undefined }));
vi.mock("./LarkThread", () => ({ LarkThread: () => null, chatOfItem: () => ({}) }));

import { resetInboxForTests, type InboxItem } from "../../lib/inbox";
import { stepsToInbox } from "../../lib/inbox-nav";
import { settleRosterDefault } from "../../lib/manager-choice";
import { Inbox } from "./Inbox";

settleRosterDefault();
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const item = (id: string): InboxItem => ({
  id, source: "lark", kind: "group", account: "feishu", account_name: "Lark", title: `chat ${id}`, preview: "text", count: 1,
  updated_at: "2026-11-09T04:00:00Z", tier: "action", version: `v-${id}`, lark: { feed_id: id, is_group: true },
});

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
/** Runs `go`, then waits for the Back it causes (history.go() is asynchronous). */
const popped = (go: () => void) =>
  new Promise<void>((done, fail) => {
    const timer = setTimeout(() => fail(new Error("no Back happened")), 1_000);
    addEventListener("popstate", () => (clearTimeout(timer), done()), { once: true });
    go();
  });
/** The item page, as the app draws it once the address is the item's. */
const openItem = async (id: string) => {
  act(() => root.render(<Inbox id={id} onToggleSidebar={() => {}} />));
  await flush();
};
const rowLink = (id: string) => host.querySelector<HTMLAnchorElement>(`[data-item="${id}"] a`)!;

beforeEach(async () => {
  calls.items = [item("a1"), item("a2")];
  resetInboxForTests();
  vi.stubGlobal("CSS", { escape: (v: string) => v });
  Element.prototype.scrollIntoView = vi.fn();
  history.pushState(null, "", "/inbox");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<Inbox onToggleSidebar={() => {}} />));
  await flush();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  history.replaceState(null, "", "/");
});

describe("Back from an Inbox item (#3329)", () => {
  it("‹ on an item opened from the list goes back to the list's entry", async () => {
    const start = history.length;
    act(() => rowLink("a2").click());
    expect(location.pathname).toBe("/inbox/a2");
    expect(stepsToInbox(history.state)).toBe(1);
    await openItem("a2");
    // the other row, beside the item on a wide screen: one more step from the list
    act(() => rowLink("a1").click());
    expect(stepsToInbox(history.state)).toBe(2);
    await openItem("a1");
    await popped(() => host.querySelector<HTMLButtonElement>('button[aria-label="返回收件箱"]')!.click());
    expect(location.pathname).toBe("/inbox");
    expect(history.length - start).toBe(2);
  });

  it("Done on an item opened from the list goes back to the list's entry", async () => {
    const start = history.length;
    act(() => rowLink("a2").click());
    await openItem("a2");
    const done = [...host.querySelectorAll("button")].filter((b) => b.textContent?.trim() === "已处理").at(-1)!;
    await act(() => popped(() => done.click())); // marking it done settles after the click
    await flush();
    expect(location.pathname).toBe("/inbox");
    expect(history.length - start).toBe(1);
  });

  it("‹ on an item reached from elsewhere puts the list in its place", async () => {
    history.replaceState(null, "", "/inbox/a2");
    const start = history.length;
    await openItem("a2");
    act(() => host.querySelector<HTMLButtonElement>('button[aria-label="返回收件箱"]')!.click());
    expect(location.pathname).toBe("/inbox");
    expect(history.length).toBe(start);
  });
});
