// @vitest-environment jsdom
// Both surfaces under the terminal's Appearance (lib/terminal-appearance.ts): the colors xterm draws with, and in Full
// view what herdr is told: the answer to CSI ? 996 n, and OSC 10/11 + CSI ? 997 while herdr has DEC 2031 on.
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hub = vi.hoisted(() => ({
  sent: [] as Record<string, unknown>[],
  listeners: new Map<string, Set<(msg: Record<string, unknown>) => void>>(),
}));

vi.mock("../../../lib/store", () => ({
  isOpen: () => true,
  getState: () => ({ features: ["herdr_client", "terminal_stream", "terminal_control"], conn: "online", sessions: {} }),
  send: (msg: Record<string, unknown>) => void hub.sent.push(msg),
  onHubEvent: (type: string, l: (msg: Record<string, unknown>) => void) => {
    if (!hub.listeners.has(type)) hub.listeners.set(type, new Set());
    hub.listeners.get(type)!.add(l);
    return () => void hub.listeners.get(type)?.delete(l);
  },
}));

vi.mock("../../../ui/atoms", () => ({ toast: vi.fn() }));

vi.mock("./cjk", () => ({
  terminalFontReady: () => Promise.resolve(),
  cjkFontStack: () => Promise.resolve(null),
}));

type Csi = (params: number[]) => boolean;

// A stand-in for xterm: what both feeds touch, and the CSI handlers herdr's questions go to.
const terms = vi.hoisted(() => [] as { options: Record<string, unknown>; csi: Map<string, Csi[]> }[]);

vi.mock("./xterm", () => {
  const disposable = () => ({ dispose: () => {} });
  class Terminal {
    cols = 80;
    rows = 24;
    element: HTMLElement | null = null;
    options: Record<string, unknown>;
    modes = { applicationCursorKeysMode: false, mouseTrackingMode: "none" };
    csi = new Map<string, Csi[]>();
    parser = {
      registerCsiHandler: (id: { prefix?: string; intermediates?: string; final: string }, cb: Csi) => {
        const key = `${id.prefix ?? ""}${id.intermediates ?? ""}${id.final}`;
        this.csi.set(key, [...(this.csi.get(key) ?? []), cb]);
        return disposable();
      },
    };
    constructor(options: Record<string, unknown>) {
      this.options = { ...options };
      terms.push(this);
    }
    loadAddon() {}
    open(host: HTMLElement) {
      this.element = document.createElement("div");
      host.appendChild(this.element);
    }
    onData = disposable;
    onResize = disposable;
    onWriteParsed = disposable;
    onSelectionChange = disposable;
    attachCustomKeyEventHandler() {}
    attachCustomWheelEventHandler() {}
    hasSelection = () => false;
    clearSelection() {}
    focus() {}
    blur() {}
    reset() {}
    resize(cols: number, rows: number) {
      this.cols = cols;
      this.rows = rows;
    }
    refresh() {}
    write(_data: unknown, cb?: () => void) {
      cb?.();
    }
    paste() {}
    dispose() {}
  }
  class FitAddon {
    proposeDimensions = () => ({ cols: 80, rows: 24 });
  }
  class SearchAddon {
    onDidChangeResults = disposable;
  }
  return { Terminal, FitAddon, SearchAddon, ClipboardAddon: class {}, WebLinksAddon: class {}, matchHerdrWidths: () => {} };
});

import { setTerminalAppearance } from "../../../lib/terminal-appearance";
import { colourReports } from "../../../lib/terminal-mode";
import { TerminalFeed } from "./feed";
import { FullFeed } from "./full";

// The page's tokens in the shape styles.css has them: each scheme's block also scoped for the terminal.
const LIGHT = { page: "#fafafb", ink: "#1f2124" };
const DARK = { page: "#17181a", ink: "#f2f3f4" };
const SCHEME = { light: `\x1b[?997;2n`, dark: `\x1b[?997;1n` };
const told = (c: typeof LIGHT) => colourReports(c.ink, c.page);

const root = document.documentElement;
let style: HTMLStyleElement;
const feeds: { dispose(): void }[] = [];

class FakeResizeObserver {
  observe() {}
  disconnect() {}
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  localStorage.clear();
  localStorage.setItem("persona.terminal.fullHint", "1");
  style = document.createElement("style");
  style.textContent = `[data-terminal-scheme="light"], :root { --page: ${LIGHT.page}; --ink: ${LIGHT.ink}; }
[data-terminal-scheme="dark"], .dark { --page: ${DARK.page}; --ink: ${DARK.ink}; }`;
  document.head.appendChild(style);
  hub.sent.length = 0;
  hub.listeners.clear();
  terms.length = 0;
});

