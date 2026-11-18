// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
vi.hoisted(() => { window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia; });
vi.mock("../../ui/Markdown", () => ({ Markdown: () => null }));
vi.mock("./ModelProfiles", () => ({ ModelProfiles: () => null }));
vi.mock("../../lib/managers", () => ({
  useRoster: () => ({ default: "assistant", managers: [{ id: "assistant", name: "Personal" }, { id: "work", name: "Work" }] }),
  managerOrder: (r: { managers: unknown[] }) => r.managers,
}));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }), PersonaAvatar: () => null }));
vi.mock("./Skills", () => ({ SkillsGroup: () => null }));
vi.mock("../../lib/store", () => ({ useStore: () => [], refreshAssistant: vi.fn() }));
vi.mock("../../ui/atoms", async (original) => ({ ...await original<typeof import("../../ui/atoms")>(), toast: vi.fn() }));
import { initI18n, t } from "../../i18n";
import { chooseManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { toast } from "../../ui/atoms";
import { AssistantSettings } from "./AssistantSettings";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root;
let host: HTMLDivElement;
let modes: Record<string, "sandbox" | "prompt" | undefined>;
let writes: { manager: string; body: Record<string, unknown> }[];
let rejectWrite = false;
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const flush = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); }); };
const selector = () => host.querySelector<HTMLSelectElement>(`select[aria-label="${t("settings-assistantsettings.readonly_mode")}"]`)!;
async function choose(value: string) {
  await act(async () => { const node = selector(); node.value = value; node.dispatchEvent(new Event("change", { bubbles: true })); });
  await flush();
}
async function current(id: string) { await act(async () => chooseManager(id)); await flush(); }
beforeEach(async () => {
  await initI18n("en");
  history.replaceState(null, "", "/settings?tab=assistant");
  resetManagerChoice(); setRosterDefault("assistant");
  modes = { assistant: undefined, work: "prompt" }; writes = []; rejectWrite = false;
  vi.mocked(toast).mockClear();
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).split("?")[0];
    const manager = new Headers(init?.headers).get("X-Persona-Manager")!;
    if (path.endsWith("/assistant")) {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)); writes.push({ manager, body });
        if (rejectWrite) return json({ error: "Denied" }, 403);
        if (body.readonly_mode) modes[manager] = body.readonly_mode;
      }
      return json({ manager, ...(modes[manager] ? { readonly_mode: modes[manager] } : {}) });
    }
    if (path.endsWith("/tasks")) return json({ tasks: [] });
    if (path.endsWith("/memory")) return json({ files: [] });
    throw new Error(`Unexpected path: ${path}`);
  }));
});
afterEach(async () => { act(() => root.unmount()); host.remove(); vi.unstubAllGlobals(); resetManagerChoice(); });
async function render() { await act(async () => root.render(<AssistantSettings />)); await flush(); }

it("defaults a missing value to runtime protection and saves the selected mode for the current manager", async () => {
  await render();
  expect(selector().value).toBe("sandbox");
  expect(selector().className).toContain("pointer-coarse:min-h-11");
  expect(selector().classList.contains("text-meta")).toBe(true);
  expect([...selector().options].map((o) => [o.value, o.textContent])).toEqual([
    ["sandbox", "Runtime protection (default)"], ["prompt", "Instructions only"],
  ]);
  expect(host.textContent).toContain("not a security boundary");
  expect(host.textContent).toContain("new starts and retries only");
  await choose("prompt");
  expect(writes).toEqual([{ manager: "assistant", body: { readonly_mode: "prompt" } }]);
  expect(selector().value).toBe("prompt");
  expect(toast).not.toHaveBeenCalled();
});

it("keeps the confirmed value and reports a failed save", async () => {
  await render();
  rejectWrite = true;
  await choose("prompt");
  expect(selector().value).toBe("sandbox");
  expect(toast).toHaveBeenCalledWith(expect.stringContaining("Denied"), "error");
});

it("loads and saves each manager independently", async () => {
  await render();
  await choose("prompt");
  await current("work");
  expect(selector().value).toBe("prompt");
  await choose("sandbox");
  await current("assistant");
  expect(selector().value).toBe("prompt");
  expect(writes).toEqual([
    { manager: "assistant", body: { readonly_mode: "prompt" } },
    { manager: "work", body: { readonly_mode: "sandbox" } },
  ]);
});

it("ignores a late save response after switching managers", async () => {
  await render();
  let finish: ((response: Response) => void) | undefined;
  const realFetch = globalThis.fetch;
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const manager = new Headers(init?.headers).get("X-Persona-Manager")!;
    if (init?.method === "PUT" && manager === "assistant") {
      writes.push({ manager, body: JSON.parse(String(init.body)) });
      return new Promise<Response>((resolve) => { finish = resolve; });
    }
    return realFetch(input, init);
  }));
  await act(async () => {
    const node = selector(); node.value = "prompt"; node.dispatchEvent(new Event("change", { bubbles: true }));
  });
  await current("work");
  expect(selector().value).toBe("prompt");
  await act(async () => finish!(json({ manager: "assistant", readonly_mode: "prompt" })));
  await flush();
  expect(selector().value).toBe("prompt");
  expect(toast).not.toHaveBeenCalled();
});
