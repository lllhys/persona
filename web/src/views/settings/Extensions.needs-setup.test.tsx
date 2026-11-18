// @vitest-environment jsdom
// An extension's page (#8853): the header's "Still needs" line opens the secret it names, and the configuration's
// heading stands only over "No configuration required" (every other group has a heading of its own).
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubExtensionDetail } from "../../lib/extensions";

const hub = vi.hoisted(() => ({ detail: null as unknown }));
vi.mock("../../lib/extensions", async (original) => ({
  ...(await original<typeof import("../../lib/extensions")>()),
  extensionDetail: vi.fn(() => Promise.resolve(hub.detail)),
}));
import { t } from "../../i18n";
import { Extensions } from "./Extensions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
const flush = async () => {
  for (let i = 0; i < 6; i++) await act(async () => { await Promise.resolve(); });
};

const speech = (extra: Partial<HubExtensionDetail> = {}): HubExtensionDetail => ({
  id: "example-speech",
  name: "Example speech",
  kind: "process",
  status: "needs_setup",
  source: "builtin",
  enabled: true,
  needs_setup: ["Example 的 API Key"],
  form: { fields: [{ name: "read_answers", type: "boolean", title: "朗读回答" }] },
  config: {},
  secrets: [{ name: "key", title: "API Key", set: false }],
  ...extra,
});

const open = async () => {
  act(() => root.render(<Extensions open="example-speech" onOpen={() => {}} />));
  await flush();
};
const needsLine = () => host.querySelector<HTMLElement>("[data-extension-needs-setup]");
const configuration = () => host.querySelector<HTMLElement>('[data-extension-section="configuration"]')!;
const headings = () => [...configuration().querySelectorAll("h2")].map((h) => h.textContent);
const secretInput = () => document.querySelector<HTMLInputElement>('input[aria-label="API Key"]');

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("Still needs", () => {
  it("opens the secret it names", async () => {
    hub.detail = speech();
    await open();
    const line = needsLine()!;
    expect(line.tagName).toBe("BUTTON");
    expect(line.textContent).toContain("Example 的 API Key");
    expect(secretInput()).toBeNull();
    await act(async () => line.click());
    expect(secretInput()).toBeTruthy();
  });

  it("stays plain text when what it names is not a secret to set here", async () => {
    hub.detail = speech({ needs_setup: ["依赖「Example」还没准备好"], secrets: [{ name: "key", title: "API Key", set: true }] });
    await open();
    expect(needsLine()!.tagName).toBe("P");
  });

  it("stays plain text where the settings are not changed here", async () => {
    hub.detail = speech({ read_only: true });
    await open();
    expect(needsLine()!.tagName).toBe("P");
  });
});

describe("the configuration's heading", () => {
  it("is not shown over groups that have their own", async () => {
    hub.detail = speech();
    await open();
    expect(headings()).not.toContain(t("settings-extensions.configuration_title"));
    expect(headings()).toEqual(expect.arrayContaining([t("settings-extensions.fields_title"), t("settings-extensions.secrets_title")]));
    expect(configuration().getAttribute("aria-label")).toBe(t("settings-extensions.configuration_title"));
  });

  it("stands over No configuration required", async () => {
    hub.detail = speech({ status: "running", needs_setup: [], form: undefined, secrets: [] });
    await open();
    expect(headings()[0]).toBe(t("settings-extensions.configuration_title"));
    expect(configuration().textContent).toContain(t("settings-extensions.no_configuration"));
  });
});
