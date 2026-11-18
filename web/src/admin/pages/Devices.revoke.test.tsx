// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((media: string) => ({ matches: false, media, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia;
});
vi.mock("../../ui/atoms", async (original) => ({ ...(await original<typeof import("../../ui/atoms")>()), toast: vi.fn() }));
import { setCsrf, setTransport } from "../api";
import { Devices } from "./Devices";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let host: HTMLDivElement, root: Root;
const pending = new Map<string, () => void>();
const flush = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); }); };
const device = (id: string, name: string) => ({ id, name, kind: "phone", role: "full", created_at: "2026-11-01T00:00:00Z", last_seen: "2026-11-01T00:00:00Z" });
const names = () => [...host.querySelectorAll("span.truncate")].map((s) => s.textContent);
const rowOf = (name: string) => [...host.querySelectorAll("span.truncate")].find((s) => s.textContent === name)!.closest("div.flex-wrap")!;
const revoke = async (name: string) => {
  for (let step = 0; step < 2; step++) {
    const button = [...rowOf(name).querySelectorAll("button")].find((b) => b.getAttribute("aria-label") === "撤销" || b.textContent?.trim() === "撤销");
    expect(button, `${name} step ${step}`).toBeTruthy();
    await act(async () => button!.click());
    await flush();
  }
};

beforeEach(() => {
  setCsrf("tok");
  pending.clear();
  setTransport(async (path, init) => {
    const reply = (body: unknown) => new Response(JSON.stringify(body), { status: 200 });
    if (init?.method === "DELETE") return new Promise<Response>((resolve) => pending.set(decodeURIComponent(path.split("/").pop()!), () => resolve(reply({ ok: true }))));
    if (path === "/api/v1/admin/devices") return reply({ devices: [device("a", "Phone A"), device("b", "Phone B"), device("c", "Phone C")] });
    return reply({});
  });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  document.body.innerHTML = "";
  setTransport((path, init) => fetch(path, init));
  setCsrf("");
});

describe("revoking devices", () => {
  it("keeps the first device gone when a second revoke finishes after it", async () => {
    await act(async () => root.render(<Devices />));
    await flush();
    expect(names()).toEqual(["Phone A", "Phone B", "Phone C"]);
    await revoke("Phone A");
    await revoke("Phone B");
    expect([...pending.keys()]).toEqual(["a", "b"]);
    await act(async () => pending.get("a")!());
    await flush();
    await act(async () => pending.get("b")!());
    await flush();
    expect(names()).toEqual(["Phone C"]);
  });
});
