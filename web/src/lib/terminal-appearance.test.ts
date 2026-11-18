// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  APPEARANCE_KEY,
  parseAppearance,
  schemeScope,
  setTerminalAppearance,
  subscribeAppearance,
  terminalAppearance,
  terminalDark,
} from "./terminal-appearance";

afterEach(() => localStorage.clear());

describe("the terminal's Appearance", () => {
  it("is Match page until another is chosen, and stores nothing for it", () => {
    expect(terminalAppearance()).toBe("page");
    setTerminalAppearance("dark");
    expect(localStorage.getItem(APPEARANCE_KEY)).toBe("dark");
    expect(terminalAppearance()).toBe("dark");
    setTerminalAppearance("page");
    expect(localStorage.getItem(APPEARANCE_KEY)).toBeNull();
    expect(terminalAppearance()).toBe("page");
  });

  it("reads anything it does not know as Match page", () => {
    expect(parseAppearance("light")).toBe("light");
    expect(parseAppearance("dark")).toBe("dark");
    for (const raw of [null, undefined, "", "page", "system", "Dark"]) expect(parseAppearance(raw)).toBe("page");
  });

  it("tells the terminals when it changes", () => {
    const heard = vi.fn();
    const off = subscribeAppearance(heard);
    setTerminalAppearance("light");
    expect(heard).toHaveBeenCalledTimes(1);
    off();
    setTerminalAppearance("dark");
    expect(heard).toHaveBeenCalledTimes(1);
  });

  it("is the page's scheme under Match page, and the chosen one otherwise", () => {
    expect([terminalDark("page", true), terminalDark("page", false)]).toEqual([true, false]);
    expect([terminalDark("dark", false), terminalDark("dark", true)]).toEqual([true, true]);
    expect([terminalDark("light", true), terminalDark("light", false)]).toEqual([false, false]);
  });

  it("scopes a scheme on the terminal only when one is chosen", () => {
    expect(schemeScope("page")).toBeUndefined();
    expect(schemeScope("light")).toBe("light");
    expect(schemeScope("dark")).toBe("dark");
  });
});
