// @vitest-environment jsdom
// The To-dos page's fourth grouping, "By request": the requests graph in place of the list (no search or filter, which
// have nothing to narrow there), the whole width to the graph until a piece is picked, then that to-do's own detail
// beside it, which closes again; the choice is kept per browser, and offered only while some request is in pieces.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { initI18n, t } from "../i18n";
import type { Todo } from "../lib/todos";

vi.hoisted(() => {
  globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as unknown as typeof ResizeObserver;
  window.matchMedia = ((media: string) => ({
    matches: false, media, onchange: null,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false,
  })) as typeof window.matchMedia;
});

vi.mock("../lib/direction-tree", async (orig) => ({ ...(await orig<typeof import("../lib/direction-tree")>()), useDirectionTree: () => null }));
vi.mock("../lib/api", async (orig) => ({ ...(await orig<typeof import("../lib/api")>()), api: async () => ({}) }));

const { getState } = await import("../lib/store");
const { TodosView } = await import("./Todos");
await import("./TodoRequestGraph"); // its chunk is ready when the lazy view asks

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

const at = "2026-11-10T00:00:00Z";
const todo = (id: string, title: string, extra: Partial<Todo> = {}): Todo => ({ id, title, created_at: at, updated_at: at, ...extra });
const TODOS = [todo("req", "Search page"), todo("api", "API fields", { part_of: "req" }), todo("ui", "Front end", { part_of: "req", after: ["api"] })];

const seed = (patch: Partial<ReturnType<typeof getState>>) => Object.assign(getState(), patch);
const before = { ...getState() };
let host: HTMLDivElement, root: Root;
// the graph is a lazy chunk
const graphLoaded = () => vi.waitFor(async () => {
  await act(async () => { await new Promise((r) => setTimeout(r, 10)); });
  expect(host.querySelector("[data-graph]")).not.toBeNull();
});
const option = (label: string) => [...host.querySelectorAll<HTMLElement>("[role=radio]")].find((b) => b.textContent?.trim() === label);
const detail = () => host.querySelector<HTMLElement>('[data-testid="todos-detail-column"]')!;

beforeEach(async () => {
  await initI18n("en");
  localStorage.clear();
  seed({ todos: TODOS, todosLoaded: true, todosError: null, todosSupported: true, machines: [], sessions: {} });
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => root.render(<TodosView />));
});

afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  seed(before);
  localStorage.clear();
});

describe("To-dos grouped by request", () => {
  it("replaces the list with the graph, without search, and gives the detail column to the page only once a piece is picked", async () => {
    expect(host.querySelector("[data-graph]")).toBeNull();
    expect(host.querySelector('input[type="search"], input[placeholder]')).not.toBeNull();
    await act(async () => option(t("todos.grouping_request"))!.click());
    expect(localStorage.getItem("persona.todos.grouping")).toBe("request");
    await graphLoaded();
    expect(host.querySelector('[data-request="todo:req"]')).not.toBeNull();
    expect(host.querySelector('input[placeholder]')).toBeNull();
    // the detail is not opened for the list's first to-do here
    expect(detail().className).toContain("md:hidden");
    await act(async () => host.querySelector<HTMLButtonElement>('[data-node="todo:ui"]')!.click());
    expect(detail().className).not.toContain("md:hidden");
    expect(detail().querySelector<HTMLInputElement>("input[type=text]")?.value).toBe("Front end");
    expect(host.querySelector('[data-node="todo:ui"]')!.getAttribute("aria-current")).toBe("true");
    await act(async () => detail().querySelector<HTMLButtonElement>('button[aria-label="Close"]')!.click());
    expect(detail().className).toContain("md:hidden");
    expect(host.querySelector('[data-node="todo:ui"]')!.hasAttribute("aria-current")).toBe(false);
  });

  it("is the grouping this browser comes back to", async () => {
    localStorage.setItem("persona.todos.grouping", "request");
    await act(async () => root.render(<div />));
    await act(async () => root.render(<TodosView />));
    await graphLoaded();
    expect(option(t("todos.grouping_request"))!.getAttribute("aria-checked")).toBe("true");
  });

  it("is offered only while some request is in pieces; a saved choice goes by status meanwhile and comes back", async () => {
    localStorage.setItem("persona.todos.grouping", "request");
    seed({ todos: [todo("a", "Buy milk"), todo("b", "Renew the passport")] });
    await act(async () => root.render(<div />));
    await act(async () => root.render(<TodosView />));
    expect(option(t("todos.grouping_request"))).toBeUndefined();
    expect(option(t("todos.grouping_status"))!.getAttribute("aria-checked")).toBe("true");
    expect(host.querySelector("[data-graph]")).toBeNull();
    expect(localStorage.getItem("persona.todos.grouping")).toBe("request");
    seed({ todos: TODOS });
    await act(async () => root.render(<div />));
    await act(async () => root.render(<TodosView />));
    await graphLoaded();
    expect(option(t("todos.grouping_request"))!.getAttribute("aria-checked")).toBe("true");
  });
});
