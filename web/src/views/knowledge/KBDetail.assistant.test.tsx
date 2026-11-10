// @vitest-environment jsdom
// A knowledge entry opened while another assistant than the default is current (docs/assistant/managers.md "Clients"):
// its address names no assistant, so it is read from the current one, and everything its page writes afterwards (tags,
// confirmed facts) goes to the same one; another made current opens the entry again from that one.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initI18n } from "../../i18n";
import { setToken } from "../../lib/api";
import type { KBDetailResponse } from "../../lib/kb";
import { chooseManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { EntryDetail } from "./KBDetail";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
type Request = { url: string; init: RequestInit; resolve: (value: Response) => void };
let requests: Request[];
let host: HTMLDivElement;
let root: Root;
const detail: KBDetailResponse = { detail: { kind: "people", id: "example", path: "", meta: { name: "Example entry", tags: ["existing"], importance: "P1" }, summary: "# Example entry" } };
const render = () => act(() => root.render(<EntryDetail kind="people" id="example" reloadKey={0} onToggleSidebar={() => {}} onBack={() => {}} onOpen={() => {}} onSaved={() => {}} />));
const reply = (r: Request, value: unknown) => act(async () => r.resolve(new Response(JSON.stringify(value), { status: 200, headers: { "Content-Type": "application/json" } })));
const named = (r: Request) => new Headers(r.init.headers).get("X-Persona-Manager");
const button = (label: string) => [...document.body.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label)!;
const reads = () => requests.filter((r) => !r.init.method && r.url === "/api/v1/kb/people/example");
const writes = () => requests.filter((r) => r.init.method === "PUT");

beforeEach(async () => {
  await initI18n("en");
  localStorage.clear();
  resetManagerChoice();
  setToken("synthetic-owner-a");
  setRosterDefault("assistant");
  requests = [];
  vi.stubGlobal("fetch", vi.fn((url: string, init: RequestInit) => new Promise<Response>((resolve) => requests.push({ url, init, resolve }))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  resetManagerChoice();
  vi.unstubAllGlobals();
});

it("reads the entry from the current assistant and saves its tags and facts to that one", async () => {
  act(() => chooseManager("m-work"));
  render();
  expect(reads().map(named)).toEqual(["m-work"]);
  await reply(reads()[0], detail);
  act(() => button("Tag").click());
  await act(async () => button("Save").click());
  expect(writes().map(named)).toEqual(["m-work"]);
  await reply(writes()[0], { id: "example" });
  act(() => button("Add").click());
  const text = document.body.querySelector<HTMLTextAreaElement>("textarea")!;
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(text, "Confirmed.");
    text.dispatchEvent(new Event("input", { bubbles: true }));
  });
  act(() => button("Save").click());
  expect(writes().map(named)).toEqual(["m-work", "m-work"]);
});

it("opens the entry again from the assistant made current, writing nothing to the one before", async () => {
  act(() => chooseManager("m-work"));
  render();
  await reply(reads()[0], detail);
  // the kept pick left the roster (or another was picked): the page is the default's now
  act(() => chooseManager(null));
  expect(reads().map(named)).toEqual(["m-work", "assistant"]);
  await reply(reads()[1], detail);
  act(() => button("Tag").click());
  await act(async () => button("Save").click());
  expect(writes().map(named)).toEqual(["assistant"]);
});
