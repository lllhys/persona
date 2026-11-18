// @vitest-environment jsdom
// Settings › Hub › Update says when the update source looks stopped (GET /api/v1/hub/update source_behind, #8108):
// one line naming the source and hub.json's update.url in place of "Up to date"; a current source keeps "Up to date".
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { t } from "../../i18n";
import type { HubUpdateStatus } from "../../lib/hub-update";

const answers = vi.hoisted(() => ({ update: null as unknown }));
vi.mock("../../lib/api", async (original) => ({
  ...(await original<typeof import("../../lib/api")>()),
  api: (path: string) => Promise.resolve(path === "/hub/update" ? answers.update : path === "/hub/update/history" ? { history: [] } : {}),
}));
const { HubSettings } = await import("./HubSettings");

let root: Root;
let host: HTMLDivElement;

const status = (o: Partial<HubUpdateStatus>): HubUpdateStatus => ({
  version: "v0.5.3-150-gc0908bfa8-202611141806", build: 202611141806, channel: "release", can_update: true, state: "idle", busy: [],
  auto: true, auto_notice: false, confirmed: true, update_url: "https://personaside.example/dl", update_url_from: "build",
  latest: { version: "v0.1.17-177-g563f7fde5b-202611071005", build: 202611071005, newer: false, bad: false }, ...o,
});

const render = async (s: HubUpdateStatus) => {
  answers.update = s;
  await act(async () => root.render(<HubSettings />));
  return host.textContent ?? "";
};

beforeEach(() => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
});

describe("Settings › Hub update source", () => {
  it("shows the stopped source in place of Up to date", async () => {
    const text = await render(status({ source_behind: true }));
    expect(text).toContain("https://personaside.example/dl");
    expect(text).toContain("update.url");
    expect(text).toContain("v0.1.17-177-g563f7fde5b-202611071005");
    expect(text).not.toContain(t("settings-hubsettings.up_to_date"));
  });

  it("keeps Up to date for a current source", async () => {
    const text = await render(status({ latest: { version: "0.5.3", build: 202611140822, newer: false, bad: false } }));
    expect(text).toContain(t("settings-hubsettings.up_to_date"));
    expect(text).not.toContain("update.url");
  });

  it("keeps the stopped-source warning beside a failed-check error", async () => {
    const text = await render(status({ source_behind: true, error: "Fixture check failed" }));
    expect(text).toContain("update.url");
    expect(text).toContain("Fixture check failed");
    expect(text).not.toContain(t("settings-hubsettings.up_to_date"));
  });
});
