// @vitest-environment jsdom
// Another assistant made current while a page stays (lib/persona-nav.ts reopensOnSwitch): Scheduled tasks and the
// knowledge base read again as the new one (their requests name it), nothing of the last one stays on screen or in a
// request, and a kept pick whose manager left the roster reads again as the default once the roster says so.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {} });
});

import { chooseManager, resetManagerChoice } from "../lib/manager-choice";
import { resetRoster, setRoster, type ManagerList } from "../lib/managers";
import type { Route } from "../lib/router";
import { Routines } from "./assistant/Routines";
import { lists, searches } from "./knowledge/cache";
import { Knowledge } from "./knowledge/Knowledge";
import { PerAssistant } from "./PerAssistant";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const ROSTER: ManagerList = {
  default: "assistant",
  managers: [
    { id: "assistant", name: "助手", online: true, builtin: true },
    { id: "m-2", name: "工作", online: true },
  ],
};

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
// whose a request is: the manager it names, "default" for none
type Call = { method: string; path: string; manager: string };
let calls: Call[];
// managers the hub no longer has: their requests answer 404 no_manager
const gone = new Set<string>();
// requests held back until released (a slow answer): by manager
const held = new Map<string, (() => void)[]>();
let hold = "";

beforeEach(() => {
  calls = [];
  gone.clear();
  held.clear();
  hold = "";
  lists.clear();
  searches.clear();
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init: RequestInit = {}) => {
    const url = new URL(String(input), "http://hub.example.com");
    const manager = new Headers(init.headers).get("X-Persona-Manager") ?? "default";
    calls.push({ method: init.method ?? "GET", path: url.pathname, manager });
    const reply = (): Response => {
      if (gone.has(manager)) return json({ error: "no such manager", code: "no_manager" }, 404);
      if (url.pathname === "/api/v1/assistant/tasks") return json({ tasks: [{ id: `task-${manager}`, title: `Task of ${manager}`, prompt: "p", repeat: "daily", time: "09:00", enabled: true }] });
      if (url.pathname === "/api/v1/assistant/runs") return json({ runs: [] });
      // the store reads the new assistant's page when the pick changes (lib/store.ts)
      if (url.pathname === "/api/v1/assistant") return json({ conversations: [] });
      if (url.pathname === "/api/v1/kb/docs") return json({ items: [{ kind: "docs", id: `doc-${manager}`, name: `Doc of ${manager}`, relevance: 0 }], tags: [] });
      throw new Error(`Unexpected request: ${url.pathname}`);
    };
    if (hold && manager === hold) return new Promise<Response>((resolve) => held.set(manager, [...(held.get(manager) ?? []), () => resolve(reply())]));
    return Promise.resolve(reply());
  }));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  resetRoster();
  resetManagerChoice();
  lists.clear();
  searches.clear();
});

let host: HTMLDivElement;
let root: Root;
const flush = () => act(async () => {
  for (let i = 0; i < 10; i++) await new Promise((resolve) => setTimeout(resolve, 0));
});
const page = (route: Route) => (route.name === "routines" ? <Routines onToggleSidebar={() => {}} /> : <Knowledge kind="docs" onToggleSidebar={() => {}} />);
const render = async (route: Route) => {
  await act(async () => root.render(<PerAssistant route={route}>{page(route)}</PerAssistant>));
  await flush();
};
const switchTo = async (id: string | null) => {
  await act(async () => chooseManager(id));
  await flush();
};
const reads = (path: string) => calls.filter((c) => c.method === "GET" && c.path === path).map((c) => c.manager);

describe("a page that stays while another assistant becomes current", () => {
  it("reads Scheduled tasks again as the new assistant, and shows nothing of the last one's", async () => {
    setRoster(ROSTER);
    await render({ name: "routines" });
    expect(reads("/api/v1/assistant/tasks")).toEqual(["default"]);
    expect(host.textContent).toContain("Task of default");

    await switchTo("m-2");
    expect(reads("/api/v1/assistant/tasks")).toEqual(["default", "m-2"]);
    expect(reads("/api/v1/assistant/runs")).toEqual(["default", "m-2"]);
    expect(host.textContent).toContain("Task of m-2");
    expect(host.textContent).not.toContain("Task of default");

    // and back: the default's again, with no header
    await switchTo(null);
    expect(reads("/api/v1/assistant/tasks").at(-1)).toBe("default");
    expect(host.textContent).toContain("Task of default");
  });

  it("reads the knowledge base again as the new assistant, never painting the last one's cached list", async () => {
    setRoster(ROSTER);
    await render({ name: "kb", kind: "docs" });
    expect(reads("/api/v1/kb/docs")).toEqual(["default"]);
    expect(host.textContent).toContain("Doc of default");

    // the new assistant's answer is slow: until it comes the list is loading, not the default's list from the cache
    hold = "m-2";
    await switchTo("m-2");
    expect(reads("/api/v1/kb/docs")).toEqual(["default", "m-2"]);
    expect(host.textContent).not.toContain("Doc of default");
    expect(host.textContent).toContain("加载中");
    await act(async () => held.get("m-2")!.forEach((go) => go()));
    await flush();
    expect(host.textContent).toContain("Doc of m-2");

    // back to the default: its cached list paints at once (it is its own) and is read again
    hold = "";
    await switchTo(null);
    expect(host.textContent).toContain("Doc of default");
    expect(host.textContent).not.toContain("Doc of m-2");
    expect(reads("/api/v1/kb/docs").at(-1)).toBe("default");
  });

  it("reads again as the default when the kept pick's manager left the roster", async () => {
    // a reload with a pick kept from before, whose manager has been deleted since
    await act(async () => chooseManager("m-gone"));
    gone.add("m-gone");
    await render({ name: "routines" });
    expect(reads("/api/v1/assistant/tasks")).toEqual(["m-gone"]);
    expect(host.textContent).not.toContain("Task of");

    // the roster comes without it: back to the default, and the page reads again
    await act(async () => setRoster(ROSTER));
    await flush();
    expect(reads("/api/v1/assistant/tasks")).toEqual(["m-gone", "default"]);
    expect(host.textContent).toContain("Task of default");
  });

  it("does the same for the knowledge base", async () => {
    await act(async () => chooseManager("m-gone"));
    gone.add("m-gone");
    await render({ name: "kb", kind: "docs" });
    expect(reads("/api/v1/kb/docs")).toEqual(["m-gone"]);
    await act(async () => setRoster(ROSTER));
    await flush();
    expect(reads("/api/v1/kb/docs")).toEqual(["m-gone", "default"]);
    expect(host.textContent).toContain("Doc of default");
  });
});
