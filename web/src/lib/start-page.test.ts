// @vitest-environment jsdom
// Where the app opens (#9071, lib/start-page.ts): the main chat by default; the start page the user chose at an opening
// of the root address only; the hub's choice read at the hello and followed on its pushes; a choice kept here handed
// to a hub that has none; a hub without the route leaving the choice in this browser.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
vi.hoisted(() => {
  // the Settings row's module draws with the sidebar's pages, which read these
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false })) as typeof window.matchMedia;
});

class Socket {
  static OPEN = 1;
  static last: Socket;
  readyState = 1;
  onopen?: () => void;
  onmessage?: (event: { data: string }) => void;
  constructor(public url: string) {
    Socket.last = this;
  }
  send() {}
  close() {
    this.readyState = 3;
  }
  push(value: unknown) {
    this.onmessage?.({ data: JSON.stringify(value) });
  }
}

const TODAY = "p:example-today/today";
// what the made-up hub keeps: undefined until something is stored; "missing" for a hub without the route
let hub: { start?: string; pages?: unknown } | "missing";
let puts: unknown[];

beforeEach(() => {
  vi.resetModules();
  localStorage.clear();
  localStorage.setItem("persona.token", "test-token");
  history.replaceState(null, "", "/");
  hub = {};
  puts = [];
  vi.stubGlobal("WebSocket", Socket);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string, init?: RequestInit) => {
      const url = new URL(String(input), location.href);
      if (url.pathname !== "/api/v1/navigation") return Response.json({});
      if (hub === "missing") return Response.json({ error: "not found" }, { status: 404 });
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as { start?: string; pages?: unknown };
        puts.push(body);
        hub = { ...hub, ...body };
      }
      return Response.json(hub);
    }),
  );
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