afterEach(() => {
  feeds.splice(0).forEach((f) => f.dispose());
  style.remove();
  root.className = "";
  localStorage.clear();
  document.body.innerHTML = "";
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

const pageDark = (dark: boolean) => root.classList.toggle("dark", dark);
/** Lets the MutationObserver on <html> run. */
const settle = () => vi.advanceTimersByTimeAsync(0);
/** What went to herdr since the last look. */
function input(): string {
  const out = hub.sent.filter((m) => m.type === "herdr_input").map((m) => String(m.data)).join("");
  hub.sent.length = 0;
  return out;
}

async function liveFull() {
  const feed = new FullFeed("m1", "s1");
  feeds.push(feed);
  const host = document.body.appendChild(document.createElement("div"));
  await feed.mount(host);
  feed.claim({}, host, host, "s1");
  const id = hub.sent.find((m) => m.type === "herdr_open")!.id;
  hub.listeners.get("herdr_state")?.forEach((l) => l({ type: "herdr_state", id, cols: 80, rows: 24 }));
  expect(feed.snapshot().status).toBe("live");
  const term = terms.at(-1)!;
  const ask = (key: string, ...params: number[]) => term.csi.get(key)!.some((h) => h(params));
  input();
  return {
    feed,
    term,
    /** herdr's CSI ? 996 n: the answer it gets. */
    scheme: () => (ask("?n", 996), input()),
    /** herdr's CSI ? 2031 h: tell it when the colors change. */
    notify: () => ask("?h", 2031),
  };
}

describe("Full view under Match page (the default)", () => {
  it("draws with the page's tokens and answers herdr with the page's scheme, as before", async () => {
    const full = await liveFull();
    expect(full.feed.snapshot().palette?.background).toBe(LIGHT.page);
    expect(full.term.options.theme).toMatchObject({ background: LIGHT.page, foreground: LIGHT.ink });
    expect(full.scheme()).toBe(SCHEME.light);
  });

  it("tells herdr when the page's scheme changes", async () => {
    const full = await liveFull();
    full.notify();
    pageDark(true);
    await settle();
    expect(input()).toBe(told(DARK) + SCHEME.dark);
    expect(full.term.options.theme).toMatchObject({ background: DARK.page });
    expect(full.scheme()).toBe(SCHEME.dark);
  });
});

describe("Full view with the terminal set to Dark", () => {
  beforeEach(() => localStorage.setItem("persona.terminal.appearance", "dark"));

  it("draws a light page's terminal with the dark tokens and tells herdr it is dark", async () => {
    const full = await liveFull();
    expect(full.feed.snapshot().palette?.background).toBe(DARK.page);
    expect(full.term.options.theme).toMatchObject({ background: DARK.page, foreground: DARK.ink }); // xterm answers OSC 10/11 from these
    expect(full.scheme()).toBe(SCHEME.dark);
  });

  it("tells herdr nothing when the page's scheme changes", async () => {
    const full = await liveFull();
    full.notify();
    pageDark(true);
    await settle();
    pageDark(false);
    await settle();
    expect(input()).toBe("");
    expect(full.term.options.theme).toMatchObject({ background: DARK.page });
    expect(full.scheme()).toBe(SCHEME.dark);
  });
});

describe("Full view with the terminal set to Light", () => {
  beforeEach(() => localStorage.setItem("persona.terminal.appearance", "light"));

  it("draws a dark page's terminal with the light tokens and tells herdr it is light", async () => {
    pageDark(true);
    const full = await liveFull();
    expect(full.feed.snapshot().palette?.background).toBe(LIGHT.page);
    expect(full.term.options.theme).toMatchObject({ background: LIGHT.page, foreground: LIGHT.ink });
    expect(full.scheme()).toBe(SCHEME.light);
  });

  it("tells herdr nothing when the page's scheme changes", async () => {
    pageDark(true);
    const full = await liveFull();
    full.notify();
    pageDark(false);
    await settle();
    expect(input()).toBe("");
    expect(full.scheme()).toBe(SCHEME.light);
  });
});

describe("Full view when the Appearance is changed", () => {
  it("tells herdr the new colors and scheme, when it asked to be told", async () => {
    const full = await liveFull();
    full.notify();
    setTerminalAppearance("dark");
    expect(input()).toBe(told(DARK) + SCHEME.dark);
    expect(full.term.options.theme).toMatchObject({ background: DARK.page });
    setTerminalAppearance("page");
    expect(input()).toBe(told(LIGHT) + SCHEME.light);
    pageDark(true);
    await settle();
    input();
    setTerminalAppearance("light");
    expect(input()).toBe(told(LIGHT) + SCHEME.light);
    expect(full.scheme()).toBe(SCHEME.light);
  });

  it("tells herdr nothing when the colors stay the same", async () => {
    const full = await liveFull();
    full.notify();
    setTerminalAppearance("light"); // the page is light already
    expect(input()).toBe("");
    pageDark(true);
    await settle();
    expect(input()).toBe(""); // and now it does not follow the page
  });

  it("recolors without telling herdr when it did not ask", async () => {
    const full = await liveFull();
    setTerminalAppearance("dark");
    expect(input()).toBe("");
    expect(full.feed.snapshot().palette?.background).toBe(DARK.page);
    expect(full.scheme()).toBe(SCHEME.dark);
  });
});

describe("Single pane under the Appearance", () => {
  async function pane() {
    const feed = new TerminalFeed("m1", "s1");
    feeds.push(feed);
    await feed.mount(document.body.appendChild(document.createElement("div")), document.createElement("div"));
    return feed;
  }

  it("follows the page under Match page", async () => {
    const feed = await pane();
    expect(feed.snapshot().palette?.background).toBe(LIGHT.page);
    pageDark(true);
    await settle();
    expect(feed.snapshot().palette?.background).toBe(DARK.page);
    expect(terms.at(-1)!.options.theme).toMatchObject({ background: DARK.page });
  });

  it("keeps its own scheme whatever the page's, and takes a new one at once", async () => {
    localStorage.setItem("persona.terminal.appearance", "dark");
    const feed = await pane();
    expect(feed.snapshot().palette?.background).toBe(DARK.page);
    pageDark(true);
    await settle();
    pageDark(false);
    await settle();
    expect(feed.snapshot().palette?.background).toBe(DARK.page);
    setTerminalAppearance("light");
    expect(feed.snapshot().palette?.background).toBe(LIGHT.page);
    expect(terms.at(-1)!.options.theme).toMatchObject({ background: LIGHT.page });
  });
});
