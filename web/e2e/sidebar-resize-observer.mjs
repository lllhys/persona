// Real Chromium startup regression for the sidebar's shared heading ResizeObserver (#7701).
// Run against a synthetic hub with the production web bundle, not a real account or a jsdom approximation.
import assert from "node:assert/strict";
import { chromium } from "playwright-core";

const origin = process.argv[2];
if (!/^http:\/\/(127\.0\.0\.1|localhost):\d+(?:\/.*)?$/.test(origin ?? "")) {
  console.log("FAIL sidebar-resize-observer: give the web page's loopback origin");
  process.exit(1);
}
const errors = [];
const browser = await chromium.launch({ channel: "chrome", headless: true });
const deadline = setTimeout(() => {
  console.log("FAIL sidebar-resize-observer: timed out after 60 seconds");
  process.exit(1);
}, 60_000);
try {
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: "zh-CN" });
  const page = await context.newPage();
  page.on("console", message => { if (message.type() === "error") errors.push(message.text()); });
  page.on("pageerror", error => errors.push(String(error)));
  await page.goto(new URL(`?token=mock`, origin).href);
  await page.locator("[data-chat-content] [data-answer]").first().waitFor({ timeout: 30_000 });
  await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
  assert.deepEqual(errors.filter(text => text.includes("ResizeObserver")), [], "the initial sidebar measurement must not produce a resize loop");
  const headings = await page.locator("[data-sidebar-group-heading]").evaluateAll(elements => elements.map(element => ({
    top: Number.parseFloat(/([-\d.]+)px\)$/.exec(element.style.top)?.[1] ?? "0"),
    height: element.getBoundingClientRect().height,
  })));
  assert.ok(headings.length > 1, "the synthetic hub has nested sidebar groups");
  const measured = await page.locator("[data-sidebar-group-heading]").evaluateAll(elements => elements.map(element => {
    let ancestor = element.parentElement.parentElement;
    while (ancestor && !ancestor.querySelector(":scope > [data-sidebar-group-heading]")) ancestor = ancestor.parentElement;
    return {
      top: Number.parseFloat(/([-\d.]+)px\)$/.exec(element.style.top)?.[1] ?? "0"),
      parentHeight: ancestor?.querySelector(":scope > [data-sidebar-group-heading]")?.getBoundingClientRect().height ?? 0,
      depth: 24 - Number.parseInt(element.style.zIndex || "24", 10),
    };
  }));
  const nested = measured.find(element => element.depth > 0);
  assert.ok(nested, "a nested heading has a positive measured ancestor offset");
  assert.equal(nested.top, nested.parentHeight, "the nested heading uses its direct parent's measured border-box height after React batches the observer report");
  await page.locator("[data-row-scroller]").evaluate(element => { element.scrollTop = 220; });
  await page.evaluate(() => new Promise(done => requestAnimationFrame(() => requestAnimationFrame(done))));
  assert.equal(await page.locator("[data-row-scroller]").evaluate(element => element.scrollTop), 220, "the regression really scrolls the sidebar");
  const pinned = await page.locator("[data-sidebar-group-heading]").evaluateAll(elements => elements.map(element => ({
    top: Number.parseFloat(/([-\d.]+)px\)$/.exec(element.style.top)?.[1] ?? "0"),
    height: element.getBoundingClientRect().height,
    viewportTop: element.getBoundingClientRect().top,
  })));
  assert.equal(pinned.at(-1).top, nested.parentHeight, "scrolling does not change the nested heading's measured parent offset");
  assert.ok(pinned.every(element => element.height > 0 && Number.isFinite(element.viewportTop)), "the measured headings remain laid out after scrolling");
  assert.deepEqual(errors.filter(text => text.includes("ResizeObserver")), [], "scrolling must not reintroduce a resize loop");
  assert.equal(await page.locator("[data-sidebar-group-heading]").count() > 0, true, "the shared heading observer still renders its headings");
  console.log("PASS sidebar-resize-observer: no ResizeObserver warning; nested heading offsets stay correct after scrolling");
} catch (error) {
  console.error("FAIL sidebar-resize-observer:", error);
  process.exitCode = 1;
} finally {
  clearTimeout(deadline);
  await browser.close();
}
