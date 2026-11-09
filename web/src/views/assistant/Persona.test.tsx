// @vitest-environment jsdom
// The look store against a hub that keeps the choice (GET /looks, PUT /looks/current, #3200): the hub's choice wins,
// this browser's old one moves up once when the hub has none, choosing goes to the hub, and another device's choice
// arrives as a "look" push.
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { RETIRED_PERSONAS, type LookCurrent, type LookView } from "../../lib/persona";

const hub = vi.hoisted(() => ({
  looks: [] as unknown[],
  current: { id: null, effective: "persona" } as { id: string | null; effective: string },
  puts: [] as unknown[],
  listeners: new Map<string, (m: Record<string, unknown>) => void>(),
  /** When set, GET /looks waits for it (a read still on its way). */
  hold: null as Promise<void> | null,
  old: false,
}));
vi.mock("../../lib/api", async (orig) => {
  const real = await orig<typeof import("../../lib/api")>();
  return {
    ...real,
    getToken: () => "",
    api: vi.fn((path: string, init?: { method?: string; json?: { id: string; if_unset?: boolean } }) => {
      if (path === "/looks") {
        if (hub.old) return Promise.reject(new real.ApiError(404, "not found", {}));
        // the answer is what the hub had when the request left
        const answer = { looks: hub.looks, current: hub.current };
        return (hub.hold ?? Promise.resolve()).then(() => answer);
      }
      if (path === "/looks/current" && init?.method === "PUT") {
        hub.puts.push(init.json);
        const body = init.json!;
        if (!(body.if_unset && hub.current.id)) hub.current = { id: body.id, effective: body.id };
        return Promise.resolve(hub.current);
      }
      if (path === "/personas") return Promise.resolve({ personas: hub.looks.filter((l) => (l as LookView).manifest).map((l) => {
        const look = l as LookView;
        return { id: look.id, rev: look.rev, manifest: look.manifest };
      }) });
      return Promise.reject(new real.ApiError(404, "not found", {}));
    }),
  };
});
vi.mock("../../lib/store", () => ({
  onHubEvent: (type: string, l: (m: Record<string, unknown>) => void) => hub.listeners.set(type, l),
  // a hub without uploads (#2404): the picker offers none
  hubHas: (features: string[], feature: string) => features.includes(feature),
  useStore: <T,>(select: (s: { features: string[] }) => T) => select({ features: [] }),
}));
vi.stubGlobal("Image", class { onload: (() => void) | null = null; onerror: (() => void) | null = null; set src(_: string) { queueMicrotask(() => this.onerror?.()); } });

const aurora: LookView = {
  id: "aurora", source: "extension", name: "Aurora", names: { ja: "オーロラ" }, app_icon: "persona", extension: "example-looks", extension_name: "Example Looks",
  rev: "0123456789abcdef",
  manifest: {
    format: 1, id: "aurora", name: "Aurora", names: { ja: "オーロラ" },
    avatar: { sizes: { "128": "a/128.png" }, svg: "a/a.svg", dark: { svg: "a/dark.svg" } },
    dot: { states: { idle: { file: "a/idle.png" } } },
    source: { kind: "extension", extension: "example-looks", extension_name: "Example Looks" },
  },
};
const builtins: LookView[] = [{ id: "persona", source: "builtin", name: "Mochi", app_icon: "persona" }];
const meadow: LookView = { ...aurora, id: "meadow", manifest: { ...aurora.manifest!, id: "meadow", name: "Meadow" } };
const frog: LookView = { ...aurora, id: "nwverse", source: "hub", name: "Milk Frog", extension: undefined, extension_name: undefined,
  manifest: { ...aurora.manifest!, id: "nwverse", name: "Milk Frog", names: undefined, source: { kind: "hub" } } };

