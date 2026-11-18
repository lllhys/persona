// Many-file Changes panel acceptance at phone width. Requires MOCK_TURN_CHANGES=1 and a loopback Vite/mock pair.
// Usage: node web/e2e/changes-panel-layout.mjs <loopback web origin> <loopback mock origin> <artifact directory> [chromium executable]
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";

const [webAddress, hubAddress, directory, executable] = process.argv.slice(2);
const web = new URL(webAddress), hub = new URL(hubAddress);
for (const base of [web, hub]) assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname));
const output = resolve(directory);
await mkdir(output, { recursive: true });
const health = await (await fetch(new URL("/api/v1/health", hub), { signal: AbortSignal.timeout(5000) })).json();
assert.equal(health.version, "mock");
const session = "/s/macbook/a1f3c2d4-0000-4000-8000-000000005157?token=mock";
const provider = "/Users/me/code/weather/WeatherWidget/Provider.swift";
const results = { status: "running", checks: [], screenshots: [], errors: [] };
let browser;

async function mock(path, params = {}) {
  const url = new URL(path, hub);
  for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
  const response = await fetch(url);
  assert.equal(response.status, 200, `${path} failed`);
}

async function run(theme) {
  await mock("/mock/reset");
  const context = await browser.newContext({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true, colorScheme: theme, locale: "en-US" });
  try {
    await context.addInitScript(theme => {
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", "en");
      localStorage.removeItem("persona.text-size");
    }, theme);
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    page.on("pageerror", error => results.errors.push(error.message));
    await page.goto(new URL(session, web).href);
    await page.locator("[data-chat-scroll] button").filter({ hasText: "Worked for" }).first().click();
    await page.locator("[data-action-row=Edit] > button").first().click();
    await page.locator("[data-change-call]").first().waitFor();
    for (let n = 1; n <= 12; n++) await mock("/mock/say", { user: `Many files ${n}`, edit: `/Users/me/code/weather/Many/File${n}.swift` });
    await page.locator('[data-change-scope="session"]').click();
    await page.waitForFunction(() => document.querySelectorAll("[data-change-file]").length >= 17);
    for (let n = 1; n <= 12; n++) await mock("/mock/say", { user: `Provider repetition ${n}`, edit: provider });
    await page.waitForFunction(() => {
      const diff = document.querySelector("[data-change-diff]");
      return document.querySelectorAll("[data-change-call]").length >= 14 && diff && diff.scrollHeight > diff.clientHeight;
    });
    const initial = await page.evaluate(() => {
      const panel = document.querySelector("[data-changes-panel]"), nav = panel.querySelector("nav"), diff = panel.querySelector("[data-change-diff]"), content = nav.parentElement;
      return { panelBottom: panel.getBoundingClientRect().bottom, contentClient: content.clientHeight, contentScroll: content.scrollHeight,
        navBottom: nav.getBoundingClientRect().bottom, diffTop: diff.getBoundingClientRect().top, diffBottom: diff.getBoundingClientRect().bottom,
        navClient: nav.clientHeight, navScroll: nav.scrollHeight, diffClient: diff.clientHeight, diffScroll: diff.scrollHeight };
    });
    assert.ok(initial.contentScroll <= initial.contentClient + 1, "the column must stay inside the panel");
    assert.ok(initial.panelBottom <= 845 && initial.diffBottom <= initial.panelBottom + 1);
    assert.ok(initial.diffTop >= initial.navBottom - 1, "file tree and diff must not overlap");
    assert.ok(initial.navScroll > initial.navClient && initial.diffScroll > initial.diffClient);
    const scrolled = await page.evaluate(() => {
      const nav = document.querySelector("[data-changes-panel] nav"), diff = document.querySelector("[data-change-diff]"), content = nav.parentElement;
      nav.scrollTop = nav.scrollHeight;
      diff.scrollTop = 120;
      return { navTop: nav.scrollTop, diffScrollTop: diff.scrollTop, diffTop: diff.getBoundingClientRect().top,
        contentClient: content.clientHeight, contentScroll: content.scrollHeight };
    });
    assert.ok(scrolled.navTop > 0 && scrolled.diffScrollTop > 0, "both regions must scroll independently");
    assert.equal(scrolled.diffTop, initial.diffTop);
    assert.ok(scrolled.contentScroll <= scrolled.contentClient + 1);
    const detail = { viewport: [390, 844], files: 17, calls: 14, initial, scrolled };
    results.checks.push({ name: `${theme} many-file phone layout`, detail });
    console.log(`PASS ${theme}: ${JSON.stringify(detail)}`);
    await page.screenshot({ path: join(output, `${theme}-390.png`) });
    results.screenshots.push(`${theme}-390.png`);
  } finally { await context.close(); }
}

const deadline = setTimeout(() => { results.status = "timeout"; void browser?.close(); }, 180_000);
try {
  browser = await chromium.launch({ headless: true, ...(executable ? { executablePath: executable } : { channel: "chrome" }) });
  for (const theme of ["light", "dark"]) await run(theme);
  assert.deepEqual(results.errors, []);
  results.status = "pass";
} catch (error) {
  results.status = results.status === "timeout" ? "timeout" : "fail";
  results.errors.push(error.stack);
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
  await browser?.close();
  await writeFile(join(output, "results.json"), JSON.stringify(results, null, 2) + "\n");
  console.log(`${results.status.toUpperCase()} changes-panel-layout: ${results.checks.length} checks`);
}
