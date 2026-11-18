// @vitest-environment jsdom
// The extensions' places on reload (docs/web/web.md "Extension pages on reload"): the list this browser was last given is
// drawn from the first paint, before the hello, its badges too, and replaced by the hub's answer; it is kept under the
// feed cache's hub and sign-in scope with the default manager it was read under, so signing out, new credentials, another
// hub or another default never show it.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({ listeners: new Map<string, ((m: Record<string, unknown>) => void)[]>() }));
const STATE = vi.hoisted(() => ({ features: [] as string[], conn: "connecting", version: "1" }));
vi.mock("./store", async (original) => {
  const real = await original<typeof import("./store")>();
  return {
    ...real,
    getState: () => ({ ...real.getState(), ...STATE }),
    useStore: <T,>(select: (s: ReturnType<typeof real.getState>) => T): T => select({ ...real.getState(), ...STATE } as ReturnType<typeof real.getState>),
    onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => {
      hub.listeners.set(type, [...(hub.listeners.get(type) ?? []), l]);
      return () => {};
    },
  };
});

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type Place = { extension: string; id: string; placement: string; title: string; icon?: string; badge?: { text: string } };
const place = (id: string, title: string, badge?: string): Place => ({ extension: "example-today", id, placement: "nav_page", title, icon: "calendar", ...(badge ? { badge: { text: badge } } : {}) });
const emit = (type: string, msg: Record<string, unknown>) => hub.listeners.get(type)?.forEach((l) => l({ type, ...msg }));
let answer: Place[] = [];
let asked = 0;
let host: HTMLDivElement;
let root: Root;
let slots: typeof import("./extension-slots");
let feed: typeof import("./feed-cache");
let managers: typeof import("./manager-choice");
const flush = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};

/** A fresh page load: the modules read again, with what the last one kept. */
async function reload() {
  if (root) act(() => root.unmount());
  hub.listeners.clear();
  // a new page knows nothing of the hub before its hello
  STATE.features = [];
  STATE.conn = "connecting";
  vi.resetModules();
  feed = await import("./feed-cache");
  managers = await import("./manager-choice");
  slots = await import("./extension-slots");
  root = createRoot(host);
}

/** What the page would keep for the next load, as a load before it would have. */
function keep(manager: string, list: Place[]) {
  const key = feed.scopedKey(feed.feedCacheScope(), "ext-places")!;
  localStorage.setItem(key, JSON.stringify({ manager, places: list }));
}

function Page() {
  const entries = slots.useExtensionSlot("nav_page");
  const open = slots.useExtensionPlace("example-today", "today");
  return (
    <>
      <p data-entries>{entries.map((u) => `${u.title}${u.badge ? `(${u.badge.text})` : ""}`).join(",")}</p>
      <h1>{open?.title ?? "today"}</h1>
    </>
  );
}

async function hello() {
  STATE.features = ["extension_ui_slots", "extensions"];
  STATE.conn = "online";
  act(() => emit("hello", { server_version: "1" }));
  await flush();
}

beforeEach(async () => {
  localStorage.clear();
  localStorage.setItem("persona.token", "jd_example_browser_credential");
  answer = [place("today", "Today", "3"), place("lark", "Lark"), place("calendar", "Calendar")];
  asked = 0;
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.includes("/-/ui")) asked++;
    return { ok: true, status: 200, text: async () => JSON.stringify(url.includes("/-/ui") ? { ui: answer } : {}), headers: new Headers() };
  }));
  host = document.createElement("div");
  document.body.append(host);
  await reload();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
});

describe("the extensions' places on reload", () => {
  it("draws the last list, its titles and badges at first paint, before the hello, and the hub's answer replaces it", async () => {
    keep("m-1", [place("today", "Today", "2"), place("lark", "Lark")]);
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today(2),Lark");
    expect(host.querySelector("h1")!.textContent).toBe("Today");
    expect(asked).toBe(0);

    await hello();
    expect(asked).toBe(1);
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today(3),Lark,Calendar");
    // and kept for the next load
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today(3),Lark,Calendar");
  });

  it("keeps a badge an extension_ui event changed", async () => {
    keep("m-1", [place("today", "Today", "2")]);
    await reload();
    act(() => root.render(<Page />));
    act(() => managers.setRosterDefault("m-1"));
    answer = [place("today", "Today", "2")];
    await hello();
    act(() => emit("extension_ui", { extension: "example-today", id: "today", badge: { text: "5" }, manager: "m-1" }));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today(5)");
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today(5)");
  });

  it("has nothing at first paint after signing out, with new credentials, or on another hub", async () => {
    keep("m-1", [place("today", "Today")]);
    feed.clearFeedCache();
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("");
    expect(host.querySelector("h1")!.textContent).toBe("today");

    keep("m-1", [place("today", "Today")]);
    localStorage.setItem("persona.token", "jd_other_account");
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("");

    keep("m-1", [place("today", "Today")]);
    vi.stubGlobal("location", { ...window.location, origin: "https://other.example.com" });
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("");
  });

  it("drops a list kept under another default manager once the roster names this one", async () => {
    keep("m-1", [place("today", "Today")]);
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today");
    answer = [place("calendar", "Calendar")];
    act(() => managers.setRosterDefault("m-2"));
    // shown no longer, before any answer (none is asked before the hello)
    expect(host.querySelector("[data-entries]")!.textContent).toBe("");
    await hello();
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Calendar");
  });

  it("keeps the list under the roster's default once it names the one it was kept under", async () => {
    keep("m-1", [place("today", "Today")]);
    await reload();
    act(() => root.render(<Page />));
    act(() => managers.setRosterDefault("m-1"));
    expect(host.querySelector("[data-entries]")!.textContent).toBe("Today");
  });

  it("shows nothing on a hub without UI slots once its hello says so", async () => {
    keep("m-1", [place("today", "Today")]);
    await reload();
    act(() => root.render(<Page />));
    STATE.features = ["extensions"];
    STATE.conn = "online";
    act(() => emit("hello", { server_version: "1" }));
    await flush();
    expect(host.querySelector("[data-entries]")!.textContent).toBe("");
    expect(asked).toBe(0);
    await reload();
    act(() => root.render(<Page />));
    expect(host.querySelector("h1")!.textContent).toBe("today");
  });
});
