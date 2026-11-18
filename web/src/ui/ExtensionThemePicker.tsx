import { createContext, useContext, useEffect, useLayoutEffect, useMemo, useState, type ReactNode } from "react";
import { useDeclaredResult } from "../lib/extension-declared";
import { t } from "../i18n";
import { allowedTheme, extensionThemeTokens, scopedThemeCSS, themeKey, type ExtensionTheme } from "./extension-theme";
import { setExtensionThemeActive } from "./look";

const KEY = "persona.extension-theme";
const AVAILABILITY_CHECK_MS = 30_000;
interface PaletteState { available: ExtensionTheme[]; selected: string; select: (id: string) => void }
const PaletteContext = createContext<PaletteState>({ available: [], selected: "", select: () => {} });

export function ExtensionThemeProvider({ children }: { children: ReactNode }) {
  const [selected, setSelected] = useState(() => typeof localStorage === "undefined" ? "" : localStorage.getItem(KEY) ?? "");
  const result = useDeclaredResult<ExtensionTheme>("themes", "themes", selected ? AVAILABILITY_CHECK_MS : 0);
  const available = useMemo(() => result.items.filter(allowedTheme), [result.items]);
  const select = (id: string) => {
    if (id && !available.some((t) => themeKey(t) === id)) return;
    if (id) localStorage.setItem(KEY, id); else localStorage.removeItem(KEY);
    setSelected(id);
  };
  useEffect(() => {
    if (result.loaded && selected && !available.some((t) => themeKey(t) === selected)) select("");
  }, [result.loaded, selected, available]);
  const current = available.find((t) => themeKey(t) === selected);
  useLayoutEffect(() => {
    if (!current) return;
    const root = document.documentElement;
    const tokens = { light: extensionThemeTokens(current, "light"), dark: extensionThemeTokens(current, "dark") };
    const keys = new Set([...Object.keys(tokens.light), ...Object.keys(tokens.dark)]);
    const before = [...keys].map((key) => ({ key, value: root.style.getPropertyValue(key), priority: root.style.getPropertyPriority(key) }));
    const restore = () => before.forEach(({ key, value, priority }) => value ? root.style.setProperty(key, value, priority) : root.style.removeProperty(key));
    const apply = () => {
      restore();
      Object.entries(tokens[root.classList.contains("dark") ? "dark" : "light"]).forEach(([key, value]) => root.style.setProperty(key, value));
      root.dataset.extensionTheme = themeKey(current);
    };
    apply();
    // the terminal's own scheme (its Appearance) reads the theme's colors of that scheme
    const scoped = document.createElement("style");
    scoped.textContent = scopedThemeCSS(tokens);
    document.head.appendChild(scoped);
    setExtensionThemeActive(true);
    const observer = new MutationObserver(apply);
    observer.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => { observer.disconnect(); restore(); scoped.remove(); delete root.dataset.extensionTheme; setExtensionThemeActive(false); };
  }, [current]);
  return <PaletteContext.Provider value={{ available, selected, select }}>{children}</PaletteContext.Provider>;
}

export function ExtensionThemePicker() {
  const { available, selected, select } = useContext(PaletteContext);
  if (!available.length) return null;
  return <div className="flex items-center gap-3 px-4 py-2.5">
    <span className="min-w-0 flex-1 text-[13px] text-ink">{t("ui-extensionthemepicker.label")}</span>
    <select aria-label={t("ui-extensionthemepicker.label")} value={selected} onChange={(e) => select(e.target.value)} className="h-8 max-w-[60%] rounded-control bg-field px-2.5 text-[12.5px] text-ink shadow-inset-field outline-none focus:shadow-[inset_0_0_0_1px_var(--accent)]">
      <option value="">{t("ui-extensionthemepicker.default")}</option>
      {available.map((th) => <option key={themeKey(th)} value={themeKey(th)}>{th.name} · {th.extension_name}</option>)}
    </select>
  </div>;
}

/** The extension theme painting over the page now, if any ("Theme color" says so: its colors are under it, ui/look.ts). */
export function useActiveExtensionTheme(): ExtensionTheme | undefined {
  const { available, selected } = useContext(PaletteContext);
  return selected ? available.find((th) => themeKey(th) === selected) : undefined;
}
