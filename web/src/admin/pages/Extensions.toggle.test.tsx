// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((media: string) => ({ matches: false, media, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { resetExtensionRoot, setCsrf, setTransport } from "../api";
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement, root: Root;
const pending = new Map<string, () => void>();
const flush = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); }); };
const ext = (id: string, enabled: boolean) => ({ id, name: `Ext ${id}`, source: "installed", status: enabled ? "running" : "stopped", enabled, permissions: [] });
const toggle = (id: string) => host.querySelector<HTMLElement>(`[aria-label="启用「Ext ${id}」"]`);
const isOn = (id: string) => toggle(id)?.getAttribute("aria-checked");

beforeEach(() => {
  resetExtensionRoot();
  setCsrf("tok");
  pending.clear();
  setTransport(async (path, init) => {
    const reply = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
    if (init?.method === "PUT" && path.endsWith("/enabled")) {
      const id = decodeURIComponent(path.split("/").slice(-2)[0]);
      const { enabled } = JSON.parse(String(init.body)) as { enabled: boolean };
      return new Promise<Response>((resolve) => pending.set(id, () => resolve(reply(ext(id, enabled)))));
    }
    if (path === "/api/v1/admin/extensions") return reply({ can_install: false, extensions: [ext("a", true), ext("b", true)] });
    return reply({ extensions: [], items: [] });
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  document.body.innerHTML = "";
  setTransport((path, init) => fetch(path, init));
  resetExtensionRoot();
  setCsrf("");
});

describe("admin extension switches", () => {
  it("keeps the first change when a second switch finishes after it", async () => {
    await act(async () => root.render(<Extensions />));
    await flush();
    expect([isOn("a"), isOn("b")]).toEqual(["true", "true"]);
    await act(async () => toggle("a")!.click());
    await act(async () => toggle("b")!.click());
    await flush();
    expect([...pending.keys()]).toEqual(["a", "b"]);
    await act(async () => pending.get("a")!());
    await flush();
    await act(async () => pending.get("b")!());
    await flush();
    expect([isOn("a"), isOn("b")]).toEqual(["false", "false"]);
  });
});
