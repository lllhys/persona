// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { Composer } from "./Composer";
import { t } from "../../i18n";
import { getState, markConversationDeleted, reset } from "../../lib/store";
import { getQuotes, sendDraft, setQuotes, storeDraft } from "../../lib/drafts";
import { addContext, getContexts, newContext } from "../../lib/links";
import { keepFailed, keepOutgoing } from "../../lib/outbox";
import { readHistory, recordSent, HISTORY_PREFIX } from "../../lib/send-history";
import { skillDraft } from "../../lib/skill-mentions";
import { beginMessageSend } from "../../lib/message-send";
import { setToken } from "../../lib/api";

vi.hoisted(() => { Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true, matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }) }); });
let root: Root, host: HTMLDivElement;
const key = "delete-draft-hub/chat";
const typed = (text: string) => act(() => {
  const el = host.querySelector("textarea")!;
  Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(el, text);
  el.dispatchEvent(new Event("input", { bubbles: true }));
});
const mount = (onSend: () => Promise<boolean> = async () => true) => act(() => root.render(<Composer draftKey={key} placeholder="" onSend={onSend} />));
const empty = () => {
  expect(localStorage.getItem(`draft:${key}`)).toBeNull();
  expect(localStorage.getItem(`quote:${key}`)).toBeNull();
  expect(getQuotes(key).list).toEqual([]);
  expect(getContexts(key)).toEqual([]);
  expect(getState().outbox[key]).toBeUndefined();
  expect(Object.keys(localStorage).some((k) => k.startsWith(`outbox-msg:${key}/`))).toBe(false);
};
beforeEach(() => {
  reset(); localStorage.clear(); setToken("deletion-test-owner");
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
});
afterEach(() => { act(() => root.unmount()); host.remove(); reset(); vi.useRealTimers(); });

it("does not persist pending typing or selected skills after deletion, unmount and the draft debounce", async () => {
  mount(); typed("Private unsaved words");
  act(() => { setQuotes(key, [{ id: "quote-private", text: "Private quote" }]); addContext(key, newContext("context", "Private context", "", "Private context text")); addContext(key, { ...newContext("skill", "Private selected skill", "", "Private skill metadata"), attrs: { ref: "opaque-private-skill" } }); });
  act(() => markConversationDeleted("delete-draft-hub", "chat"));
  act(() => root.render(null));
  await act(async () => vi.advanceTimersByTimeAsync(300));
  empty();
});

it("does not restore text, quotes, selected skills, contexts or outbox after a late send failure", async () => {
  let finish!: (ok: boolean) => void;
  mount(() => new Promise<boolean>((resolve) => { finish = resolve; }));
  typed("Private failed send");
  act(() => { setQuotes(key, [{ id: "quote-private", text: "Private quote" }]); addContext(key, newContext("context", "Private context", "", "Private context text")); addContext(key, { ...newContext("skill", "Private selected skill", "", "Private skill metadata"), attrs: { ref: "opaque-private-skill" } }); });
  await act(async () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.send")}"]`)!.click());
  act(() => markConversationDeleted("delete-draft-hub", "chat"));
  act(() => root.render(null));
  await act(async () => finish(false));
  keepFailed("delete-draft-hub", "chat", { id: "late", text: "Private failed send", images: [], at: 1 }, new Error("late failure"));
  keepOutgoing(key, { id: "late2", text: "Private late save", images: [], at: 2 });
  storeDraft(key, "late draft"); setQuotes(key, [{ id: "quote-late", text: "late quote" }]); sendDraft(key, { type: "unsent", text: "late event" });
  await act(async () => vi.advanceTimersByTimeAsync(300));
  empty();
});

it.each(["reset", "credential"])("fences pending draft saves from a previous %s scope and allows new drafts", async (kind) => {
  mount(); typed("Old account words");
  act(() => { if (kind === "reset") reset(); else setToken("replacement-owner"); });
  act(() => root.render(null));
  await act(async () => vi.advanceTimersByTimeAsync(300));
  expect(localStorage.getItem(`draft:${key}`)).toBeNull();
  mount(); typed("New account draft"); act(() => root.render(null));
  expect(localStorage.getItem(`draft:${key}`)).toBe("New account draft");
});

it("does not restore a rejected send into a replacement account", async () => {
  let finish!: (ok: boolean) => void;
  mount(() => new Promise<boolean>((resolve) => { finish = resolve; })); typed("Old account send");
  act(() => addContext(key, { ...newContext("skill", "Private skill", "", "Private metadata"), attrs: { ref: "opaque-private-skill" } }));
  await act(async () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.send")}"]`)!.click());
  act(() => { reset(); setToken("replacement-owner"); root.render(null); });
  await act(async () => finish(false));
  await act(async () => vi.advanceTimersByTimeAsync(300));
  expect(localStorage.getItem(`draft:${key}`)).toBeNull();
  expect(getContexts(key)).toEqual([]);
});

it("retires an outstanding message request before its late HTTP response", () => {
  const request = beginMessageSend("delete-draft-hub", "chat", "pending-send", "Private request");
  expect(request.current()).toBe(true);
  markConversationDeleted("delete-draft-hub", "chat");
  expect(request.current()).toBe(false);
  request.finish("failed");
  empty();
});

it("removes sent history and selected skill references and rejects late history writes", () => {
  const text = skillDraft([{ ...newContext("skill", "Private skill", "", "Private metadata"), attrs: { ref: "opaque-private-skill" } }], "Private sent message");
  recordSent(key, text);
  expect(readHistory(key)).toHaveLength(1);
  markConversationDeleted("delete-draft-hub", "chat");
  expect(localStorage.getItem(HISTORY_PREFIX + key)).toBeNull();
  expect(readHistory(key)).toEqual([]);
  recordSent(key, text);
  expect(localStorage.getItem(HISTORY_PREFIX + key)).toBeNull();
  expect(readHistory(key)).toEqual([]);
});

it("does not record a late successful skill send after deletion", async () => {
  let finish!: (ok: boolean) => void;
  mount(() => new Promise<boolean>((resolve) => { finish = resolve; })); typed("Private sent message");
  act(() => addContext(key, { ...newContext("skill", "Private skill", "", "Private metadata"), attrs: { ref: "opaque-private-skill" } }));
  await act(async () => host.querySelector<HTMLButtonElement>(`button[aria-label="${t("chat-composer.send")}"]`)!.click());
  act(() => { markConversationDeleted("delete-draft-hub", "chat"); root.render(null); });
  await act(async () => finish(true));
  expect(readHistory(key)).toEqual([]);
  expect(localStorage.getItem(HISTORY_PREFIX + key)).toBeNull();
});
