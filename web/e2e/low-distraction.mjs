// Settings › Notifications › Low-distraction mode (#8157) against the fake hub: the hub's switch is the page's last group,
// after the note that the rows above are this browser's; it reads what GET /api/v1/push/ln-mode says, a tap writes it
// (the fake hub keeps it, so its GET answers the new value), and a second tap puts it back. Wide (1280) and narrow
// (390), light and dark, in Chinese, plus English wide and light; --shots saves the group off and on. Last, a hub without
// the route (the page's ln-mode request answered 404 in the browser): the group is there and says to update the hub.
//
//   PORT=18797 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5197 --strictPort &)
//   node web/e2e/low-distraction.mjs http://127.0.0.1:5197 --hub http://127.0.0.1:18797 [--shots <dir>] [--chromium <path>]
//
// Prints one line per check and a verdict; exits 1 on a failure. It only writes the fake hub's switch, and leaves it off.
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
const executablePath = flag("--chromium");
if (!base || !hub) {
  console.error("usage: node web/e2e/low-distraction.mjs <web URL> --hub <fake hub URL> [--shots <dir>] [--chromium <path>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

const TEXT = {
  "zh-Hans": { label: "低打扰模式", note: "这些设置跟着这个浏览器走", footer: "对你所有的设备生效", older: "这台 hub 还不支持低打扰模式" },
  en: { label: "Low-distraction mode", note: "These settings belong to this browser", footer: "For all your devices", older: "This hub doesn't support low-distraction mode yet" },
};

async function hubSwitch(body) {
  const r = await fetch(`${hub}/api/v1/push/ln-mode`, {
    method: body ? "PUT" : "GET", headers: { Authorization: "Bearer mock", "Content-Type": "application/json" },
    body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(5000),
  });
  return (await r.json()).enabled === true;
}

async function run(browser, lang, theme, width) {
  const t = TEXT[lang];
  const tag = `${lang}-${width}-${theme}`;
  if (await hubSwitch({ enabled: false })) fail(`${tag}: the fake hub did not start off`);
  const ctx = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 2, colorScheme: theme, locale: lang === "en" ? "en-US" : "zh-CN" });
  const page = await ctx.newPage();
  await page.goto(`${base}/?token=mock`);
  await page.waitForSelector("main", { timeout: 20000 });
  await page.evaluate(([th, l]) => {
    localStorage.setItem("persona.theme", th);
    localStorage.setItem("persona.language", l);
  }, [theme, lang]);
  await page.goto(`${base}/settings?tab=notifications`);
  const toggle = page.getByRole("switch", { name: t.label, exact: true });
  try {
    await toggle.waitFor({ state: "visible", timeout: 15000 });
  } catch {
    fail(`${tag}: no low-distraction switch on Settings › Notifications`);
  }
  const group = page.locator('[data-settings-group="low-distraction"]');
  await group.scrollIntoViewIfNeeded();
  if ((await toggle.getAttribute("aria-checked")) !== "false") fail(`${tag}: the switch does not read the hub's off`);
  const order = await page.evaluate(([note, footer]) => {
    const text = document.querySelector("main")?.textContent ?? document.body.textContent ?? "";
    return [text.indexOf(note), text.indexOf(footer)];
  }, [t.note, t.footer]);
  if (order[0] < 0 || order[1] <= order[0]) fail(`${tag}: the group is not after the note that the rows above are this browser's (${order})`);
  ok(`${tag}: last group, off as the hub says, with the footer`);
  if (shots) {
    await page.waitForTimeout(300);
    await page.screenshot({ path: join(shots, `web-low-distraction-${tag}-off.png`) });
  }

  await toggle.click();
  await page.waitForFunction((name) => document.querySelector(`[role="switch"][aria-label="${name}"]`)?.getAttribute("aria-checked") === "true", t.label, { timeout: 8000 });
  if (!(await hubSwitch())) fail(`${tag}: GET /api/v1/push/ln-mode does not say on after the tap`);
  ok(`${tag}: a tap turns the hub's switch on`);
  if (shots) {
    await page.waitForTimeout(300);
    await page.screenshot({ path: join(shots, `web-low-distraction-${tag}-on.png`) });
  }
  await toggle.click();
  await page.waitForFunction((name) => document.querySelector(`[role="switch"][aria-label="${name}"]`)?.getAttribute("aria-checked") === "false", t.label, { timeout: 8000 });
  if (await hubSwitch()) fail(`${tag}: the second tap did not turn it back off`);
  ok(`${tag}: a second tap turns it back off`);
  await ctx.close();
}

async function olderHub(browser, lang, theme, width) {
  const t = TEXT[lang];
  const tag = `${lang}-${width}-${theme}-older-hub`;
  const ctx = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 2, colorScheme: theme, locale: lang === "en" ? "en-US" : "zh-CN" });
  const page = await ctx.newPage();
  await page.route("**/api/v1/push/ln-mode", (route) => route.fulfill({ status: 404, contentType: "application/json", body: '{"error":"not found"}' }));
  await page.goto(`${base}/?token=mock`);
  await page.waitForSelector("main", { timeout: 20000 });
  await page.evaluate(([th, l]) => {
    localStorage.setItem("persona.theme", th);
    localStorage.setItem("persona.language", l);
  }, [theme, lang]);
  await page.goto(`${base}/settings?tab=notifications`);
  const group = page.locator('[data-settings-group="low-distraction"]');
  try {
    await group.getByText(t.older).waitFor({ state: "visible", timeout: 15000 });
  } catch {
    fail(`${tag}: the group does not say to update the hub`);
  }
  if (await page.getByRole("switch", { name: t.label, exact: true }).count()) fail(`${tag}: a switch on a hub without one`);
  await group.scrollIntoViewIfNeeded();
  ok(`${tag}: the group is there with no switch, and says to update the hub`);
  if (shots) {
    await page.waitForTimeout(300);
    await page.screenshot({ path: join(shots, `web-low-distraction-${tag}.png`) });
  }
  await ctx.close();
}

const browser = await chromium.launch(executablePath ? { executablePath, headless: true } : { channel: "chrome", headless: true });
try {
  for (const theme of ["light", "dark"]) for (const width of [1280, 390]) await run(browser, "zh-Hans", theme, width);
  await run(browser, "en", "light", 1280);
  for (const theme of ["light", "dark"]) await olderHub(browser, "zh-Hans", theme, 1280);
  console.log("PASS low-distraction");
} finally {
  await hubSwitch({ enabled: false }).catch(() => {});
  await browser.close();
}
