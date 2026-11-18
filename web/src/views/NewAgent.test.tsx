// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Machine } from "../lib/types";

const TWO: Machine[] = [{ id: "first", name: "First computer", online: true }, { id: "second", name: "Second computer", online: true }];
const state = vi.hoisted(() => ({ loaded: true, features: [] as string[], machines: [] as Machine[] }));
// one assistant: its computers are the store's (lib/current-assistant.ts reads the roster, which this store stand-in has none of)
vi.mock("../lib/current-assistant", async () => {
  const store = (await import("../lib/store")) as unknown as { useStore: <T>(select: (s: { machines: unknown[] }) => T) => T };
  return { useScopedMachines: () => store.useStore((s) => s.machines) };
});
vi.mock("../lib/store", () => ({ useStore: (selector: (value: { machines: Machine[] }) => unknown) => selector(state), hubHas: () => false }));
vi.mock("react-dom", async (original) => ({ ...(await original<typeof import("react-dom")>()), createPortal: (children: React.ReactNode) => children }));

import { NewAgent } from "./NewAgent";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type Pending = { path: string; signal?: AbortSignal | null; answer: (value: unknown) => void; reject: (error: unknown) => void };
let pending: Pending[];
let host: HTMLDivElement;
let root: Root;
const flush = () => act(async () => { await Promise.resolve(); });
const dirs = (name: string) => ({ path: `/projects/${name}`, entries: [{ name, path: `/projects/${name}/child` }] });
const clickMachine = (name: string) => act(() => [...host.querySelectorAll("button")].find((button) => button.textContent?.trim() === name)!.click());

beforeEach(() => {
  state.machines = TWO;
  localStorage.clear();
  pending = [];
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    if (!String(input).includes("/dirs?")) return Promise.resolve(new Response("{}"));
    return new Promise<Response>((resolve, reject) => pending.push({ path: String(input), signal: init?.signal, answer: (value) => resolve(new Response(JSON.stringify(value))), reject }));
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  act(() => root.render(<NewAgent open onClose={() => {}} />));
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("NewAgent directory requests", () => {
  it("keeps the current machine's directories when an old answer arrives last", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    const second = pending.at(-1)!;
    expect(second.path).toContain("/machines/second/dirs?");
    await act(async () => second.answer(dirs("current-folder")));
    expect(host.textContent).toContain("current-folder");
    await act(async () => first.answer(dirs("stale-folder")));
    expect(host.textContent).not.toContain("stale-folder");
    expect(host.textContent).toContain("current-folder");
    expect(first.signal?.aborted).toBe(true);
  });

  it("keeps the current directories when a cancelled request rejects", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    await act(async () => pending.at(-1)!.answer(dirs("current-folder")));
    await act(async () => first.reject(new DOMException("Aborted", "AbortError")));
    expect(host.textContent).toContain("current-folder");
  });

  it("keeps the directory spinner until the current request finishes", async () => {
    await flush();
    const first = pending[0]!;
    clickMachine("Second computer");
    await flush();
    const spinner = () => host.querySelector('span[style*="width: 10px"]');
    expect(spinner()).not.toBeNull();
    await act(async () => first.reject(new DOMException("Aborted", "AbortError")));
    expect(spinner()).not.toBeNull();
    await act(async () => pending.at(-1)!.answer(dirs("current-folder")));
    expect(spinner()).toBeNull();
  });
});

describe("NewAgent with no computer", () => {
  it("says why the 电脑 field is empty and offers 添加电脑 instead of a form that cannot start", async () => {
    state.machines = [];
    act(() => root.render(<NewAgent open onClose={() => {}} key="none" />));
    await flush();
    const empty = document.body.querySelector('[data-testid="new-agent-no-computer"]');
    expect(empty?.textContent).toContain("还没有电脑");
    expect([...empty!.querySelectorAll("button")].map((b) => b.textContent?.trim())).toContain("添加电脑");
    expect(document.body.textContent).not.toContain("选一个目录");
  });
});

describe("NewAgent with an Inbox item (#5933)", () => {
  it("shows the item's card, forwarded from where it came, over the first message, and the ask in the box", async () => {
    const forward = { id: "inbox:lark:f1:1", kind: "context" as const, title: "Lark · Example", url: "", text: "Example: is it merged?" };
    act(() => root.render(<NewAgent open onClose={() => {}} preset={{ forwards: [forward], prompt: "Look into it" }} key="inbox" />));
    await flush();
    const card = document.body.querySelector("[data-new-agent-forward]");
    expect(card?.textContent).toContain("Lark · Example");
    expect(card?.textContent).toContain("Example: is it merged?");
    expect(card?.textContent).toContain("转发自 Lark · Example");
    expect([...document.body.querySelectorAll("textarea")].some((t) => t.value.startsWith("Look into it"))).toBe(true);
  });
});

describe("NewAgent draft persistence under storage pressure", () => {
  it("keeps the box mounted when its prompt draft write hits a full localStorage", async () => {
    const quotaError = () => {
      const err = new Error("QuotaExceededError");
      err.name = "QuotaExceededError";
      throw err;
    };
    const original = Storage.prototype.setItem;
    const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(function (this: Storage, key: string, value: string) {
      if (key === "draft:new-agent") quotaError();
      return original.call(this, key, value);
    });
    try {
      expect(() => act(() => root.render(<NewAgent open onClose={() => {}} preset={{ prompt: "Check this machine" }} key="quota" />))).not.toThrow();
      await flush();
      expect(localStorage.getItem("draft:new-agent")).toBeNull(); // the failed write stayed quiet, nothing half-written
      expect([...document.body.querySelectorAll("textarea")].some((t) => t.value.startsWith("Check this machine"))).toBe(true);
    } finally {
      spy.mockRestore();
    }
  });
});
