// End-to-end check of opening a chat whose agent Paseo let go (#8321, docs/web/web.md "Connection"): its bridge reads
// such an agent only while its chat is watched (#8132), so the page's first read is refused as not_loaded, and the
// subscribe that follows loads it; the hub's backlog brings the history a few seconds later. Meanwhile the chat says
// "Loading chat", never the refusal.
//
// Needs the web page against the fake hub with its closed Grok agent, and a Chromium (playwright-core drives it):
//
//   MOCK_PASEO_NOT_LOADED=1 PORT=18821 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18821 npx vite --port 5821 --strictPort &)
//   node web/e2e/paseo-not-loaded.mjs http://127.0.0.1:5821 --hub http://127.0.0.1:18821 [--shots <dir>] [--chromium <executable>]
//
// Runs wide (1280) and phone width (390), light and dark, in English; --shots saves the loading state and the history.
// Prints one line per check and a verdict; exits 1 on a failed check and 2 on its three-minute timeout.
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const base = (args[0] ?? "").replace(/\/$/, "");
const hub = (flag("--hub") ?? "").replace(/\/$/, "");
const shots = flag("--shots");
if (!base || !hub) {
  console.error("usage: node web/e2e/paseo-not-loaded.mjs <web URL> --hub <fake hub URL> [--shots <dir>] [--chromium <executable>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => { console.log("VERDICT TIMEOUT paseo not loaded"); process.exit(2); }, 180_000);

// the fake hub's closed Grok agent (scripts/mock/mock_hub.py MOCK_PASEO_NOT_LOADED) and what it said
const CHAT = "server/paseo-9a5e0000-0000-4000-8000-000000000012";
const ANSWER = "Done: 212 photos in 9 albums, one per town.";
const REFUSAL = "This agent isn’t running in Paseo";
const LOADING = "Loading chat";

const failures = [];
const check = (ok, what, detail = "") => {
  console.log(`${ok ? "ok  " : "FAIL"} ${what}${detail ? `: ${detail}` : ""}`);
  if (!ok) failures.push(what);
};

const browser = await chromium.launch({ executablePath: flag("--chromium"), headless: true });
try {
  for (const { name, theme, viewport, touch } of [
    { name: "wide-light", theme: "light", viewport: { width: 1280, height: 860 } },
    { name: "wide-dark", theme: "dark", viewport: { width: 1280, height: 860 } },
    { name: "phone-light", theme: "light", viewport: { width: 390, height: 844 }, touch: true },
    { name: "phone-dark", theme: "dark", viewport: { width: 390, height: 844 }, touch: true },
  ]) {
    // Paseo lets the agent go again: the next open is refused, and its watch loads it
    await fetch(`${hub}/mock/paseo-unload`, { signal: AbortSignal.timeout(5_000) });
    const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, locale: "en-US", colorScheme: theme, hasTouch: Boolean(touch), isMobile: Boolean(touch) });
    // Whether the refusal is ever drawn, however briefly: every change of the page is looked at.
    await ctx.addInitScript((refusal) => {
      window.__refusalShown = false;
      new MutationObserver(() => {
        if (document.body?.innerText.includes(refusal)) window.__refusalShown = true;
      }).observe(document, { subtree: true, childList: true, characterData: true });
    }, REFUSAL);
    const page = await ctx.newPage();
    await page.goto(`${base}/?token=mock`);
    await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15_000 });
    await page.evaluate((t) => {
      localStorage.setItem("persona.theme", t);
      localStorage.setItem("persona.language", "en");
    }, theme);
    const opened = Date.now();
    await page.goto(`${base}/s/${CHAT}`);
    const content = page.locator("[data-chat-content]");
    const loading = content.getByText(LOADING);
    try {
      await loading.waitFor({ timeout: 15_000 });
      check(true, `${name}: the chat says it is loading while Paseo loads the agent`);
    } catch {
      check(false, `${name}: the chat says it is loading while Paseo loads the agent`, (await content.innerText().catch(() => "")).slice(0, 200));
    }
    if (shots) await page.screenshot({ path: join(shots, `web-paseo-not-loaded-loading-${name}.png`) });
    await content.getByText(ANSWER).waitFor({ timeout: 20_000 }).catch(() => {});
    const shown = await content.getByText(ANSWER).isVisible().catch(() => false);
    check(shown, `${name}: the history comes with the watch's backlog`, `${((Date.now() - opened) / 1000).toFixed(1)} s after opening`);
    check(!(await loading.isVisible().catch(() => false)), `${name}: no loading left once it is in`);
    check(!(await page.evaluate(() => window.__refusalShown)), `${name}: the refusal was never shown`);
    if (shots) {
      await page.waitForTimeout(400); // the lines' unfolding settles
      await page.screenshot({ path: join(shots, `web-paseo-not-loaded-history-${name}.png`) });
    }
    await ctx.close();
  }
} finally {
  await browser.close();
  clearTimeout(watchdog);
}
console.log(failures.length ? `VERDICT FAIL ${failures.length} check(s): ${failures.join("; ")}` : "VERDICT PASS paseo not loaded");
process.exit(failures.length ? 1 : 0);
