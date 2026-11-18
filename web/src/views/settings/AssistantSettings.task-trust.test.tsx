// @vitest-environment jsdom
import { act, StrictMode } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

vi.hoisted(() => { window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia; });
vi.mock("../../ui/Markdown", () => ({ Markdown: () => null }));
vi.mock("../../lib/managers", () => ({
  useRoster: () => ({ default: "assistant", managers: [{ id: "assistant", name: "Personal" }, { id: "work", name: "Work" }] }),
  managerOrder: (roster: { managers: unknown[] }) => roster.managers,
}));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }), PersonaAvatar: () => null }));
vi.mock("./Skills", () => ({ SkillsGroup: () => null }));
vi.mock("../../lib/store", () => ({ useStore: () => [], refreshAssistant: vi.fn() }));

import { initI18n } from "../../i18n";
import { handleUnauthorized, setToken } from "../../lib/api";
import { chooseManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { AssistantSettings } from "./AssistantSettings";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root;
let host: HTMLDivElement;
type Call = { path: string; init: RequestInit; answer: (body: unknown, status?: number) => void; reject: (error: Error) => void };
let trustCalls: Call[];
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const first = { key: "synthetic-box|/sample/a & b|read", machine: "synthetic-box", cwd: "/sample/a & b", readonly: true };
const second = { key: "synthetic-box|/sample/edit folder|edit", machine: "synthetic-box", cwd: "/sample/edit folder", readonly: false };
const ambiguousKey = "synthetic-box|/sample/a|b & c|read";
const ambiguous = { key: ambiguousKey, machine: "", cwd: encodeURIComponent(ambiguousKey), readonly: true, ambiguous: true };
const reads = () => trustCalls.filter((call) => !call.init.method || call.init.method === "GET");
const deletes = () => trustCalls.filter((call) => call.init.method === "DELETE");
const text = () => document.body.textContent ?? "";
const buttons = () => [...document.querySelectorAll<HTMLButtonElement>("button")].filter((button) => !button.closest("[data-closing]"));
const button = (label: string) => buttons().find((candidate) => candidate.textContent?.trim() === label)!;
const click = async (label: string) => act(async () => { expect(button(label), label).toBeTruthy(); button(label).click(); });
const answer = async (call: Call, body: unknown, status = 200) => act(async () => call.answer(body, status));
const row = (identifier: string) => [...host.querySelectorAll<HTMLElement>("[data-task-trust-rule]")].find((element) => element.textContent?.includes(identifier))!;
const clickRow = async (identifier: string) => act(async () => { const node = row(identifier); expect(node).toBeTruthy(); node.querySelector("button")!.click(); });
const dialog = () => document.querySelector<HTMLElement>('[role="dialog"][aria-modal="true"]')!;
const render = async () => act(async () => root.render(<AssistantSettings />));
const open = async () => click("Manage");
const load = async (rules: unknown[] = [first, second]) => { await render(); await open(); await answer(reads().at(-1)!, { kinds: rules }); };
const confirm = async (identifier = first.cwd) => { await clickRow(identifier); await click("Remove rule"); };

beforeEach(async () => {
  await initI18n("en");
  resetManagerChoice(); setRosterDefault("assistant"); setToken("synthetic-a");
  trustCalls = [];
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).split("/api/v1")[1];
    if (path.startsWith("/task-trust")) return new Promise<Response>((resolve, reject) => {
      trustCalls.push({ path, init, answer: (body, status = 200) => resolve(json(body, status)), reject });
    });
    if (path === "/assistant") return Promise.resolve(json({ manager: new Headers(init.headers).get("X-Persona-Manager"), notes: "" }));
    if (path === "/assistant/tasks") return Promise.resolve(json({ tasks: [] }));
    if (path === "/assistant/memory") return Promise.resolve(json({ files: [] }));
    throw new Error(`Unexpected path: ${path}`);
  }));
});
afterEach(async () => { act(() => root.unmount()); host.remove(); document.body.innerHTML = ""; handleUnauthorized(() => {}); vi.unstubAllGlobals(); setToken(""); resetManagerChoice(); await initI18n("zh-Hans"); });

