// "Appearance" in Terminal settings (docs/integrations/terminal.md "Appearance"): the terminal's light / dark of its own,
// as iOS's terminal has colors apart from the app's (TerminalTheme.swift). "Match page", the default, stores nothing and
// is the page's scheme, as the terminal always was; "Light" / "Dark" are the page's own tokens of that scheme, scoped on
// an element with SCHEME_ATTR (styles.css, ui/look.ts, ui/extension-theme.ts), never a palette of their own. One
// setting for both surfaces, kept in this browser. Full view also tells herdr the scheme (terminal/full.ts).
import { useSyncExternalStore } from "react";

export type TerminalAppearance = "page" | "light" | "dark";

export const APPEARANCES: TerminalAppearance[] = ["page", "light", "dark"];

export const APPEARANCE_KEY = "persona.terminal.appearance";

/** The attribute that gives an element one scheme's tokens whatever the page's (the terminal's screen, and where its colors are read). */
export const SCHEME_ATTR = "data-terminal-scheme";

export function parseAppearance(raw: string | null | undefined): TerminalAppearance {
  return raw === "light" || raw === "dark" ? raw : "page";
}

export function terminalAppearance(): TerminalAppearance {
  try {
    return parseAppearance(localStorage.getItem(APPEARANCE_KEY));
  } catch {
    return "page";
  }
}

const listeners = new Set<() => void>();

export function subscribeAppearance(l: () => void): () => void {
  listeners.add(l);
  return () => void listeners.delete(l);
}

export function setTerminalAppearance(a: TerminalAppearance) {
  try {
    if (a === "page") localStorage.removeItem(APPEARANCE_KEY);
    else localStorage.setItem(APPEARANCE_KEY, a);
  } catch {
    // not kept: the terminal stays as it was
  }
  listeners.forEach((l) => l());
}

export const useTerminalAppearance = () => useSyncExternalStore(subscribeAppearance, terminalAppearance);

/** Whether the terminal draws dark: the page's scheme under "Match page", else the one chosen. */
export function terminalDark(a: TerminalAppearance, pageDark: boolean): boolean {
  return a === "page" ? pageDark : a === "dark";
}

/** SCHEME_ATTR's value for the terminal; none under "Match page", which reads the page's tokens off <html> as before. */
export function schemeScope(a: TerminalAppearance): "light" | "dark" | undefined {
  return a === "page" ? undefined : a;
}
