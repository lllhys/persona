// @vitest-environment jsdom
// Settings > Claude Code: two rows saved at once. Each row stays busy until its own save ends, and the shown computer's
// answer to one save does not put another row back to the value it had before its own save.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import type { AgentSetting } from "../../lib/agent-settings";

type Call = { path: string; method: string; json?: { key: string; value: unknown }; resolve: (v: unknown) => void; reject: (e: unknown) => void };
const calls: Call[] = [];
vi.mock("../../lib/api", async (orig) => ({
  ...(await orig<typeof import("../../lib/api")>()),
  api: (path: string, init?: { method?: string; json?: { key: string; value: unknown } }) =>
    new Promise((resolve, reject) => calls.push({ path, method: init?.method ?? "GET", json: init?.json, resolve, reject })),
}));
const MACHINES = [{ id: "m-1", name: "工作", online: true }];
vi.mock("../../lib/store", async (orig) => ({
  ...(await orig<typeof import("../../lib/store")>()),
  useStore: (select: (s: { machines: typeof MACHINES }) => unknown) => select({ machines: MACHINES }),
}));
vi.mock("./ClaudeDirs", () => ({ ClaudeDirs: () => null })); // its own reads are not this test's
vi.mock("../../ui/atoms", async (orig) => ({ ...(await orig<typeof import("../../ui/atoms")>()), toast: () => {} }));

const { AgentSettings } = await import("./AgentSettings");

// Two bool options the catalog has no words for, so each row is named by the title the node sends.
const row = (key: string, value: boolean | null): AgentSetting => ({ key, title: key, detail: "", type: "bool", default: false, value });
const list = (a: boolean | null, b: boolean | null) => [row("optA", a), row("optB", b)];

let root: Root;
let host: HTMLDivElement;
beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  calls.length = 0;
  localStorage.clear();
  localStorage.setItem("persona.agentSettings.applyAll", "0"); // only the shown computer, one PUT per change
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

const sw = (label: string) => host.querySelector<HTMLButtonElement>(`[role="switch"][aria-label="${label}"]`)!;
const click = (label: string) => act(async () => sw(label).click());
const put = (key: string) => calls.find((c) => c.method === "PUT" && c.json?.key === key)!;
const answer = (c: Call, v: unknown) => act(async () => c.resolve(v));

async function loaded(settings = list(false, false)) {
  await act(async () => root.render(<AgentSettings agent="claude" />));
  await answer(calls.find((c) => c.method === "GET" && c.path.endsWith("/claude-settings"))!, { settings });
}

const ignoredEffort: AgentSetting = {
  key: "effortLevel", title: "Thinking effort", detail: "Node fallback", type: "enum",
  options: ["auto", "low", "medium", "high", "xhigh"], default: "auto", value: null, ignored_value: "max",
};
const effortSelect = () => host.querySelector<HTMLSelectElement>(`select[aria-label="${t("lib-agent-settings.setting_effortLevel")}"]`)!;
const effortWarning = () => host.textContent!.includes(t("lib-agent-settings.setting_effortLevel_ignored_max"));
const selectEffort = (value: string) => act(async () => {
  effortSelect().value = value;
  effortSelect().dispatchEvent(new Event("change", { bubbles: true }));
});

