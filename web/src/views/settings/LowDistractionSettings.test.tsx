// @vitest-environment jsdom
// Settings › Notifications › Low-distraction mode (#8157): the row shows the hub's switch as GET /push/ln-mode answers,
// a tap PUTs it and shows what the hub answered, a failed write goes back and says so, and a hub without the route
// says to update it. A new credential reads the switch again from nothing, and answers to the old one's reads and
// writes change nothing; the settings search lands on the group in every state.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import { setToken } from "../../lib/api";
import { searchSettings } from "./search";

type Call = { path: string; method: string; json?: unknown; resolve: (v: unknown) => void; reject: (e: unknown) => void };
const calls: Call[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { method?: string; json?: unknown }) =>
    new Promise((resolve, reject) => calls.push({ path, method: init?.method ?? "GET", json: init?.json, resolve, reject })),
}));
const toast = vi.fn();
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: (...a: unknown[]) => toast(...a) }));

const { LowDistractionSettings } = await import("./LowDistractionSettings");

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  calls.length = 0;
  toast.mockReset();
  setToken("token-a");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const toggle = () => host.querySelector<HTMLButtonElement>('[role="switch"]');
const last = () => calls[calls.length - 1];

async function mount(answer: unknown) {
  act(() => root.render(<LowDistractionSettings />));
  expect(calls[0]).toMatchObject({ path: "/push/ln-mode", method: "GET" });
  await act(async () => calls[0].resolve(answer));
}

describe("LowDistractionSettings", () => {
  it("shows the hub's switch with what it does, and turns it on and back off", async () => {
    await mount({ enabled: false });
    expect(toggle()?.getAttribute("aria-checked")).toBe("false");
    expect(toggle()?.getAttribute("aria-label")).toBe(t("settings-notifications.low_distraction"));
    expect(host.textContent).toContain(t("settings-notifications.low_distraction_footer"));

    act(() => toggle()!.click());
    expect(last()).toMatchObject({ path: "/push/ln-mode", method: "PUT", json: { enabled: true } });
    expect(toggle()?.disabled).toBe(true); // one write at a time
    await act(async () => last().resolve({ enabled: true }));
    expect(toggle()?.getAttribute("aria-checked")).toBe("true");
    expect(toggle()?.disabled).toBe(false);

    act(() => toggle()!.click());
    expect(last()).toMatchObject({ method: "PUT", json: { enabled: false } });
    await act(async () => last().resolve({ enabled: false }));
    expect(toggle()?.getAttribute("aria-checked")).toBe("false");
  });

  it("goes back and says so when the hub refuses the change", async () => {
    await mount({ enabled: true });
    act(() => toggle()!.click());
    await act(async () => last().reject(Object.assign(new Error("hub away"), { status: 503 })));
    expect(toggle()?.getAttribute("aria-checked")).toBe("true");
    expect(toast).toHaveBeenCalledWith(expect.stringContaining("hub away"), "error");
  });

  it("says to update a hub without the switch", async () => {
    await mount(undefined);
    expect(toggle()).not.toBeNull(); // a body without enabled reads off
    act(() => root.render(<></>));
    calls.length = 0;
    act(() => root.render(<LowDistractionSettings />));
    await act(async () => calls[0].reject(Object.assign(new Error("not found"), { status: 404 })));
    expect(toggle()).toBeNull();
    expect(host.textContent).toContain(t("settings-notifications.low_distraction_unsupported"));
  });

  it("offers Retry when the switch cannot be read", async () => {
    act(() => root.render(<LowDistractionSettings />));
    await act(async () => calls[0].reject(Object.assign(new Error("offline"), { status: 0 })));
    expect(toggle()).toBeNull();
    const retry = [...host.querySelectorAll("button")].find((b) => b.textContent === t("settings-notifications.retry"))!;
    act(() => retry.click());
    await act(async () => last().resolve({ enabled: true }));
    expect(toggle()?.getAttribute("aria-checked")).toBe("true");
  });

  // review of #8287: a new pairing or sign-in is another hub's switch
  it("reads the switch again from nothing when the credential changes, and never shows the old hub's", async () => {
    await mount({ enabled: true });
    expect(toggle()?.getAttribute("aria-checked")).toBe("true");
    act(() => setToken("token-b"));
    expect(last()).toMatchObject({ path: "/push/ln-mode", method: "GET" });
    expect(calls).toHaveLength(2);
    expect(toggle()).toBeNull(); // the new hub has not answered: no value, not the old one
    await act(async () => last().reject(Object.assign(new Error("offline"), { status: 0 })));
    expect(toggle()).toBeNull();
    expect(host.textContent).toContain(t("settings-notifications.low_distraction_load_failed"));
  });

  it("drops a write's answer that comes after the credential changed, and lets the new hub's switch be used", async () => {
    await mount({ enabled: false });
    act(() => toggle()!.click());
    const put = last();
    expect(put).toMatchObject({ method: "PUT", json: { enabled: true } });
    act(() => setToken("token-b"));
    const read = last();
    expect(read).toMatchObject({ method: "GET" });
    await act(async () => put.resolve({ enabled: true })); // the old hub's answer, before the new hub's
    expect(toggle()).toBeNull();
    await act(async () => read.resolve({ enabled: false }));
    expect(toggle()?.getAttribute("aria-checked")).toBe("false");
    expect(toggle()?.disabled).toBe(false);

    act(() => toggle()!.click());
    const second = last();
    act(() => setToken("token-c"));
    await act(async () => last().resolve({ enabled: true }));
    await act(async () => second.reject(Object.assign(new Error("old hub refused"), { status: 403 }))); // after the new hub's
    expect(toggle()?.getAttribute("aria-checked")).toBe("true");
    expect(toggle()?.disabled).toBe(false);
    expect(toast).not.toHaveBeenCalled();
  });

  it("says nothing about a write that ends after the page is closed", async () => {
    await mount({ enabled: false });
    act(() => toggle()!.click());
    const put = last();
    act(() => root.render(<></>));
    await act(async () => put.reject(Object.assign(new Error("hub away"), { status: 503 })));
    expect(toast).not.toHaveBeenCalled();
  });

  it("is where the settings search lands, whatever the hub answers", async () => {
    const hit = searchSettings(t("settings-notifications.low_distraction")).groups.find((g) => g.tab === "notifications");
    expect(hit).toBeDefined();
    const there = () => host.querySelector(`[data-settings-group="${hit!.anchor}"], [data-settings-row="${hit!.anchor}"]`);
    const answers: [string, (c: Call) => void][] = [
      ["switch", (c) => c.resolve({ enabled: true })],
      ["no route", (c) => c.reject(Object.assign(new Error("not found"), { status: 404 }))],
      ["unreadable", (c) => c.reject(Object.assign(new Error("offline"), { status: 0 }))],
    ];
    for (const [what, answer] of answers) {
      act(() => root.render(<></>));
      calls.length = 0;
      act(() => root.render(<LowDistractionSettings />));
      expect(there(), `${what}: before the hub answers`).not.toBeNull();
      await act(async () => answer(calls[0]));
      expect(there(), what).not.toBeNull();
    }
  });
});
