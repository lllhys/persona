import tokens from "../styles.css?page-tokens";
import { SCHEME_ATTR } from "../lib/terminal-appearance";

export interface ExtensionTheme {
  extension: string;
  extension_name: string;
  id: string;
  name: string;
  light: Record<string, string>;
  dark: Record<string, string>;
  problems?: string[];
}
const ROLES = { accent: "accent", positive: "green", caution: "orange", danger: "red", ai: "ai" } as const;
const BACKGROUNDS = ["page", "canvas", "surface", "inset", "hover", "hover-2", "field"];
export const MIN_ROLE_CONTRAST = 3;
export const MIN_TEXT_CONTRAST = 4.5;
const TINT = { light: 10, dark: 16 };

/** A color token's value in styles.css's first :root / .dark block ("" when it is not a hex there; page-tokens.ts). */
export function pageToken(role: string, scheme: "light" | "dark"): string {
  return tokens[scheme][role] ?? "";
}
function backgroundColors(scheme: "light" | "dark"): string[] {
  return BACKGROUNDS.map((role) => pageToken(role, scheme));
}
function rgb(hex: string): number[] {
  const s = hex.length === 4 ? hex.slice(1).split("").map((x) => x + x).join("") : hex.slice(1);
  return [0, 2, 4].map((i) => parseInt(s.slice(i, i + 2), 16));
}
function luminance(hex: string): number {
  return rgb(hex).map((c) => c / 255).map((v) => v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)
    .reduce((n, c, i) => n + c * [0.2126, 0.7152, 0.0722][i], 0);
}
export function contrast(a: string, b: string): number {
  const x = luminance(a), y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}
export function themeKey(t: ExtensionTheme): string { return `${t.extension}/${t.id}`; }

export function allowedTheme(t: ExtensionTheme): boolean {
  if (t.problems?.length) return false;
  return (["light", "dark"] as const).every((scheme) => {
    const colors = t[scheme], backgrounds = backgroundColors(scheme);
    return !!colors?.accent && Object.entries(colors).every(([role, color]) =>
      role in ROLES && /^#[0-9a-f]{6}$/i.test(color) && backgrounds.every((b) => /^#[0-9a-f]{3,6}$/i.test(b) && contrast(color, b) >= (role === "accent" ? MIN_ROLE_CONTRAST : MIN_TEXT_CONTRAST)));
  });
}
/** The color moved toward black (light) or white (dark) until it reads at `min`:1 on every page background. */
export function meetContrast(color: string, scheme: "light" | "dark", min: number): string {
  const backgrounds = backgroundColors(scheme);
  const base = rgb(color), end = scheme === "light" ? 0 : 255;
  for (let step = 0; step <= 100; step++) {
    const hex = "#" + base.map((c) => Math.round(c + (end - c) * step / 100).toString(16).padStart(2, "0")).join("");
    if (backgrounds.every((b) => contrast(hex, b) >= min)) return hex;
  }
  return color;
}
const readableAccent = (color: string, scheme: "light" | "dark") => meetContrast(color, scheme, MIN_TEXT_CONTRAST);

// Only role tokens are overridden; page/background/layout tokens remain untouched.
export function extensionThemeTokens(t: ExtensionTheme, scheme: "light" | "dark"): Record<string, string> {
  if (!allowedTheme(t)) return {};
  const out: Record<string, string> = {};
  for (const [role, color] of Object.entries(t[scheme])) {
    const token = ROLES[role as keyof typeof ROLES];
    out[`--${token}`] = color;
    // Success, caution and AI keep pale role fills; selection and danger remain neutral.
    out[`--${token}-tint`] = role === "accent" ? "var(--hover-2)" : role === "danger" ? "var(--inset)" : `color-mix(in srgb, ${color} ${TINT[scheme]}%, var(--surface))`;
    if (role === "accent") out["--accent-ink"] = readableAccent(color, scheme);
    if (role === "ai") out["--thinking-ink"] = color;
  }
  return out;
}

/**
 * The theme's tokens of each scheme on its terminal scope (SCHEME_ATTR, lib/terminal-appearance.ts): a terminal of the
 * other scheme than the page's reads them there. Above the stylesheet's and the theme color's (`[data-accent] …`), as
 * the inline tokens on <html> are.
 */
export function scopedThemeCSS(byScheme: Record<"light" | "dark", Record<string, string>>): string {
  return (["light", "dark"] as const)
    .filter((scheme) => Object.keys(byScheme[scheme]).length)
    .map((scheme) => `:root[data-extension-theme] [${SCHEME_ATTR}="${scheme}"] { ${Object.entries(byScheme[scheme]).map(([k, v]) => `${k}: ${v};`).join(" ")} }`)
    .join("\n");
}
