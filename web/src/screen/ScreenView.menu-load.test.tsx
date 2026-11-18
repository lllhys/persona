// @vitest-environment jsdom
// The Displays menu's chunk that would not load (offline, the hub redeployed; #5729): the page stays up and shows every
// display, also for a Mac this browser kept one display for, since there is no menu to go back to all of them.
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";

const rfbs = vi.hoisted(() => [] as (EventTarget & { canvas: HTMLCanvasElement })[]);
vi.mock("@novnc/novnc", () => ({
  default: class extends EventTarget {
    viewOnly = true;
    canvas = document.createElement("canvas");
    constructor(target: HTMLElement) {
      super();
      target.append(this.canvas);
      rfbs.push(this as unknown as EventTarget & { canvas: HTMLCanvasElement });
    }
    sendCredentials() {}
    disconnect() {}
  },
}));
vi.mock("../views/theme", () => ({}));
vi.mock("./aes", () => ({ ensureSubtleAES: () => {} }));
vi.mock("../lib/api", async (actual) => ({
  ...(await actual<typeof import("../lib/api")>()),
  api: async (path: string) =>
    path === "/machines"
      ? { machines: [{ id: "macbook", name: "MacBook", online: true }] }
      : { enabled: true, screen_sharing: true, displays: [{ id: 1, x: 0, y: 0, width: 240, height: 150 }, { id: 2, x: 240, y: 0, width: 240, height: 150 }] },
}));
vi.mock("./DisplayMenu", () => {
  throw new Error("Failed to fetch dynamically imported module");
});
import { ScreenView } from "./ScreenView";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

it("shows every display when the Displays menu would not load", async () => {
  localStorage.setItem("persona.screen.display.macbook", "display-2");
  // the room below the bar, so display 2 would be cropped if the page did crop; jsdom lays nothing out
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue({ left: 0, top: 0, right: 480, bottom: 400, width: 480, height: 400, x: 0, y: 0, toJSON() {} });
  const host = document.createElement("div");
  document.body.append(host);
  const root = createRoot(host);
  const flush = async () => {
    for (let i = 0; i < 5; i++) await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
  };
  act(() => root.render(<ScreenView target={{ mid: "macbook", token: "tok", embedded: false, name: "MacBook", keep: "", unlock: false }} />));
  await flush();
  const canvas = rfbs.at(-1)!.canvas;
  canvas.width = 960;
  canvas.height = 300;
  await flush();
  act(() => void rfbs.at(-1)!.dispatchEvent(new CustomEvent("connect", { detail: {} })));
  await flush();

  expect(host.querySelector("header")).not.toBeNull();
  expect(host.querySelector('[data-testid="screen-displays"]')).toBeNull();
  expect(host.querySelector<HTMLElement>('[data-testid="screen"]')!.style.width, "not cropped to display 2").toBe("");
  act(() => root.unmount());
  host.remove();
  localStorage.clear();
  vi.restoreAllMocks();
});
