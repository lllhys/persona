// Extension pages on reload (#9283, docs/web/web.md "Extension pages on reload") in a real browser against the mock hub:
// every request the hub relays to the manager (/api/v1/extensions…) is held for a second here, as a hub far away takes.
// The first load has nothing kept: no sidebar entry, the page's id in the header, the loading state. The second load
// draws the sidebar's "Today", the header's title and icon and the page's last view before those answers come back,
// marked as updating and inert, and the answers replace it. Synthetic data only; it writes nothing to the hub.
//
// Start the mock with MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1 and Vite (or `vite preview`) pointing at it
// (PERSONA_HUB_URL).
// node web/e2e/extension-first-paint.mjs <loopback web URL> <artifact directory> [--shots-only] [Chromium executable]
// --shots-only: only the screenshots of the second load's first paint (for a build without the cache, as "before").
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const shotsOnly = args.includes("--shots-only");
const [base, shots, executable] = args.filter((a) => a !== "--shots-only");
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/extension-first-paint.mjs <loopback web URL> <artifact directory> [--shots-only] [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: extension-first-paint timed out (180s)");
  process.exit(124);
}, 180_000);
const root = base.replace(/\/$/, "");
const PAGE = "/extension/example-today/today";
const RELAY_DELAY = 1000; // ms each relayed request is held
const results = [];
const check = (name, ok, detail) => {
  results.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${detail === undefined ? "" : ` ${JSON.stringify(detail)}`}`);
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });

/** When each part was first on screen, by the page's clock (Date.now), looked at every frame from the start of a load. */
const WATCH = () => {
  const seen = {};
  const visible = (el) => !!el && el.getClientRects().length > 0;
  const tick = () => {
    const now = Date.now();
    if (!seen.entry && visible(document.querySelector('a[href="/extension/example-today/today"]'))) seen.entry = now;
    const h1 = document.querySelector("header h1");
    if (!seen.title && h1?.textContent === "Today") seen.title = now;
    // the header's icon as first drawn (the tile before the title)
    if (h1 && seen.icon === undefined) seen.icon = h1.previousElementSibling?.innerHTML ?? "";
    if (!seen.kept && document.querySelectorAll("[data-kept-view] [data-columns] section").length >= 5) seen.kept = now;
    if (!seen.fresh && !document.querySelector("[data-kept-view]") && document.querySelectorAll("[data-columns] section").length >= 5) seen.fresh = now;
    window.__seen = seen;
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
};

/** One browser profile (its localStorage is what a reload keeps), each relayed request held RELAY_DELAY ms, and longer
 *  while `hold` is set (a first paint being photographed). */
async function profile(theme) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, colorScheme: theme, deviceScaleFactor: 2, serviceWorkers: "block" });
  await context.addInitScript((theme) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "en");
  }, theme);
  await context.addInitScript(WATCH);
  const relay = { released: [], hold: null };
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    // nothing leaves the loopback origin
    if (url.origin !== new URL(root).origin) return route.abort();
    if (!/^\/api\/v1\/(extensions|plugins)(\/|$)/.test(url.pathname)) return route.continue();
    await new Promise((resolve) => setTimeout(resolve, RELAY_DELAY));
    if (relay.hold) await relay.hold;
    relay.released.push(Date.now());
    return route.continue().catch(() => {}); // the page may have moved on
  });
  const p = await context.newPage();
  const errors = [];
  p.on("pageerror", (e) => errors.push(String(e)));
  await p.goto(`${root}/?token=mock`);
  await p.waitForFunction(() => localStorage.getItem("persona.token"));
  return { context, p, relay, errors };
}

/** A load of the page, until the hub's answers are drawn: when each part was first on screen, in ms from the start of
 *  the load, and when the first relayed answer was let through. `shot`: the first paint is photographed while every
 *  relayed request is still held. */
async function load(s, shot) {
  let release = () => {};
  s.relay.hold = shot ? new Promise((resolve) => (release = resolve)) : null;
  s.relay.released = [];
  const started = Date.now();
  await s.p.goto(root + PAGE, { waitUntil: "commit" });
  let held = true;
  if (shot) {
    // the app's shell (the page's header), then whatever the first paint holds: the kept view, or the loading state
    await s.p.locator("header h1").first().waitFor({ timeout: 20_000 });
    await s.p.locator("[data-kept-view] [data-columns] section").nth(4).waitFor({ timeout: 2500 }).catch(() => {});
    await s.p.waitForTimeout(400); // the cards' fade-up
    await s.p.screenshot({ path: shot });
    held = s.relay.released.length === 0;
    release();
    s.relay.hold = null;
  }
  await s.p.locator("[data-columns] section").nth(4).waitFor({ timeout: 20_000 });
  await s.p.waitForFunction(() => !document.querySelector("[data-kept-view]") && window.__seen?.fresh, null, { timeout: 20_000 });
  const seen = await s.p.evaluate(() => ({ ...window.__seen, iconNow: document.querySelector("header h1")?.previousElementSibling?.innerHTML ?? "" }));
  const at = (t) => (t ? t - started : null);
  return {
    held, answer: at(Math.min(...s.relay.released)), ...Object.fromEntries(["entry", "title", "kept", "fresh"].map((k) => [k, at(seen[k])])),
    iconFirst: seen.icon === seen.iconNow, // the page's own icon from the first paint
  };
}

let failed = false;
const timings = {};
try {
  for (const theme of ["light", "dark"]) {
    const s = await profile(theme);
    // first load: nothing kept yet; second: what the first kept; third: its first paint photographed
    const first = await load(s);
    const second = await load(s);
    const third = await load(s, join(shots, `first-paint-${shotsOnly ? "before" : "after"}-${theme}.png`));
    timings[theme] = { first, second, third };
    console.log(theme, JSON.stringify(timings[theme]));
    check(`${theme}: the photographed first paint came before any relayed answer`, third.held, third);
    if (!shotsOnly) {
      check(`${theme}: first load: the entry and the title wait for the relayed answer`, first.entry > first.answer && first.title > first.answer, first);
      check(`${theme}: second load: the sidebar entry before the first relayed answer`, second.entry !== null && second.entry < second.answer, second);
      check(`${theme}: second load: the header's title before the first relayed answer`, second.title !== null && second.title < second.answer, second);
      check(`${theme}: second load: the page's last view before the first relayed answer`, second.kept !== null && second.kept < second.answer, second);
      check(`${theme}: second load: the hub's drawing replaces the last view`, second.fresh > second.kept, second);
      check(`${theme}: first load: the header's icon is the generic one until the hub answers`, !first.iconFirst, first);
      check(`${theme}: second load: the header has the page's own icon from its first paint`, second.iconFirst, second);
    }
    check(`${theme}: no page errors`, s.errors.length === 0, s.errors);
    await s.context.close();
  }
} catch (e) {
  failed = true;
  console.error(e);
} finally {
  await browser.close();
  clearTimeout(watchdog);
}
writeFileSync(join(shots, shotsOnly ? "results-before.json" : "results.json"), JSON.stringify({ results, timings }, null, 2));
const bad = failed || results.some((r) => !r.ok);
console.log(`RESULT: extension-first-paint ${bad ? "failed" : "passed"} (${results.filter((r) => r.ok).length}/${results.length})`);
process.exit(bad ? 1 : 0);
