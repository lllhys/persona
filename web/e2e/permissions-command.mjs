// End-to-end check of `/permissions` on the web (#6476, docs/web/web.md, lib/permissions-command.ts): "/" in a running
// Claude Code Agent's composer lists /permissions; Enter on it opens the permission picker's menu with all five modes
// (Plan mode and Auto included), the current one checked, without sending anything; Auto switches the running session
// (POST …/modes) at once; Full access on an agent started without a bypass flag says the session restarts once its turn
// ends, asks first, then shows the wait (bypass_pending) until the fake hub's "restart" comes up in Full access.
//
// Needs the web page against a fresh fake hub (one per theme: the restart changes the session) and Chrome:
//
//   MOCK_BYPASS_RESTART_SECONDS=4 PORT=18797 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5197 --strictPort &)
//   node web/e2e/permissions-command.mjs http://127.0.0.1:5197 --hub http://127.0.0.1:18797 --theme light [--shots <dir>]
//
// Runs wide (1280) in Chinese; prints one line per check and a verdict; exits 1 on a failure, 2 after its 3-minute timeout.
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
const theme = flag("--theme") ?? "light";
const shots = flag("--shots");
if (!base || !hub) {
  console.error("usage: node web/e2e/permissions-command.mjs <web URL> --hub <fake hub URL> [--theme light|dark] [--shots <dir>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("TIMEOUT permissions-command: no verdict in 5 minutes");
  process.exit(2);
}, 300_000).unref();

const SID = "a1f3c2d4-0000-4000-8000-000000000006"; // a resting Claude Code agent in a pane, started without a bypass flag
const PATH = `/s/macbook/${SID}`;

const ok = (msg) => console.log(`ok  ${msg}`);
let browser;
async function fail(msg) {
  console.log(`FAIL ${theme}: ${msg}`);
  await browser?.close();
  process.exit(1);
}

const lastMode = async () => {
  const { modes } = await (await fetch(`${hub}/mock/modes`, { signal: AbortSignal.timeout(5000) })).json();
  return modes.filter((m) => m.session_id === SID).at(-1);
};

async function shot(page, name) {
  if (!shots) return;
  await page.waitForTimeout(350);
  await page.screenshot({ path: join(shots, `web-permissions-${name}-${theme}.png`) });
}

async function until(what, check, ms = 10_000) {
  const end = Date.now() + ms;
  for (;;) {
    if (await check()) return;
    if (Date.now() > end) await fail(what);
    await new Promise((r) => setTimeout(r, 150));
  }
}

browser = await chromium.launch({ executablePath: flag("--chromium") ?? "/usr/bin/chromium", args: ["--no-sandbox"] });
const ctx = await browser.newContext({ viewport: { width: 1280, height: 820 }, deviceScaleFactor: 2, colorScheme: theme, locale: "zh-CN" });
const page = await ctx.newPage();
await page.goto(`${base}/?token=mock`, { timeout: 120_000 });
await page.waitForSelector("main textarea", { timeout: 90_000 });
await page.evaluate((t) => {
  localStorage.setItem("persona.theme", t);
  localStorage.setItem("persona.language", "zh-Hans");
}, theme);
await page.goto(`${base}${PATH}`, { timeout: 120_000 });
const chip = page.locator('[data-testid="permission-picker"]');
await chip.waitFor({ state: "visible", timeout: 60_000 });
const box = page.locator("main textarea").last();
const menu = page.locator('[role="menu"]');
const modeRows = menu.locator('[role="menuitemradio"]');

// 1. "/" lists /permissions with Persona's other commands
await box.click();
await box.fill("/");
const palette = page.locator("[data-slash-palette]");
await palette.waitFor({ state: "visible", timeout: 10_000 });
const listed = await palette.locator("[role=option]").allTextContents();
if (!listed.some((r) => r.startsWith("permissions"))) await fail(`"/" does not list /permissions: ${JSON.stringify(listed)}`);
ok(`${theme}: "/" lists /permissions`);
await box.fill("/perm");
await until("/perm does not narrow the list to /permissions", async () => (await palette.locator("[role=option]").count()) === 1);
await shot(page, "palette");

