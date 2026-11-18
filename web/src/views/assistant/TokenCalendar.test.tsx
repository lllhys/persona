// @vitest-environment jsdom
// The token calendar on the new-chat page (#9506): with usage it shows today's and the last 30 days' tokens over a
// square a day; a clicked day shows its own tokens and four kinds, clicked again today's line is back; a hub with no
// usage or without the route (older) shows nothing; it reads when the tab comes back, never on a timer.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({ calls: [] as string[], answer: "days" as "days" | "quiet" | "older" }));
vi.mock("../../lib/api", async (orig) => {
  const real = await orig<typeof import("../../lib/api")>();
  return {
    ...real,
    api: (path: string) => {
      hub.calls.push(path);
      if (hub.answer === "older") return Promise.reject(new real.ApiError(404, "not found", {}));
      const days = hub.answer === "quiet"
        ? [{ date: "2026-11-16" }, { date: "2026-11-17" }]
        : [
            { date: "2026-11-15", cache_read: 500_000_000 },
            { date: "2026-11-16", in: 1, out: 2_000_000, cache_write: 30_000_000, cache_read: 5_800_000_000 },
            { date: "2026-11-17", cache_read: 3_200_000_000 },
          ];
      return Promise.resolve({ usage: { zone: "UTC", stored: true, days } });
    },
  };
});

import TokenCalendar from "./TokenCalendar";

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  hub.calls = [];
  hub.answer = "days";
  host = document.createElement("div");
  document.body.appendChild(host);
  root = createRoot(host);
  globalThis.ResizeObserver ??= class {
    observe() {}
    disconnect() {}
    unobserve() {}
  } as unknown as typeof ResizeObserver;
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

async function show() {
  await act(async () => root.render(<TokenCalendar />));
  await act(async () => {});
}

describe("TokenCalendar", () => {
  it("shows today's and the last 30 days' tokens over the days, and a clicked day's own", async () => {
    await show();
    expect(hub.calls).toEqual(["/usage/days?days=280"]);
    const card = host.querySelector("[data-token-calendar]")!;
    expect(card).not.toBeNull();
    // the tests run in Chinese: its own short numbers
    expect(card.textContent).toContain("今天 32亿 token");
    expect(card.textContent).toContain("近 30 天 95.3亿");
    const squares = [...card.querySelectorAll<HTMLElement>("span[title]")];
    const busy = squares.find((s) => s.title.includes("58.3亿"))!;
    act(() => busy.click());
    expect(card.textContent).toContain("缓存读 58亿");
    expect(card.textContent).not.toContain("近 30 天");
    act(() => busy.click());
    expect(card.textContent).toContain("近 30 天");
  });

  it("reads again when the tab comes back to the front", async () => {
    await show();
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    await act(async () => document.dispatchEvent(new Event("visibilitychange")));
    expect(hub.calls).toHaveLength(2);
  });

  it("shows nothing without usage or on an older hub", async () => {
    hub.answer = "quiet";
    await show();
    expect(host.querySelector("[data-token-calendar]")).toBeNull();
    act(() => root.unmount());
    root = createRoot(host);
    hub.answer = "older";
    await show();
    expect(host.querySelector("[data-token-calendar]")).toBeNull();
  });
});
