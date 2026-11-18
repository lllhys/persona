// Checks and screenshots of Run agent here offering Codex (#5504): on a computer whose Codex launchers say run_here the
// shell's Run agent here lists Codex and starts it in that shell; on one whose don't (an older node) it lists Claude
// Code only, as before.
//
// Needs the web page against the fake hub with MOCK_TERMINAL_PANES=1, and Edge or Chrome (playwright-core drives it):
//
//   MOCK_TERMINAL_PANES=1 MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_RUN_HERE_CODEX=server,macbook,mini PORT=18927 \
//     python3 scripts/mock/mock_hub.py &   # computers that say run_here
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18927 npx vite --port 5927 --strictPort &)
//   node web/e2e/run-here-codex.mjs http://127.0.0.1:5927 --expect offered [--shots <dir>] [--channel msedge]
//
// --expect offered: Codex is listed and picking it starts it in the shell (panes/agent with launcher codex);
// --expect absent: a mock without MOCK_RUN_HERE_CODEX, Codex is not listed; --before: screenshots only (main's page).
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
const expect = flag("--expect") ?? "offered";
const before = args.includes("--before");
const channel = flag("--channel") ?? "chrome";
if (!base || !["offered", "absent"].includes(expect)) {
  console.error("usage: node web/e2e/run-here-codex.mjs <web URL> --expect offered|absent [--shots <dir>] [--before] [--channel msedge]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

const SIZES = { wide: { width: 1280, height: 860 }, narrow: { width: 390, height: 844 } };
const RUNS = [["wide", "light"], ["wide", "dark"], ["narrow", "light"], ["narrow", "dark"]];
const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

async function run(browser, size, theme) {
  const tag = `${size}-${theme}`;
  const ctx = await browser.newContext({ viewport: SIZES[size], deviceScaleFactor: 2, hasTouch: size === "narrow", colorScheme: theme, locale: "zh-CN" });
  const page = await ctx.newPage();
  await page.goto(`${base}/?token=mock`);
  await page.waitForSelector("main textarea", { timeout: 15000 }).catch(() => fail(`${tag}: did not pair with the fake hub`));
  await page.evaluate((t) => localStorage.setItem("persona.theme", t), theme);
  const posts = [];
  page.on("request", (r) => {
    if (r.method() === "POST" && r.url().includes("/panes/agent")) posts.push(r.postDataJSON());
  });
  await page.goto(`${base}/terminal`);
  // the page opens on its sessions; its Terminals segment lists the workspaces, tabs and panes
  await page.getByRole("radio", { name: "终端", exact: true }).first().click({ timeout: 8000 }).catch(() => fail(`${tag}: no Terminals segment on the page`));
  await page.locator("[data-terminals-list]").waitFor({ timeout: 8000 }).catch(() => fail(`${tag}: the terminals did not list`));
  // a free shell's row; its ⋯ shows under the pointer (always on a touch screen)
  const row = page.locator("main a").filter({ hasText: "zsh" }).filter({ visible: true }).first();
  await row.waitFor({ timeout: 10000 }).catch(async () => {
    if (shots) await page.screenshot({ path: join(shots, `failed-${tag}.png`) });
    fail(`${tag}: no free shell on the Terminal page (run the mock with MOCK_TERMINAL_PANES=1)`);
  });
  await row.hover().catch(() => {});
  await row.locator("xpath=..").getByRole("button", { name: "zsh 的操作" }).click({ timeout: 4000 }).catch(() => fail(`${tag}: no ⋯ on the shell's row`));
  await page.getByRole("menuitem", { name: "在这里运行 Agent…" }).click({ timeout: 4000 }).catch(() => fail(`${tag}: no Run agent here in the shell's menu`));
  const rows = page.getByRole("menuitem").filter({ hasText: /^(claude|sonnetcc|codex)/ });
  await rows.first().waitFor({ timeout: 6000 }).catch(() => fail(`${tag}: the launchers did not load in Run agent here`));
  await page.waitForTimeout(400); // the submenu's entrance
  if (shots) await page.screenshot({ path: join(shots, `web-runhere-${expect}-${tag}.png`) });
  if (before) {
    ok(`${tag}: shot`);
    return ctx.close();
  }
  const names = await rows.evaluateAll((els) => els.map((e) => (e.textContent ?? "").trim()));
  const codex = names.filter((n) => n.startsWith("codex"));
  if (expect === "absent") {
    if (codex.length) fail(`${tag}: Codex offered by a computer that does not say run_here: ${JSON.stringify(names)}`);
    ok(`${tag}: Codex not offered where the computer does not say run_here`);
    return ctx.close();
  }
  if (codex.length !== 1 || !names.some((n) => n.startsWith("claude"))) fail(`${tag}: Run agent here lists ${JSON.stringify(names)}`);
  ok(`${tag}: Run agent here lists Codex next to Claude Code`);
  await rows.filter({ hasText: /^codex/ }).first().click({ timeout: 4000 });
  for (let i = 0; i < 40 && !posts.length; i++) await page.waitForTimeout(100);
  if (posts.length !== 1 || posts[0].launcher !== "codex" || !posts[0].pane) fail(`${tag}: picking Codex posted ${JSON.stringify(posts)}`);
  ok(`${tag}: picking Codex starts it in the shell (panes/agent ${posts[0].pane})`);
  await ctx.close();
}

const browser = await chromium.launch({ channel, headless: true });
try {
  for (const [size, theme] of RUNS) await run(browser, size, theme);
} finally {
  await browser.close();
}
console.log(`PASS run here codex (${expect}${before ? ", before" : ""})`);