// 2. Enter opens the menu with all five modes, the current one checked, and sends nothing
const sentBefore = (await (await fetch(`${hub}/mock/sent`)).json().catch(() => ({}))).sent?.length ?? 0;
await box.press("Enter");
await menu.waitFor({ state: "visible", timeout: 5000 });
const names = await modeRows.allTextContents();
for (const name of ["默认权限", "允许编辑", "计划模式", "自动审批", "完全访问"]) {
  if (!names.some((n) => n.startsWith(name))) await fail(`the menu lacks ${name}: ${JSON.stringify(names)}`);
}
const checked = await menu.locator('[role="menuitemradio"][aria-checked="true"]').allTextContents();
if (checked.length !== 1 || !checked[0].startsWith("默认权限")) await fail(`checked rows ${JSON.stringify(checked)}`);
if ((await box.inputValue()) !== "") await fail("the command stayed in the box");
const sentAfter = (await (await fetch(`${hub}/mock/sent`)).json().catch(() => ({}))).sent?.length ?? 0;
if (sentAfter !== sentBefore) await fail("/permissions was sent to the agent");
const restartNote = await menu.locator('[role="menuitemradio"]', { hasText: "完全访问" }).textContent();
if (!restartNote.includes("重启会话")) await fail(`Full access does not say it restarts: ${restartNote}`);
ok(`${theme}: Enter opens five modes (Plan, Auto included), Default checked, Full access says it restarts; nothing sent`);
await shot(page, "menu");

// 3. Auto switches at once
await modeRows.filter({ hasText: "自动审批" }).click();
await until("Auto was not posted", async () => (await lastMode())?.permission_mode === "auto");
await until("the chip does not show Auto", async () => (await chip.getAttribute("data-mode")) === "auto");
ok(`${theme}: Auto switches the running session (POST …/modes {permission_mode: auto})`);
await shot(page, "auto");

// 4. Full access: asks first with the restart note, then waits for the restart, then runs in it
await box.fill("/permissions");
await box.press("Escape"); // the palette, not the menu, takes this Enter otherwise
await box.press("Enter");
await menu.waitFor({ state: "visible", timeout: 5000 });
await menu.locator('[role="menuitemradio"]', { hasText: "完全访问" }).click();
await until("Full access did not ask first", async () => (await menu.textContent())?.includes("确认切到完全访问"));
if ((await lastMode())?.permission_mode !== "auto") await fail("Full access was posted before it was confirmed");
await shot(page, "confirm");
await menu.locator('[role^="menuitem"]', { hasText: "确认切到完全访问" }).click();
await until("Full access was not posted", async () => (await lastMode())?.permission_mode === "bypassPermissions");
await until("the chip does not show the wait", async () => (await chip.getAttribute("data-pending")) === "bypassPermissions");
if ((await chip.getAttribute("data-mode")) !== "auto") await fail("the chip shows a mode the agent does not run in yet");
const tip = await chip.getAttribute("title");
if (!tip?.includes("重启会话")) await fail(`the chip's hover does not say it restarts: ${tip}`);
await chip.click();
await menu.waitFor({ state: "visible", timeout: 5000 });
ok(`${theme}: Full access asks first, then waits for the restart (bypass_pending), the chip still on Auto`);
await shot(page, "pending");
await page.keyboard.press("Escape");
await until("the restart did not come up in Full access", async () => (await chip.getAttribute("data-mode")) === "bypassPermissions" && !(await chip.getAttribute("data-pending")), 20_000);
ok(`${theme}: after the restart the session runs in Full access`);
await shot(page, "bypass");

await browser.close();
console.log(`PASS permissions-command (${theme})`);
