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
