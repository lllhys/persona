// Screenshots and checks of the inbox's folders (#2463, docs/assistant/inbox.md "Folders"): the folder bar's crumbs and
// subfolders, picking a folder (the list keeps what is in it and under it), a row's "Move to folder…" and the folder
// settings.
//
// Needs the web page against the fake hub with MOCK_INBOX_FOLDERS=1, and Chrome (playwright-core drives it):
//
//   MOCK_INBOX_FOLDERS=1 MOCK_GITHUB_INBOX=1 PORT=18796 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18796 npx vite --port 5196 --strictPort &)
//   node web/e2e/inbox-folders.mjs http://127.0.0.1:5196 [--shots <dir>] [--locale en-US|zh-CN|ja-JP] [--chrome <path>]
//
// Runs wide (1280) and phone width (390), light and dark. Prints one line per check and a verdict; exits 1 on a failure.
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
const locale = flag("--locale") ?? "en-US";
const chrome = flag("--chrome") ?? process.env.PERSONA_CHROME;
if (!base) {
  console.error("usage: node web/e2e/inbox-folders.mjs <web URL> [--shots <dir>] [--locale en-US|zh-CN|ja-JP] [--chrome <path>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

const lang = locale.startsWith("zh") ? "zh" : locale.startsWith("ja") ? "ja" : "en";
const WORDS = {
  en: { all: "All folders", move: "Move to folder…", settings: "Folders…" },
  zh: { all: "全部文件夹", move: "移到文件夹…", settings: "文件夹…" },
  ja: { all: "すべてのフォルダ", move: "フォルダに移動…", settings: "フォルダ…" },
}[lang];
const SIZES = { wide: { width: 1280, height: 860 }, narrow: { width: 390, height: 844 } };
const RUNS = [["wide", "light"], ["wide", "dark"], ["narrow", "light"], ["narrow", "dark"]];

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

async function shot(page, name, tag) {
  if (!shots) return;
  await page.waitForTimeout(400); // entrances
  await page.screenshot({ path: join(shots, `${name}-${tag}.png`) });
}

const rows = (page) => page.locator("[data-item]").filter({ visible: true });

async function run(browser, size, theme) {
  const tag = `${size}-${theme}`;
  const ctx = await browser.newContext({ viewport: SIZES[size], deviceScaleFactor: 2, hasTouch: size === "narrow", colorScheme: theme, locale });
  await ctx.addInitScript(() => localStorage.removeItem("inbox.folder"));
  const page = await ctx.newPage();
  await page.goto(`${base}/inbox?token=mock`); // paired, the token traded for this browser's own
  const bar = page.locator("[data-folder-bar]");
  await bar.waitFor({ timeout: 10000 }).catch(() => fail(`${tag}: no folder bar`));
  const all = await rows(page).count();
  const top = await bar.locator('[role="toolbar"] button').allTextContents();
  if (!top.some((x) => x.startsWith("projects")) || !top.some((x) => x.startsWith("work"))) fail(`${tag}: top folders are ${JSON.stringify(top)}`);
  ok(`${tag}: the top folders are offered (${top.length} chips, ${all} items)`);
  if (await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth)) fail(`${tag}: the page scrolls sideways`);
  await shot(page, "inbox-folders-top", tag);

  await bar.locator('[role="toolbar"] button', { hasText: /^projects/ }).click();
  await bar.locator('[role="toolbar"] button', { hasText: /^atlas/ }).click();
  const crumbs = await bar.locator("ol button").allTextContents();
  if (JSON.stringify(crumbs) !== JSON.stringify([WORDS.all, "projects", "atlas"])) fail(`${tag}: crumbs are ${JSON.stringify(crumbs)}`);
  const inAtlas = await rows(page).count();
  if (inAtlas !== 2) fail(`${tag}: ${inAtlas} items in projects/atlas, want 2`);
  ok(`${tag}: projects › atlas keeps its 2 items`);
  await shot(page, "inbox-folders-atlas", tag);

  // a row's menu: Move to folder…
  const row = rows(page).first();
  if (size === "narrow") await row.click({ button: "right" }).catch(() => {});
  else await row.click({ button: "right" });
  await page.getByRole("menuitem", { name: new RegExp(WORDS.move.replace("…", "")) }).click();
  const dialog = page.getByRole("dialog");
  await dialog.waitFor({ timeout: 5000 }).catch(() => fail(`${tag}: no Move to folder dialog`));
  await dialog.locator("input").fill("work/");
  ok(`${tag}: Move to folder opens with suggestions`);
  await shot(page, "inbox-folders-move", tag);
  await page.keyboard.press("Escape");

  // ⋯ › Folders…
  await bar.locator("ol button").first().click();
  // the ⋯ at the end of the filter chips
  await page.locator('[role="toolbar"]').first().locator("xpath=following-sibling::button").first().click();
  await page.getByRole("menuitem", { name: WORDS.settings }).click();
  await page.getByRole("dialog").waitFor({ timeout: 5000 }).catch(() => fail(`${tag}: no folder settings`));
  const on = await page.getByRole("switch").getAttribute("aria-checked");
  if (on !== "true") fail(`${tag}: automatic folders show ${on}, the mock has them on`);
  ok(`${tag}: folder settings show automatic folders on`);
  await shot(page, "inbox-folders-settings", tag);
  await ctx.close();
}

const browser = await chromium.launch(chrome ? { executablePath: chrome, headless: true, args: ["--no-sandbox"] } : { channel: "chrome", headless: true });
const deadline = setTimeout(() => {
  console.log("FAIL inbox-folders: timed out after 3 minutes");
  process.exit(1);
}, 180000);
try {
  for (const [size, theme] of RUNS) await run(browser, size, theme);
  console.log("PASS inbox-folders");
} finally {
  clearTimeout(deadline);
  await browser.close();
}