async function load() {
  vi.resetModules();
  const { initI18n } = await import("../../i18n");
  await initI18n("en");
  return import("./Persona");
}

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root | null = null;
let host: HTMLDivElement | null = null;
async function draw(p: Awaited<ReturnType<typeof load>>, picker = false) {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  function Probe() {
    const current = p.usePersona();
    return <><output data-builtin={current.builtin} data-package={current.info?.id} />
      {picker ? <p.PersonaPicker /> : <><p.PersonaAvatar size={40} /><p.PersonaDot size={40} state="needs_you" /></>}
    </>;
  }
  await act(async () => root!.render(<Probe />));
  return host;
}

function expectDefault(node: HTMLElement) {
  expect(node.querySelector("output")?.getAttribute("data-builtin")).toBe("persona");
  expect(node.querySelector("output")?.hasAttribute("data-package")).toBe(false);
  expect(node.querySelectorAll("svg")).toHaveLength(2);
  expect(node.querySelectorAll("svg path").length).toBeGreaterThan(0);
  expect(node.querySelector("img")).toBeNull();
}

describe("the look the hub keeps", () => {
  // The first import transforms Persona's whole module graph, which takes seconds when the full suite loads the
  // machine; doing it once here keeps that cost out of the first test's 5 s budget. load() still resets modules.
  beforeAll(() => import("./Persona"), 60_000);
  beforeEach(() => {
    localStorage.clear();
    hub.looks = [...builtins, aurora, meadow];
    hub.current = { id: null, effective: "persona" };
    hub.puts = [];
    hub.hold = null;
    hub.old = false;
  });
  afterEach(() => {
    if (root) act(() => root!.unmount());
    host?.remove();
    root = null;
    host = null;
    localStorage.clear();
  });

  it("moves this browser's old choice up once when the hub has none", async () => {
    localStorage.setItem("persona.persona", "aurora");
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([{ id: "aurora", if_unset: true }]);
    expect(hub.current.id).toBe("aurora");
  });

  it("takes the hub's choice over this browser's", async () => {
    localStorage.setItem("persona.persona", "persona");
    hub.current = { id: "aurora", effective: "aurora" } satisfies LookCurrent;
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
  });

  it("does not move anything up from a browser that never chose", async () => {
    const p = await load();
    await p.refreshPersonas();
    expect(hub.puts).toEqual([]);
  });

  it("sends a choice to the hub, and follows another device's", async () => {
    const p = await load();
    await p.refreshPersonas();
    p.choosePersona("meadow");
    expect(hub.puts).toEqual([{ id: "meadow" }]);
    expect(localStorage.getItem("persona.persona")).toBe("meadow");
    hub.listeners.get("look")!({ type: "look", id: "aurora", effective: "aurora" });
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
    hub.listeners.get("look")!({ type: "look", id: "persona", effective: "persona" });
    expect(localStorage.getItem("persona.persona")).toBeNull();
  });

  // #4682
  const holdReads = () => {
    let release!: () => void;
    hub.hold = new Promise<void>((r) => (release = r));
    return () => {
      hub.hold = null;
      release();
    };
  };

  it("keeps a choice made while the list is being read", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const release = holdReads();
    const reading = p.refreshPersonas(); // the picker opens
    p.choosePersona("meadow");
    expect(hub.current.id).toBe("meadow");
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("meadow");
  });

  it("keeps another device's choice pushed while the list is being read", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const release = holdReads();
    const reading = p.refreshPersonas();
    hub.current = { id: "meadow", effective: "meadow" };
    hub.listeners.get("look")!({ type: "look", id: "meadow", effective: "meadow" });
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("meadow");
  });

  it("sends a choice made before the first answer once it comes", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas(); // the page's first read
    p.choosePersona("meadow");
    expect(hub.puts).toEqual([]); // not known yet whether this hub keeps it
    release();
    await reading;
    expect(hub.puts).toEqual([{ id: "meadow" }]);
    expect(hub.current.id).toBe("meadow");
    expect(localStorage.getItem("persona.persona")).toBe("meadow");
  });

  it("keeps the look a push confirms while the list is being read, even one this page already shows", async () => {
    hub.current = { id: "persona", effective: "persona" };
    localStorage.setItem("persona.persona", "aurora");
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas(); // its answer will say persona
    hub.current = { id: "aurora", effective: "aurora" };
    hub.listeners.get("look")!({ type: "look", id: "aurora", effective: "aurora" });
    release();
    await reading;
    expect(localStorage.getItem("persona.persona")).toBe("aurora");
  });

  it("sends a choice made before the first answer once, when the hub has none yet", async () => {
    const p = await load();
    const release = holdReads();
    const reading = p.refreshPersonas();
    p.choosePersona("meadow");
    release();
    await reading;
    expect(hub.puts).toEqual([{ id: "meadow" }]);
    expect(hub.current.id).toBe("meadow");
  });

  it.each(RETIRED_PERSONAS)("drops saved retired choice %s before the first request", async (id) => {
    localStorage.setItem("persona.persona", id);
    const p = await load();
    expect(localStorage.getItem("persona.persona")).toBeNull();
    expectDefault(await draw(p));
    expect(hub.puts).toEqual([]);
  });

  it.each(RETIRED_PERSONAS)("falls back for hub choice %s and prevents extension takeover", async (id) => {
    hub.looks = [...builtins, frog, { ...aurora, id, manifest: { ...aurora.manifest!, id } }];
    hub.current = { id, effective: id };
    const p = await load();
    await p.refreshPersonas();
    expect(localStorage.getItem("persona.persona")).toBeNull();
    const node = await draw(p, true);
    expect(node.querySelector("output")?.getAttribute("data-builtin")).toBe("persona");
    const radios = [...node.querySelectorAll('[role="radio"]')];
    expect(radios.map((n) => n.getAttribute("aria-label"))).toEqual(["Mochi, the default avatar", "Milk Frog"]);
    expect(radios[0].getAttribute("aria-checked")).toBe("true");
  });

  it.each(RETIRED_PERSONAS)("defaults a pushed retired choice %s without a blank face", async (id) => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const node = await draw(p);
    await act(async () => hub.listeners.get("look")!({ id, effective: id }));
    expectDefault(node);
    expect(localStorage.getItem("persona.persona")).toBeNull();
  });

  it("shows the default for an unknown saved or hub choice and unsupported app icon", async () => {
    localStorage.setItem("persona.persona", "unknown-look");
    hub.current = { id: "unknown-look", effective: "unknown-look" };
    hub.looks = [...builtins, { id: "unknown-look", source: "builtin", name: "Unknown", app_icon: "unsupported-icon" }];
    const p = await load();
    await p.refreshPersonas();
    expectDefault(await draw(p));
  });

  it("ignores unknown app_icon hints without blocking a valid extension package", async () => {
    hub.looks = [...builtins, { ...aurora, app_icon: "unsupported-icon" }];
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const node = await draw(p);
    expect(node.querySelector("output")?.getAttribute("data-package")).toBe("aurora");
    expect(node.querySelector("img")?.getAttribute("src")).toContain("/personas/aurora/a/a.svg");
  });

  it("defaults unknown pushed choices and retired choices on older hubs", async () => {
    hub.old = true;
    localStorage.setItem("persona.persona", RETIRED_PERSONAS[0]);
    const p = await load();
    await p.refreshPersonas();
    const node = await draw(p);
    expectDefault(node);
    await act(async () => hub.listeners.get("look")!({ id: "unknown-look" }));
    expectDefault(node);
    expect(hub.puts).toEqual([]);
  });

  it("uses the default for a package image that fails to load", async () => {
    hub.current = { id: "aurora", effective: "aurora" };
    const p = await load();
    await p.refreshPersonas();
    const node = await draw(p);
    await act(async () => node.querySelectorAll("img").forEach((img) => img.dispatchEvent(new Event("error"))));
    expect(node.querySelectorAll("svg")).toHaveLength(2);
    expect(node.querySelector("img")).toBeNull();
  });
});
