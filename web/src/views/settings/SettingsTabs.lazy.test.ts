/// <reference types="node" />
// Settings draws the Extensions and Connectors pages from chunks of their own, loaded when one is opened or its sidebar
// row is pointed at or focused (Settings.tsx, lazyPage). What only those pages draw (the market, an extension's sign-in
// and settings, the hub's connectors, the Lark bot, the sent-message rules) stays with them: a static import of any of
// it from another module would put it back into the Settings chunk, which every Settings page loads.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { describe, expect, it } from "vitest";

const root = new URL("../../", import.meta.url);
// each module, and the modules allowed to import it statically (admin/ is an entry of its own)
const pages: Record<string, string[]> = {
  Extensions: [],
  Connectors: [],
  ExtensionMarket: ["views/settings/Extensions.tsx", "admin/pages/Extensions.tsx"],
  ExtensionAuth: ["views/settings/Extensions.tsx"],
  ExtensionSettingsPage: ["views/settings/Extensions.tsx"],
  ExtensionOperation: ["views/settings/ExtensionMarket.tsx"],
  HubConnector: ["views/settings/Extensions.tsx", "views/settings/Connectors.tsx", "views/settings/FeishuBot.tsx"],
  FeishuBot: ["views/settings/Connectors.tsx"],
  SentSettings: ["views/settings/Connectors.tsx"],
  SentChatRules: ["views/settings/SentSettings.tsx"],
};

function sources(dir: URL): string[] {
  return readdirSync(dir).flatMap((name) => {
    const u = new URL(name, dir);
    if (statSync(u).isDirectory()) return sources(new URL(name + "/", dir));
    return /\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) ? [u.pathname] : [];
  });
}

describe("the Extensions and Connectors pages boundary", () => {
  it("imports what only those pages draw from those pages alone", () => {
    const base = new URL(root).pathname;
    for (const file of sources(root)) {
      const rel = file.slice(base.length);
      const text = readFileSync(file, "utf8");
      for (const [name, allowed] of Object.entries(pages)) {
        if (allowed.includes(rel)) continue;
        const staticImport = new RegExp(`^import\\s+(?!type\\b)[^;]*from\\s+["'](?:\\.{1,2}/)+(?:views/)?(?:settings/)?${name}["']`, "m");
        expect(text, `${rel} imports ${name}`).not.toMatch(staticImport);
      }
    }
  });

  it("draws both pages in Settings through lazyPage", () => {
    const settings = readFileSync(new URL("../Settings.tsx", import.meta.url), "utf8");
    expect(settings).toMatch(/const Extensions = lazyPage\(\(\) => import\("\.\/settings\/Extensions"\)\.then\(\(m\) => m\.Extensions\)\)/);
    expect(settings).toMatch(/const Connectors = lazyPage\(\(\) => import\("\.\/settings\/Connectors"\)\.then\(\(m\) => m\.Connectors\)\)/);
    // the pages render through these in Settings.navigation / Settings.groups tests (loadPages)
  });
});