const flush = async () => {
  for (let i = 0; i < 6; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
};

/** This page's modules as a fresh load has them, with `stored` as this browser's copy. */
async function load(stored?: string) {
  if (stored !== undefined) localStorage.setItem("persona.startPage", stored);
  const page = await import("./start-page");
  const store = await import("./store");
  const hello = async (extra: Record<string, unknown> = {}) => {
    store.connect();
    Socket.last.push({ type: "hello", features: [], ...extra });
    await flush();
  };
  return { ...page, store, hello };
}

describe("startRoute", () => {
  it("knows the sidebar's pages and an extension's, and nothing else", async () => {
    const { startRoute } = await load();
    expect(startRoute("")).toBeNull();
    expect(startRoute("home")).toEqual({ name: "home" });
    expect(startRoute("inbox")).toEqual({ name: "inbox" });
    expect(startRoute("workspace-board")).toEqual({ name: "workspace-board" });
    expect(startRoute(TODAY)).toEqual({ name: "extension", extension: "example-today", ui: "today" });
    for (const k of ["settings", "persona", "unknown", "p:", "p:/today", "p:example-today/", "p:example-today"]) expect(startRoute(k)).toBeNull();
  });
});

describe("opening the app", () => {
  it("opens on the main chat when nothing was chosen", async () => {
    const { openStartPage } = await load();
    openStartPage();
    expect(location.pathname).toBe("/");
  });

  it("opens on the chosen page, an extension's Today", async () => {
    const { openStartPage } = await load(TODAY);
    openStartPage();
    expect(location.pathname).toBe("/extension/example-today/today");
  });

  it("opens on Overview when that is chosen", async () => {
    const { openStartPage } = await load("home");
    openStartPage();
    expect(location.pathname).toBe("/overview");
  });

  it("leaves a reload, Back / Forward, another page's address and a query where they are", async () => {
    const { openStartPage } = await load(TODAY);
    for (const type of ["reload", "back_forward"]) {
      vi.spyOn(performance, "getEntriesByType").mockReturnValue([{ type } as PerformanceNavigationTiming]);
      openStartPage();
      expect(location.pathname).toBe("/");
    }
    vi.restoreAllMocks();
    for (const at of ["/inbox", "/persona", "/?machine=x"]) {
      history.replaceState(null, "", at);
      openStartPage();
      expect(location.pathname + location.search).toBe(at);
    }
  });

  it("opens on the main chat for a key this version does not know", async () => {
    const { openStartPage } = await load("p:broken");
    openStartPage();
    expect(location.pathname).toBe("/");
  });
});

describe("startPageGone", () => {
  it("says gone only once it can be told", async () => {
    const { startPageGone } = await load();
    const none = { extensions: false, board: false };
    const all = { extensions: true, board: true };
    expect(startPageGone(TODAY, true, none)).toBe(false);
    expect(startPageGone(TODAY, false, none)).toBeUndefined();
    expect(startPageGone(TODAY, false, all)).toBe(true);
    expect(startPageGone("workspace-board", false, { extensions: true, board: false })).toBeUndefined();
    expect(startPageGone("workspace-board", false, all)).toBe(true);
    expect(startPageGone("messages", false, none)).toBe(true);
  });
});

describe("the hub's choice", () => {
  it("is read at the hello, kept for the next opening, and followed on its pushes", async () => {
    hub = { start: TODAY };
    const { hello, startPage, startPageSynced } = await load();
    expect(startPage()).toBe("");
    await hello();
    expect(startPage()).toBe(TODAY);
    expect(startPageSynced()).toBe(true);
    expect(localStorage.getItem("persona.startPage")).toBe(TODAY);
    Socket.last.push({ type: "navigation", start: "" });
    expect(startPage()).toBe("");
    expect(localStorage.getItem("persona.startPage")).toBeNull();
    expect(puts).toEqual([]);
  });

  it("is not read again at a hello that resumed", async () => {
    const { hello, store } = await load();
    await hello();
    const reads = vi.mocked(fetch).mock.calls.filter(([u]) => String(u).includes("/navigation")).length;
    Socket.last.push({ type: "hello", features: [], resumed: true });
    await flush();
    expect(vi.mocked(fetch).mock.calls.filter(([u]) => String(u).includes("/navigation")).length).toBe(reads);
    store.disconnect();
  });

  it("an empty start is the main chat chosen: it wins over this browser's older copy", async () => {
    hub = { start: "" };
    const { hello, startPage } = await load(TODAY);
    await hello();
    expect(startPage()).toBe("");
    expect(puts).toEqual([]);
  });

  it("is handed the choice this browser kept while the hub had none", async () => {
    const { hello, startPage } = await load("kb");
    await hello();
    expect(puts).toEqual([{ start: "kb" }]);
    expect(startPage()).toBe("kb");
  });

  it("two devices: what one chooses, the other opens on from its next opening", async () => {
    // the laptop chooses Today
    const laptop = await load();
    await laptop.hello();
    const { chooseStartPage } = await import("../views/settings/StartPage");
    await chooseStartPage(TODAY);
    expect(puts).toEqual([{ start: TODAY }]);
    laptop.store.disconnect();
    // the phone: another browser, nothing kept yet; its first opening is the main chat, and it reads the hub's choice
    vi.resetModules();
    localStorage.clear();
    localStorage.setItem("persona.token", "test-token");
    const phone = await load();
    phone.openStartPage();
    expect(location.pathname).toBe("/");
    await phone.hello();
    expect(phone.startPage()).toBe(TODAY);
    phone.store.disconnect();
    // its next opening
    vi.resetModules();
    const next = await load();
    next.openStartPage();
    expect(location.pathname).toBe("/extension/example-today/today");
  });
});

describe("the sidebar's arrangement (groups, #9071)", () => {
  const arrangement = { order: ["routines"], shown: [], hidden: [], groups: [{ id: "u:life", title: "生活" }], assign: { routines: "u:life" } };

  it("two devices: what one arranges, the other draws from its hello and on the push", async () => {
    const laptop = await load();
    await laptop.hello();
    const pages = await import("./sidebar-pages");
    const life = pages.addPageGroup([""], "生活");
    pages.setPageGroup("routines", life);
    await flush();
    expect(hub).toMatchObject({ pages: { groups: [{ id: "" }, { id: life, title: "生活" }], assign: { routines: life } } });
    laptop.store.disconnect();
    vi.resetModules();
    localStorage.clear();
    localStorage.setItem("persona.token", "test-token");
    const phone = await load();
    await phone.hello();
    const phonePages = await import("./sidebar-pages");
    expect(phonePages.getSidebarPrefs().assign).toEqual({ routines: life });
    expect(JSON.parse(localStorage.getItem("sidebar.pages")!).groups[1]).toEqual({ id: life, title: "生活" });
    // another device renames it: the push brings it here
    Socket.last.push({ type: "navigation", pages: { ...arrangement, groups: [{ id: life, title: "Life" }], assign: { routines: life } } });
    expect(phonePages.getSidebarPrefs().groups).toEqual([{ id: life, title: "Life" }]);
  });

  it("is handed this browser's arrangement when the hub has none", async () => {
    hub = { start: "" };
    localStorage.setItem("sidebar.pages", JSON.stringify(arrangement));
    const { hello } = await load();
    await hello();
    expect(puts).toEqual([{ pages: arrangement }]);
  });

  it("keeps a change the hub did not take and sends it again at the next hello", async () => {
    hub = { pages: { order: [], shown: [], hidden: [] } };
    const { hello } = await load();
    await hello();
    const pages = await import("./sidebar-pages");
    vi.mocked(fetch).mockImplementationOnce(async () => Response.json({ error: "database down" }, { status: 503 }));
    pages.setPageShown("kb", false);
    await flush();
    expect(pages.getSidebarPrefs().hidden).toEqual(["kb"]);
    await hello();
    expect(pages.getSidebarPrefs().hidden).toEqual(["kb"]);
    expect(hub).toMatchObject({ pages: { hidden: ["kb"] } });
  });

  it("stays in this browser on a hub without the route", async () => {
    hub = "missing";
    const { hello } = await load();
    await hello();
    const pages = await import("./sidebar-pages");
    pages.setPageShown("kb", false);
    await flush();
    expect(JSON.parse(localStorage.getItem("sidebar.pages")!).hidden).toEqual(["kb"]);
    expect(vi.mocked(fetch).mock.calls.filter(([, i]) => (i as RequestInit | undefined)?.method === "PUT")).toEqual([]);
  });
});

describe("a hub without the route", () => {
  it("keeps the choice in this browser and opens on it", async () => {
    hub = "missing";
    const { hello, startPage, startPageSynced } = await load();
    await hello();
    expect(startPageSynced()).toBe(false);
    const { chooseStartPage } = await import("../views/settings/StartPage");
    await chooseStartPage("inbox");
    expect(startPage()).toBe("inbox");
    expect(localStorage.getItem("persona.startPage")).toBe("inbox");
    // no PUT after the hub said it has no route
    expect(vi.mocked(fetch).mock.calls.filter(([, i]) => (i as RequestInit | undefined)?.method === "PUT")).toEqual([]);
    vi.resetModules();
    const next = await load();
    next.openStartPage();
    expect(location.pathname).toBe("/inbox");
  });

  it("a refused save puts the last choice back", async () => {
    hub = { start: "inbox" };
    const { hello, startPage } = await load();
    await hello();
    vi.mocked(fetch).mockImplementationOnce(async () => Response.json({ error: "database down" }, { status: 503 }));
    const { chooseStartPage } = await import("../views/settings/StartPage");
    await chooseStartPage("kb");
    expect(startPage()).toBe("inbox");
  });
});