describe("Settings > Claude Code, ignored stored max", () => {
  it("shows the default and warning, and offers only persistent effort levels", async () => {
    await loaded([ignoredEffort]);
    expect(effortSelect().value).toBe("auto");
    expect([...effortSelect().options].map((o) => o.value)).toEqual(["auto", "low", "medium", "high", "xhigh"]);
    expect(effortWarning()).toBe(true);
    expect(calls.filter((c) => c.method === "PUT")).toHaveLength(0);
  });

  it("restores the warning and default when saving an explicit level fails", async () => {
    await loaded([ignoredEffort]);
    await selectEffort("high");
    expect(put("effortLevel").json).toEqual({ key: "effortLevel", value: "high" });
    expect(effortWarning()).toBe(false);
    expect(effortSelect().disabled).toBe(true);
    await act(async () => put("effortLevel").reject(new Error("refused")));
    expect(effortSelect().value).toBe("auto");
    expect(effortSelect().disabled).toBe(false);
    expect(effortWarning()).toBe(true);
  });

  it("can reset an ignored stored value even though the default is already effective", async () => {
    await loaded([ignoredEffort]);
    const label = t("settings-agentsettings.more", { title: t("lib-agent-settings.setting_effortLevel") });
    await act(async () => host.querySelector<HTMLButtonElement>(`button[aria-label="${label}"]`)!.click());
    const reset = [...document.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')].find((b) => b.textContent!.includes(t("settings-agentsettings.reset")))!;
    expect(reset.disabled).toBe(false);
    await act(async () => reset.click());
    expect(put("effortLevel").json).toEqual({ key: "effortLevel", value: null });
    await answer(put("effortLevel"), { settings: [{ ...ignoredEffort, ignored_value: undefined }] });
    expect(effortWarning()).toBe(false);
    expect(effortSelect().value).toBe("auto");
  });

  it("keeps a corrected effort when another row's stale response arrives later", async () => {
    await loaded([row("optA", false), ignoredEffort]);
    await click("optA");
    await selectEffort("high");
    await answer(put("effortLevel"), { settings: [row("optA", true), { ...ignoredEffort, value: "high", ignored_value: undefined }] });
    await answer(put("optA"), { settings: [row("optA", true), ignoredEffort] });
    expect(effortSelect().value).toBe("high");
    expect(effortWarning()).toBe(false);
  });

  it("keeps ignored-value metadata from the saved row's response while another save is pending", async () => {
    await loaded([row("optA", false), { ...ignoredEffort, ignored_value: undefined }]);
    await click("optA");
    await selectEffort("high");
    // The file was changed elsewhere before the node read back its settings.
    await answer(put("effortLevel"), { settings: [row("optA", false), ignoredEffort] });
    expect(effortWarning()).toBe(true);
    expect(effortSelect().value).toBe("auto");
    expect(sw("optA").disabled).toBe(true);
    expect(sw("optA").getAttribute("aria-checked")).toBe("true");
  });
});

describe("Settings > Claude Code, two saves at once", () => {
  it("keeps the other row busy and at its new value when the first save answers", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    expect(sw("optA").disabled).toBe(true);
    expect(sw("optB").disabled).toBe(true);

    // A's answer was read before B's write landed on the node: B is still off in it.
    await answer(put("optA"), { settings: list(true, false) });
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optA").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").disabled).toBe(true);

    await answer(put("optB"), { settings: list(true, true) });
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optB").disabled).toBe(false);
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("does not let a late answer to the first save undo the second", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    await answer(put("optB"), { settings: list(true, true) });
    expect(sw("optB").disabled).toBe(false);
    expect(sw("optA").disabled).toBe(true);

    await answer(put("optA"), { settings: list(true, false) });
    expect(sw("optA").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("still takes the whole list when one save is alone", async () => {
    await loaded();
    await click("optA");
    // The node's answer also carries a change made elsewhere (B set on another device).
    await answer(put("optA"), { settings: list(true, true) });
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
  });

  it("rolls back only the refused row", async () => {
    await loaded();
    await click("optA");
    await click("optB");
    await act(async () => put("optA").reject(new Error("refused")));
    expect(sw("optA").getAttribute("aria-checked")).toBe("false");
    expect(sw("optA").disabled).toBe(false);
    expect(sw("optB").getAttribute("aria-checked")).toBe("true");
    expect(sw("optB").disabled).toBe(true);
  });
});

describe("Settings > Claude Code, live replies (#1309)", () => {
  const liveGet = () => calls.filter((c) => c.method === "GET" && c.path.endsWith("/claude-live-replies"));

  it("has the live replies switch for the shown computer under Claude Code only", async () => {
    await loaded();
    expect(liveGet().map((c) => c.path)).toEqual(["/machines/m-1/claude-live-replies"]);
    await answer(liveGet()[0], { on: true });
    expect(sw(t("settings-livereplies.label")).getAttribute("aria-checked")).toBe("true");

    act(() => root.unmount());
    root = createRoot(host);
    calls.length = 0;
    await act(async () => root.render(<AgentSettings agent="codex" />));
    expect(liveGet()).toEqual([]);
  });
});

describe("Settings > Grok (#8353)", () => {
  const grok: AgentSetting[] = [
    { key: "ui.permission_mode", title: "Permission mode", detail: "", type: "enum", options: ["default", "ask", "auto", "always-approve"], default: "default", value: null },
    { key: "ui.yolo", title: "Approve all tool calls", detail: "", type: "bool", default: false, value: null },
  ];

  it("reads and writes the Grok CLI's settings, named in the page's language", async () => {
    await act(async () => root.render(<AgentSettings agent="grok" />));
    const get = calls.find((c) => c.method === "GET")!;
    expect(get.path).toBe("/machines/m-1/grok-settings");
    await answer(get, { settings: grok });
    expect(host.textContent).toContain(t("lib-agent-settings.setting_ui.permission_mode"));
    expect(host.textContent).toContain(t("lib-agent-settings.setting_ui.yolo_detail"));
    expect(host.textContent).toContain(t("lib-agent-settings.grok_footer"));
    await click(t("lib-agent-settings.setting_ui.yolo"));
    expect(put("ui.yolo")).toMatchObject({ path: "/machines/m-1/grok-settings", json: { key: "ui.yolo", value: true } });
  });
});
