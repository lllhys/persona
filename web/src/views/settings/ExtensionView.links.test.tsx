// @vitest-environment jsdom
// An extension's page in another manager's inbox item or chat (docs/assistant/managers.md "Grouping"): a document an action
// answers with open_url opens in the panel as that manager; the default's names none.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const DOC = "https://example.feishu.cn/docx/Act1";

vi.mock("../../lib/extension-ui", async (original) => {
  const real = await original<typeof import("../../lib/extension-ui")>();
  return {
    ...real,
    renderUI: async () => real.readView({ sections: [{ items: [{ key: "open", type: "button", title: "Open the plan", action: { id: "open" } }] }] }),
    actUI: async () => ({ open_url: DOC }),
  };
});
import { ItemManager, LinkManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { closePanel, getPanel } from "../../lib/panel";
import { LegacyExtensionUI } from "./ExtensionView";

vi.hoisted(() => {
  const g = globalThis as Record<string, unknown>;
  g.IS_REACT_ACT_ENVIRONMENT = true;
  g.matchMedia = (q: string) => ({ matches: q.includes("min-width"), addEventListener() {}, removeEventListener() {} });
});

let host: HTMLDivElement;
let root: Root;
const settle = async () => {
  for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve(); });
};
const press = async () => {
  const button = [...host.querySelectorAll("button")].find((b) => b.textContent?.includes("Open the plan"));
  expect(button).toBeTruthy();
  await act(async () => button!.click());
  await settle();
};

beforeEach(() => {
  resetManagerChoice();
  setRosterDefault("assistant");
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  closePanel();
});

describe("an action's open_url on an extension's page", () => {
  it("opens as the inbox item's or chat's manager, the default's naming none", async () => {
    act(() => root.render(<ItemManager.Provider value="m-3"><LegacyExtensionUI extension="lark" ui="inbox_item" manager="m-3" embedded /></ItemManager.Provider>));
    await settle();
    await press();
    expect(getPanel()).toMatchObject({ kind: "link", url: DOC, manager: "m-3" });

    act(() => root.render(<LinkManager.Provider value="m-2"><LegacyExtensionUI extension="lark" ui="card" embedded /></LinkManager.Provider>));
    await settle();
    await press();
    expect(getPanel()).toMatchObject({ kind: "link", url: DOC, manager: "m-2" });

    act(() => root.render(<ItemManager.Provider value="assistant"><LegacyExtensionUI extension="lark" ui="inbox_item" embedded /></ItemManager.Provider>));
    await settle();
    await press();
    expect(getPanel()).toMatchObject({ kind: "link", url: DOC });
    expect((getPanel() as { manager?: string }).manager).toBeUndefined();
  });
});
