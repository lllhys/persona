// End-to-end check of a reply's Sources row (#9132, docs/web/web.md "Sources"): the main chat's last reply looked Kyoto's
// weather up on the web, and under it a Sources row shows three pages (each by its site, its title in the tooltip and
// spoken name), +2 shows the other two and Show less folds them again; a chip is a link to the page that opens in a new
// tab, and the row stays inside the window.
//
// Needs the web page against the fake hub with its web-sources exchange, and Chrome (playwright-core drives it):
//
//   MOCK_WEB_SOURCES=1 PORT=18932 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18932 npx vite --port 5932 --strictPort &)
//   node web/e2e/turn-sources.mjs http://127.0.0.1:5932 [--shots <dir>] [--channel msedge]
//
// Runs wide (1280) and phone width (390), light and dark, in English, then 1280 light in Chinese and 390 light at the
// largest text size; --shots saves each as web-turn-sources-<state>-<width>-<theme>[-zh|-large].png. Prints one line per
// check and a verdict; exits 1 on a failure.
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : undefined;
};
const base = (args[0] ?? "").replace(/\/$/, "");
const shots = flag("--shots");
const channel = flag("--channel") ?? "chrome";
if (!base) {
  console.error("usage: node web/e2e/turn-sources.mjs <web URL> [--shots <dir>] [--channel msedge]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

// the words the checks look for, as the catalogs say them
const WORDS = {
  en: { locale: "en-US", label: "Sources", more: "+2", less: "Show less" },
  "zh-Hans": { locale: "zh-CN", label: "来源", more: "+2", less: "收起" },
};
const FIRST = { text: "Kyoto 10-day forecast", site: "weather.example.com", url: "https://weather.example.com/kyoto/10-day" };

let failed = false;
const ok = (msg) => console.log(`ok   ${msg}`);
function fail(msg) {
  failed = true;
  throw new Error(msg);
}

async function run(browser, { theme, width, lang, size }) {
  const w = WORDS[lang];
  const tag = `${width}/${theme}/${lang}${size ? `/${size}` : ""}`;
  const ctx = await browser.newContext({
    viewport: { width, height: width > 600 ? 900 : 844 }, deviceScaleFactor: 2, colorScheme: theme, locale: w.locale, hasTouch: width <= 600,
  });
  try {
    const page = await ctx.newPage();
    await page.goto(`${base}/?token=mock`);
    await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15000 });
    await page.evaluate(({ theme, lang, size }) => {
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", lang);
      if (size) localStorage.setItem("persona.text-size", size);
    }, { theme, lang, size });
    await page.reload();
    const row = page.locator("[data-turn-sources]").last();
    // a cold dev server compiles the chat and the row's chunk on first load
    await row.waitFor({ timeout: 45000 }).catch(() => fail(`${tag}: no Sources row under the last reply`));
    await row.scrollIntoViewIfNeeded();
    if ((await row.getAttribute("aria-label")) !== w.label) fail(`${tag}: the row is labelled ${await row.getAttribute("aria-label")}`);
    const chips = row.locator("[data-source]");
    if ((await chips.count()) !== 3) fail(`${tag}: ${await chips.count()} chips before +N, want 3`);
    const first = chips.first();
    const text = (await first.textContent()) ?? "";
    if (text !== FIRST.site || !(await first.getAttribute("aria-label"))?.startsWith(FIRST.text)) fail(`${tag}: the first chip says ${text} (${await first.getAttribute("aria-label")})`);
    if ((await first.getAttribute("href")) !== FIRST.url || (await first.getAttribute("target")) !== "_blank") fail(`${tag}: the first chip links to ${await first.getAttribute("href")}`);
    const box = await row.boundingBox();
    if (!box || box.x + box.width > width + 1) fail(`${tag}: the row overflows the window`);
    if (shots) await page.screenshot({ path: join(shots, `web-turn-sources-row-${width}-${theme}${lang === "en" ? "" : "-zh"}${size ? `-${size}` : ""}.png`) });
    ok(`${tag}: three pages under the last reply, the first "${FIRST.text}" from ${FIRST.site}, a link opening in a new tab`);
    await row.getByRole("button", { name: lang === "en" ? "Show 2 more sources" : "再显示 2 个来源", exact: true }).click();
    if ((await chips.count()) !== 5) fail(`${tag}: +N shows ${await chips.count()} pages, want 5`);
    if (shots) await page.screenshot({ path: join(shots, `web-turn-sources-expanded-${width}-${theme}${lang === "en" ? "" : "-zh"}${size ? `-${size}` : ""}.png`) });
    await row.getByRole("button", { name: w.less, exact: true }).click();
    if ((await chips.count()) !== 3) fail(`${tag}: ${w.less} leaves ${await chips.count()} pages`);
    ok(`${tag}: ${w.more} shows all five, ${w.less} folds them again`);
  } finally {
    await ctx.close();
  }
}

const PASSES = [
  ...[1280, 390].flatMap((width) => ["light", "dark"].map((theme) => ({ width, theme, lang: "en" }))),
  { width: 1280, theme: "light", lang: "zh-Hans" },
  { width: 390, theme: "light", lang: "en", size: "large" },
];
const browser = await chromium.launch({ channel });
try {
  for (const pass of PASSES) await run(browser, pass);
} catch (e) {
  failed = true;
  console.log(`FAIL ${e instanceof Error ? e.message.split("\n")[0] : String(e)}`);
} finally {
  await browser.close();
}
console.log(failed ? "verdict: FAIL" : "verdict: PASS (Sources row)");
process.exit(failed ? 1 : 0);