it("offers remembered task rules from the actual assistant settings without changing the default", async () => {
  await act(async () => root.render(<AssistantSettings />));
  expect(host.textContent).toContain("Remembered auto-start rules");
  expect(trustCalls).toHaveLength(0);
});

it("shows loading and an honest empty response, and refreshes without changing any default", async () => {
  await render(); await open();
  expect(host.querySelector('[role="status"]')).toBeTruthy();
  expect(text()).not.toContain("No remembered rules were returned.");
  await answer(reads()[0], { kinds: [] });
  expect(text()).toContain("No remembered rules were returned.");
  expect(text()).toContain("tick the line above Start on a matching plan");
  await click("Refresh rules");
  expect(button("Refresh rules").disabled).toBe(true);
  await answer(reads()[1], { kinds: [first, second] });
  expect(text()).toContain(first.machine); expect(text()).toContain(first.cwd); expect(text()).toContain("Read-only");
  expect(text()).toContain(second.cwd); expect(text()).toContain("Can edit files");
  expect(deletes()).toHaveLength(0);
});

it("shows unresolved locations and the exact identifier without presenting an ambiguous key as a directory", async () => {
  await load([ambiguous, second]);
  const unresolved = row(ambiguous.key);
  expect(unresolved.textContent).toContain("could not be separated");
  expect(unresolved.textContent).toContain(`Rule identifier: ${ambiguous.key}`);
  expect(unresolved.textContent).toContain("Read-only");
  expect(unresolved.textContent).not.toContain("Directory:");
  expect(unresolved.textContent).not.toContain(ambiguous.cwd);
  await clickRow(ambiguous.key);
  expect(dialog().textContent).toContain(ambiguous.key);
  expect(dialog().textContent).not.toContain("Directory:");
  await click("Remove rule");
  expect(deletes()[0].path).toBe(`/task-trust?key=${encodeURIComponent(ambiguous.key)}`);
  await answer(deletes()[0], { ok: true });
  await answer(reads().at(-1)!, { kinds: [second] });
  expect(row(ambiguous.key)).toBeUndefined();
});

it("confirms the exact rule, respects cancellation, admits one deletion and permits a newly remembered rule", async () => {
  await load(); await clickRow(first.cwd);
  expect(dialog().textContent).toContain(first.machine);
  expect(dialog().textContent).toContain(first.cwd);
  expect(dialog().textContent).toContain("Read-only");
  expect(dialog().textContent).toContain("Other settings may still start tasks automatically");
  expect(dialog().textContent).toContain("A matching plan can be remembered again");
  const cancelled = button("Remove rule");
  await click("Cancel");
  await act(async () => cancelled.click());
  expect(deletes()).toHaveLength(0);
  await clickRow(first.cwd);
  await act(async () => { button("Remove rule").click(); button("Remove rule").click(); });
  expect(deletes()).toHaveLength(1);
  expect(deletes()[0].path).toBe(`/task-trust?key=${encodeURIComponent(first.key)}`);
  expect(new Headers(deletes()[0].init.headers).get("X-Persona-Manager")).toBe("assistant");
  expect(new Headers(deletes()[0].init.headers).get("Authorization")).toBe("Bearer synthetic-a");
  await answer(deletes()[0], { ok: true });
  expect(row(first.cwd)).toBeUndefined(); expect(row(second.cwd)).toBeTruthy();
  await answer(reads().at(-1)!, { kinds: [second] });
  await click("Refresh rules"); await answer(reads().at(-1)!, { kinds: [first, second] });
  expect(row(first.cwd)).toBeTruthy();
});

