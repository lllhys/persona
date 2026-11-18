// @vitest-environment jsdom
// Terminal settings › Appearance: three choices a screen reader names, Match page until another is picked, and the
// line under the label saying the one chosen.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { t } from "../../../i18n";
import { AppearanceRow } from "./TerminalOptions";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;

beforeEach(() => {
  localStorage.clear();
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  localStorage.clear();
});

const radios = () => [...host.querySelectorAll<HTMLButtonElement>('[role="radio"]')];
const checked = () => radios().find((r) => r.getAttribute("aria-checked") === "true")?.getAttribute("aria-label");

describe("Appearance in Terminal settings", () => {
  it("offers Match page, Light and Dark, Match page chosen", () => {
    act(() => root.render(<AppearanceRow />));
    expect(host.querySelector('[role="radiogroup"]')?.getAttribute("aria-label")).toBe(t("session-terminal-terminaloptions.appearance"));
    expect(radios().map((r) => r.getAttribute("aria-label"))).toEqual([
      t("session-terminal-terminaloptions.appearance_page"),
      t("session-terminal-terminaloptions.appearance_light"),
      t("session-terminal-terminaloptions.appearance_dark"),
    ]);
    expect(checked()).toBe(t("session-terminal-terminaloptions.appearance_page"));
    expect(host.textContent).toContain(t("session-terminal-terminaloptions.appearance_hint_page"));
  });

  it("keeps a choice in this browser and can go back to Match page", () => {
    act(() => root.render(<AppearanceRow />));
    act(() => radios()[2].click());
    expect(localStorage.getItem("persona.terminal.appearance")).toBe("dark");
    expect(checked()).toBe(t("session-terminal-terminaloptions.appearance_dark"));
    expect(host.textContent).toContain(t("session-terminal-terminaloptions.appearance_hint_dark"));
    act(() => radios()[0].click());
    expect(localStorage.getItem("persona.terminal.appearance")).toBeNull();
    expect(checked()).toBe(t("session-terminal-terminaloptions.appearance_page"));
  });
});
