// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
vi.hoisted(() => { window.matchMedia ??= ((query: string) => ({ matches: false, media: query, onchange: null, addEventListener() {}, removeEventListener() {} })) as typeof window.matchMedia; });
vi.mock("../../ui/Markdown", () => ({ Markdown: () => null }));
vi.mock("../../lib/managers", () => ({
  useRoster: () => ({ default: "assistant", managers: [{ id: "assistant", name: "Personal" }, { id: "work", name: "Work" }] }),
  managerOrder: (r: { managers: unknown[] }) => r.managers,
}));
vi.mock("../assistant/Persona", () => ({ usePersona: () => ({ name: "Persona" }), PersonaAvatar: () => null }));
vi.mock("./Skills", () => ({ SkillsGroup: () => null }));
vi.mock("../../lib/store", () => ({ useStore: () => [], refreshAssistant: vi.fn() }));
vi.mock("../../ui/atoms", async (original) => ({ ...await original<typeof import("../../ui/atoms")>(), toast: vi.fn() }));
import { initI18n, t } from "../../i18n";
import { resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { toast } from "../../ui/atoms";
import { AssistantSettings } from "./AssistantSettings";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root;
let host: HTMLDivElement;
let roles: Record<string, string | undefined>;
let writes: { manager: string; body: Record<string, unknown> }[];
/** What a PUT answers instead of the stored state, when set. */
let answer: ((manager: string, body: Record<string, unknown>) => Response) | undefined;
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
const flush = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); }); };
const meeting = () => host.querySelector<HTMLSelectElement>(`select[aria-label="${t("settings-assistantsettings.meeting_attendance")}"]`);
async function choose(node: HTMLSelectElement, value: string) {
  await act(async () => { node.value = value; node.dispatchEvent(new Event("change", { bubbles: true })); });
  await flush();
}
const state = (manager: string) => ({ manager, notes: "", ...(roles[manager] === undefined ? {} : { meeting_attendance_default: roles[manager] }) });
beforeEach(async () => {
  await initI18n("en");
  history.replaceState(null, "", "/settings?tab=persona");
  resetManagerChoice(); setRosterDefault("assistant");
  vi.mocked(toast).mockClear();
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
  roles = { assistant: "off", work: undefined }; writes = []; answer = undefined;
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).split("?")[0];
    const manager = new Headers(init?.headers).get("X-Persona-Manager")!;
    if (path.endsWith("/assistant")) {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)); writes.push({ manager, body });
        if (answer) return answer(manager, body);
        if (!["off", "listener", "participant"].includes(body.meeting_attendance_default)) return json({ error: "Unknown role" }, 400);
        roles[manager] = body.meeting_attendance_default;
      }
      return json(state(manager));
    }
    if (path.endsWith("/tasks")) return json({ tasks: [] });
    if (path.endsWith("/memory")) return json({ files: [] });
    throw new Error(`Unexpected path: ${path}`);
  }));
});
afterEach(async () => { act(() => root.unmount()); host.remove(); vi.unstubAllGlobals(); });
async function render() { await act(async () => root.render(<AssistantSettings />)); await flush(); }

it("shows the default role with its footer and saves a change confirmed by the echo", async () => {
  await render();
  const select = meeting()!;
  expect(select.value).toBe("off");
  expect([...select.options].map((o) => [o.value, o.textContent])).toEqual([["off", "Off"], ["listener", "Listener"], ["participant", "Participant"]]);
  expect(host.textContent).toContain("Meetings");
  expect(host.textContent).toContain("Answers use meeting material only");
  await choose(select, "participant");
  expect(writes).toEqual([{ manager: "assistant", body: { meeting_attendance_default: "participant" } }]);
  expect(meeting()!.value).toBe("participant");
  expect(meeting()!.disabled).toBe(false);
  expect(toast).not.toHaveBeenCalled();
});

it("has no group on a hub or manager that does not attend meetings", async () => {
  roles.assistant = undefined;
  await render();
  expect(meeting()).toBeNull();
  expect(host.textContent).not.toContain("Meetings");
});

it.each([
  ["another value", (manager: string) => json({ manager, meeting_attendance_default: "off" })],
  ["another manager", () => json({ manager: "work", meeting_attendance_default: "listener" })],
  ["no manager identity", () => json({ meeting_attendance_default: "listener" })],
])("keeps the shown role and says so when the hub echoes %s", async (_, reply) => {
  answer = reply;
  await render();
  await choose(meeting()!, "listener");
  expect(writes).toHaveLength(1);
  expect(toast).toHaveBeenCalledWith(t("settings-assistantsettings.save_failed", { error: t("settings-assistantsettings.meeting_attendance_unconfirmed") }), "error");
  expect(meeting()!.value).toBe("off");
});

it("reports a refused change and keeps the role", async () => {
  answer = () => json({ error: "Synthetic refusal" }, 403);
  await render();
  await choose(meeting()!, "listener");
  expect(toast).toHaveBeenCalledWith(expect.stringContaining("Synthetic refusal"), "error");
  expect(meeting()!.value).toBe("off");
});

it("cannot write without the manager's identity, and shows an unknown role as it is", async () => {
  const real = globalThis.fetch;
  vi.stubGlobal("fetch", vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
    String(input).endsWith("/assistant") && !init?.method ? Promise.resolve(json({ meeting_attendance_default: "listener" })) : real(input, init)));
  await render();
  expect(meeting()!.disabled).toBe(true);
  act(() => root.unmount()); host.replaceChildren(); root = createRoot(host);
  roles.assistant = "observer";
  vi.stubGlobal("fetch", real);
  await render();
  expect(meeting()!.value).toBe("observer");
  expect(meeting()!.options[0].disabled).toBe(true);
  expect(writes).toEqual([]);
});