it("gives the read-only and edit actions distinct spoken names for the same computer and directory", async () => {
  const edit = { ...first, key: "synthetic-box|/sample/a & b|edit", readonly: false };
  await load([first, edit]);
  const names = [...host.querySelectorAll<HTMLElement>("[data-task-trust-rule] button")].map((node) => node.textContent?.replace(/\s+/g, " ").trim());
  expect(names).toEqual([
    `Remove… ${first.machine} ${first.cwd} Read-only`,
    `Remove… ${first.machine} ${first.cwd} Can edit files`,
  ]);
});

it.each([
  [403, "don't have access"], [404, "HTTP 404"], [503, "couldn't load these rules right now"], [500, "Couldn't load the remembered rules"],
])("reports initial read failure %s and retries it", async (status, message) => {
  await render(); await open(); await answer(reads()[0], {}, status);
  expect(text()).toContain(message); expect(text()).not.toContain("No remembered rules were returned.");
  await click("Refresh rules"); await answer(reads()[1], { kinds: [first] });
  expect(text()).not.toContain(message); expect(row(first.cwd)).toBeTruthy();
});

it("keeps rows after a failed refresh and rejects malformed success responses", async () => {
  await load([first]);
  await click("Refresh rules"); await answer(reads().at(-1)!, {}, 503);
  expect(row(first.cwd)).toBeTruthy(); expect(text()).toContain("couldn't load these rules right now");
  await click("Refresh rules"); await answer(reads().at(-1)!, {});
  expect(row(first.cwd)).toBeTruthy(); expect(text()).toContain("Couldn't load the remembered rules");
  await click("Refresh rules"); await answer(reads().at(-1)!, { kinds: [first] });
  expect(text()).not.toContain("Couldn't load the remembered rules");
});

it.each([
  [403, "Full access is required"], [404, "The rule or removal endpoint was not found"],
  [503, "Removal is unconfirmed"], [500, "The rule may still apply"],
])("retains the exact failed removal after status %s and a refresh that omits it", async (status, message) => {
  await load([first]); await confirm(); await answer(deletes()[0], {}, status);
  await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd).textContent).toContain(message);
  expect(row(first.cwd).textContent).toContain("Retry removal");
  await confirm();
  expect(deletes()[1].path).toBe(`/task-trust?key=${encodeURIComponent(first.key)}`);
  await answer(deletes()[1], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd)).toBeUndefined();
});

it.each(["network", "empty-acknowledgement"])("keeps an uncertain removal for retry after %s", async (failure) => {
  await load([first]); await confirm();
  if (failure === "network") await act(async () => deletes()[0].reject(new TypeError("synthetic network failure")));
  else await answer(deletes()[0], {});
  await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd).textContent).toContain("The rule may still apply");
  await confirm(); expect(deletes()[1].path).toBe(deletes()[0].path);
  await answer(deletes()[1], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("lets the user dismiss an uncertain warning after the server deleted the rule but its reply was lost", async () => {
  const originalFetch = globalThis.fetch;
  let stored = [first];
  const sent: string[] = [];
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = String(input).split("/api/v1")[1];
    if (!path.startsWith("/task-trust")) return originalFetch(input, init);
    if (init.method !== "DELETE") return Promise.resolve(json({ kinds: stored }));
    sent.push(path);
    if (stored.some((rule) => rule.key === new URL(`http://localhost${path}`).searchParams.get("key"))) {
      stored = []; // The server commits before the connection drops.
      return Promise.reject(new TypeError("synthetic lost reply"));
    }
    return Promise.resolve(json({ error: "not remembered" }, 404));
  }));
  await render(); await open(); expect(row(first.cwd)).toBeTruthy();
  await confirm(); expect(stored).toEqual([]);
  expect(row(first.cwd).textContent).toContain("The rule may still apply");
  await confirm(); expect(sent).toEqual([`/task-trust?key=${encodeURIComponent(first.key)}`, `/task-trust?key=${encodeURIComponent(first.key)}`]);
  expect(row(first.cwd).textContent).toContain("rule or removal endpoint was not found");
  expect(row(first.cwd).textContent).toContain("Dismissing this warning does not confirm removal");
  await act(async () => [...row(first.cwd).querySelectorAll("button")].find((node) => node.textContent?.startsWith("Dismiss warning"))!.click());
  expect(row(first.cwd)).toBeUndefined();
  expect(text()).toContain("No remembered rules were returned.");
  stored = [first]; // A matching plan can remember it again.
  await click("Refresh rules");
  expect(row(first.cwd).textContent).toContain("Remove…");
  expect(row(first.cwd).textContent).not.toContain("Dismiss warning");
});

