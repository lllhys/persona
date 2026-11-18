// The terminal's colors (docs/integrations/terminal.md "Appearance"): the page's tokens of the terminal's scheme. Under
// "Match page" they are read off <html>, as they always were; under "Light" / "Dark" off an element scoped to that
// scheme (SCHEME_ATTR), so a light page still gives a dark terminal the page's real dark values, a theme color's and an
// extension theme's included. Both surfaces read them here.
import { terminalPalette, type TerminalPalette } from "../../../lib/terminal";
import { SCHEME_ATTR, schemeScope, terminalAppearance, terminalDark, type TerminalAppearance } from "../../../lib/terminal-appearance";
import { hex6 } from "../../../lib/terminal-details";

export const pageDark = () => document.documentElement.classList.contains("dark");

/** Calls `read` with the terminal's tokens (`token("--page")` → its value) and whether its scheme is dark. */
export function withTerminalTokens<T>(read: (token: (name: string) => string, dark: boolean) => T, appearance: TerminalAppearance = terminalAppearance()): T {
  const dark = terminalDark(appearance, pageDark());
  const scope = schemeScope(appearance);
  if (!scope) {
    const css = getComputedStyle(document.documentElement);
    return read((n) => css.getPropertyValue(n), dark);
  }
  const probe = document.createElement("div");
  probe.setAttribute(SCHEME_ATTR, scope);
  probe.hidden = true;
  document.body.appendChild(probe);
  try {
    const css = getComputedStyle(probe);
    return read((n) => css.getPropertyValue(n), dark);
  } finally {
    probe.remove();
  }
}

export const terminalColors = (appearance?: TerminalAppearance): TerminalPalette => withTerminalTokens(terminalPalette, appearance);

/** The find addon's marks in the terminal's colors; undefined when a token is not a color it takes. */
export function findDecorations() {
  return withTerminalTokens((token) => {
    const border = hex6(token("--orange")), tint = hex6(token("--accent-tint")), accent = hex6(token("--accent"));
    return border && tint && accent
      ? { matchBorder: border, matchOverviewRuler: border, activeMatchBackground: tint, activeMatchBorder: accent, activeMatchColorOverviewRuler: accent }
      : undefined;
  });
}