it("does not dismiss a previous assistant's warning after an assistant switch", async () => {
  await load([first]); await confirm(); await answer(deletes()[0], {}, 503); await answer(reads().at(-1)!, { kinds: [] });
  const old = [...row(first.cwd).querySelectorAll("button")].find((node) => node.textContent?.startsWith("Dismiss warning"))!;
  await act(async () => { chooseManager("work"); old.click(); });
  await open(); await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd)).toBeUndefined();
  await act(async () => chooseManager(null)); await open(); await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd).textContent).toContain("Dismiss warning");
});

it("can dismiss a warning while its refresh is pending without accepting that old read", async () => {
  await load([first]); await confirm(); await answer(deletes()[0], {}, 503);
  const old = reads().at(-1)!;
  await act(async () => [...row(first.cwd).querySelectorAll("button")].find((node) => node.textContent?.startsWith("Dismiss warning"))!.click());
  expect(old.init.signal?.aborted).toBe(true);
  await answer(old, { kinds: [] });
  expect(row(first.cwd).textContent).not.toContain("Dismiss warning");
  await click("Refresh rules"); await answer(reads().at(-1)!, { kinds: [first] });
  expect(row(first.cwd)).toBeTruthy();
});

it("retires a confirmation when a refresh starts", async () => {
  await load([first]); await clickRow(first.cwd);
  const old = button("Remove rule");
  await click("Refresh rules");
  await act(async () => old.click()); expect(deletes()).toHaveLength(0);
  await answer(reads().at(-1)!, { kinds: [first] });
  await confirm(); expect(deletes()).toHaveLength(1);
  await answer(deletes()[0], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("does not start a refresh in the same batch as a confirmed write", async () => {
  await load([first]); await clickRow(first.cwd);
  const refresh = button("Refresh rules");
  await act(async () => { button("Remove rule").click(); refresh.click(); });
  expect(deletes()).toHaveLength(1); expect(reads()).toHaveLength(1);
  expect(dialog()).toBeTruthy();
  await answer(deletes()[0], { ok: true });
  expect(reads()).toHaveLength(2);
  await answer(reads()[1], { kinds: [] });
});

it("allows Escape to cancel a confirmation but keeps a pending write protected", async () => {
  await load([first]); await clickRow(first.cwd); const cancelled = button("Remove rule");
  await act(async () => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
  await act(async () => cancelled.click()); expect(deletes()).toHaveLength(0);
  await confirm();
  await act(async () => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true })));
  expect(dialog()).toBeTruthy(); expect(button("Remove rule").disabled).toBe(true);
  await answer(deletes()[0], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("aborts a closed list and ignores its late response after reopening", async () => {
  await render(); await open(); const old = reads()[0];
  await click("Hide"); expect(old.init.signal?.aborted).toBe(true);
  await open(); const fresh = reads().at(-1)!;
  await answer(fresh, { kinds: [second] });
  await answer(old, { kinds: [first] });
  expect(row(first.cwd)).toBeUndefined(); expect(row(second.cwd)).toBeTruthy();
});

it("uses the selected manager for both operations and ignores a previous manager's late read", async () => {
  await render(); await open(); const old = reads()[0];
  await act(async () => chooseManager("work"));
  expect(old.init.signal?.aborted).toBe(true);
  await open(); const fresh = reads().at(-1)!;
  expect(new Headers(fresh.init.headers).get("X-Persona-Manager")).toBe("work");
  await answer(fresh, { kinds: [second] }); await answer(old, { kinds: [first] });
  expect(row(first.cwd)).toBeUndefined(); expect(row(second.cwd)).toBeTruthy();
  await confirm(second.cwd);
  expect(new Headers(deletes()[0].init.headers).get("X-Persona-Manager")).toBe("work");
  expect(deletes()[0].path).toBe(`/task-trust?key=${encodeURIComponent(second.key)}`);
  await answer(deletes()[0], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it.each(["manager", "manager-return", "default-manager", "credential", "same-token", "navigation"])("retires an unsent confirmation on %s", async (mode) => {
  await load([first]); await clickRow(first.cwd); const old = button("Remove rule");
  await act(async () => {
    if (mode === "manager") chooseManager("work");
    if (mode === "manager-return") { chooseManager("work"); chooseManager(null); }
    if (mode === "default-manager") setRosterDefault("work");
    if (mode === "credential") setToken("synthetic-b");
    if (mode === "same-token") { setToken("synthetic-b"); setToken("synthetic-a"); }
    if (mode === "navigation") root.render(<p>Another page</p>);
    else old.click();
  });
  if (mode === "navigation") await act(async () => old.click());
  expect(deletes()).toHaveLength(0);
  if (mode !== "navigation") {
    await open(); await answer(reads().at(-1)!, { kinds: [second] });
    await confirm(second.cwd);
    const call = deletes()[0];
    expect(new Headers(call.init.headers).get("X-Persona-Manager")).toBe(mode === "manager" || mode === "default-manager" ? "work" : "assistant");
    expect(new Headers(call.init.headers).get("Authorization")).toBe(`Bearer ${mode === "credential" ? "synthetic-b" : "synthetic-a"}`);
    await answer(call, { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
  }
});

it("keeps a dispatched write single across a manager round trip and restores its failure only to its owner", async () => {
  await load([first]); await confirm(); const old = deletes()[0];
  await act(async () => chooseManager("work")); await open();
  await answer(reads().at(-1)!, { kinds: [second] });
  expect(row(first.cwd)).toBeUndefined();
  await act(async () => chooseManager(null)); await open();
  expect(row(first.cwd)).toBeTruthy(); expect(row(first.cwd).querySelector("button")!.disabled).toBe(true);
  await answer(old, {}, 503);
  await answer(reads().at(-1)!, { kinds: [] });
  expect(row(first.cwd).textContent).toContain("Removal is unconfirmed");
  await confirm(); expect(deletes()).toHaveLength(2);
  await answer(deletes()[1], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("keeps an uncertain in-flight removal across unmount and retries with the original manager and credential", async () => {
  await load([first]); await confirm(); const old = deletes()[0];
  await act(async () => root.render(<p>Another page</p>));
  const count = reads().length;
  await act(async () => old.reject(new TypeError("synthetic network failure")));
  expect(reads()).toHaveLength(count);
  await render(); await open();
  expect(row(first.cwd).textContent).toContain("The rule may still apply");
  await answer(reads().at(-1)!, { kinds: [] });
  await confirm();
  expect(deletes()).toHaveLength(2);
  expect(new Headers(deletes()[1].init.headers).get("X-Persona-Manager")).toBe("assistant");
  expect(new Headers(deletes()[1].init.headers).get("Authorization")).toBe("Bearer synthetic-a");
  await answer(deletes()[1], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("retires a cross-tab credential's pending read and does not accept its late response", async () => {
  await render(); await open(); const old = reads()[0];
  await act(async () => { localStorage.setItem("persona.token", "synthetic-b"); window.dispatchEvent(new StorageEvent("storage", { key: "persona.token" })); });
  expect(old.init.signal?.aborted).toBe(true);
  await open(); const fresh = reads().at(-1)!;
  expect(new Headers(fresh.init.headers).get("Authorization")).toBe("Bearer synthetic-b");
  await answer(fresh, { kinds: [second] }); await answer(old, { kinds: [first] });
  expect(row(first.cwd)).toBeUndefined(); expect(row(second.cwd)).toBeTruthy();
});

it("fences an old 401 and retained failure after credential replacement, but handles the new owner's 401", async () => {
  const unauthorized = vi.fn(); handleUnauthorized(unauthorized);
  await load([first]); await confirm(); const old = deletes()[0];
  await act(async () => setToken("synthetic-b")); await open();
  const fresh = reads().at(-1)!;
  await answer(old, {}, 401); expect(unauthorized).not.toHaveBeenCalled();
  expect(row(first.cwd)).toBeUndefined();
  await answer(fresh, {}, 401); expect(unauthorized).toHaveBeenCalledOnce();
  expect(new Headers(old.init.headers).get("Authorization")).toBe("Bearer synthetic-a");
  expect(new Headers(fresh.init.headers).get("Authorization")).toBe("Bearer synthetic-b");
});

it.each(["manager", "manager-return", "navigation"])("does not let a retired %s read sign out the current view", async (mode) => {
  const unauthorized = vi.fn(); handleUnauthorized(unauthorized);
  await render(); await open(); const old = reads()[0];
  await act(async () => {
    if (mode === "manager") chooseManager("work");
    if (mode === "manager-return") { chooseManager("work"); chooseManager(null); }
    if (mode === "navigation") root.render(<p>Another page</p>);
  });
  if (mode !== "navigation") await open();
  await answer(old, {}, 401);
  expect(unauthorized).not.toHaveBeenCalled();
  if (mode !== "navigation") {
    await answer(reads().at(-1)!, {}, 401);
    expect(unauthorized).toHaveBeenCalledOnce();
  }
});

it.each(["manager", "navigation"])("does not let a retired %s removal sign out the current view", async (mode) => {
  const unauthorized = vi.fn(); handleUnauthorized(unauthorized);
  await load([first]); await confirm(); const old = deletes()[0];
  await act(async () => {
    if (mode === "manager") chooseManager("work");
    else root.render(<p>Another page</p>);
  });
  await answer(old, {}, 401); expect(unauthorized).not.toHaveBeenCalled();
  if (mode === "manager") {
    await open(); await answer(reads().at(-1)!, {}, 401); expect(unauthorized).toHaveBeenCalledOnce();
  }
});

it("does not transfer an old write failure to a new generation of the same credential", async () => {
  await load([first]); await confirm(); const old = deletes()[0];
  await act(async () => { setToken("synthetic-b"); setToken("synthetic-a"); }); await open();
  const fresh = reads().at(-1)!;
  await answer(old, {}, 503); await answer(fresh, { kinds: [] });
  expect(row(first.cwd)).toBeUndefined();
  expect(text()).not.toContain("Removal is unconfirmed");
  expect(text()).toContain("No remembered rules were returned.");
});

it("survives StrictMode effect replay without admitting duplicate removals", async () => {
  await act(async () => root.render(<StrictMode><AssistantSettings /></StrictMode>)); await open();
  const old = reads()[0]; const latest = reads().at(-1)!;
  if (old !== latest) expect(old.init.signal?.aborted).toBe(true);
  await answer(latest, { kinds: [first] });
  if (old !== latest) await answer(old, { kinds: [second] });
  expect(row(second.cwd)).toBeUndefined(); await clickRow(first.cwd);
  await act(async () => { button("Remove rule").click(); button("Remove rule").click(); });
  expect(deletes()).toHaveLength(1);
  await answer(deletes()[0], { ok: true }); await answer(reads().at(-1)!, { kinds: [] });
});

it("does not sign out for an obsolete StrictMode read when the replacement is current", async () => {
  const unauthorized = vi.fn(); handleUnauthorized(unauthorized);
  await act(async () => root.render(<StrictMode><AssistantSettings /></StrictMode>)); await open();
  expect(reads().length).toBeGreaterThan(1);
  await answer(reads()[0], {}, 401); expect(unauthorized).not.toHaveBeenCalled();
  await answer(reads().at(-1)!, {}, 401); expect(unauthorized).toHaveBeenCalledOnce();
});
