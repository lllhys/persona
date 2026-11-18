// The start page (#9071) in a real browser against the mock hub: the app opens on the main chat by default; choosing
// an extension's Today page in Settings › General › Start page saves it to the hub, a new tab opens on Today, a reload
// and the Persona row stay on the main chat; a second browser (another device) reads the choice at its hello and opens on
// Today from its next opening; a Today the hub no longer lists gives way to the main chat and the setting says why.
// `--old-hub`: a hub without the route (the mock with MOCK_NAVIGATION=off) keeps the choice in the browser and says so.
// Light and dark screenshots of the setting and of the app opened on Today, at 1440 × 900 and 390 × 844.
//
// Start the mock with MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1 (and MOCK_NAVIGATION=off for --old-hub), and Vite (or
// `vite preview`) pointing at it (PERSONA_HUB_URL). The run sets the mock's start page and puts it back to the default.
// node web/e2e/start-page.mjs <loopback web URL> <artifact directory> [--old-hub] [Chromium executable]
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const oldHub = args.includes("--old-hub");
const [base, shots, executable] = args.filter((a) => a !== "--old-hub");
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/start-page.mjs <loopback web URL> <artifact directory> [--old-hub] [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: start-page timed out (240s)");
  process.exit(124);
}, 240_000);
const root = base.replace(/\/$/, "");
const TODAY = "p:example-today/today";
const TODAY_PATH = "/extension/example-today/today";
const results = [];
const check = (name, ok, detail) => {
  results.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${detail === undefined ? "" : ` ${JSON.stringify(detail)}`}`);
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });

/** A new browser (another device): signed in, its first opening done, nothing else kept. */
async function device({ width, height, theme = "light", hideToday = false }) {
  const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme, deviceScaleFactor: 2, serviceWorkers: "block", hasTouch: width < 640 });
  await context.addInitScript((theme) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "en");
    // the addresses the page put in place of the one it was opened at, in order
    const replace = history.replaceState;
    history.replaceState = function (...a) {
      (window.__replaced ??= []).push(new URL(String(a[2]), location.href).pathname);
      return replace.apply(this, a);
    };
  }, theme);
  // nothing leaves the loopback origin
  await context.route("**/*", (route) => (new URL(route.request().url()).origin === new URL(root).origin ? route.continue() : route.abort()));
  if (hideToday) {
    // the hub no longer lists Today (its extension removed or turned off)
    await context.route("**/api/v1/extensions/-/ui*", async (route) => {
      const response = await route.fetch();
      const body = await response.json();
      body.ui = (body.ui ?? []).filter((u) => (u.extension ?? u.plugin) !== "example-today");
      await route.fulfill({ response, json: body });
    });
  }
  const p = await context.newPage();
  await p.goto(`${root}/?token=mock`);
  await p.waitForFunction(() => localStorage.getItem("persona.token"));
  return { context, p };
}

/** The hub's start page, read with the mock's token. */
const hubStart = async (p) =>
  p.evaluate(async () => {
    const r = await fetch("/api/v1/navigation", { headers: { Authorization: `Bearer ${localStorage.getItem("persona.token")}` } });
    return r.status === 200 ? (await r.json()).start : `HTTP ${r.status}`;
  });
const putHubStart = (p, start) =>
  p.evaluate(async (start) => {
    await fetch("/api/v1/navigation", { method: "PUT", headers: { Authorization: `Bearer ${localStorage.getItem("persona.token")}`, "Content-Type": "application/json" }, body: JSON.stringify({ start }) });
  }, start);

/** A new tab of the same browser opened at the root address, settled on whatever page it opened. */
async function openRoot(context) {
  const p = await context.newPage();
  await p.goto(`${root}/`);
  await p.waitForLoadState("networkidle");
  return p;
}

const settingsPath = (width) => (width < 640 ? "/settings?tab=general" : "/settings");
async function showSetting(p, width) {
  await p.goto(root + settingsPath(width));
  const group = p.locator('[data-settings-group="start-page"]');
  await group.waitFor({ timeout: 20_000 });
  await group.evaluate((e) => e.scrollIntoView({ block: "center" }));
  return group;
}

let failed = false;
try {
  if (oldHub) {
    const { context, p } = await device({ width: 1440, height: 900 });
    check("the hub has no navigation route", (await hubStart(p)) === "HTTP 404", await hubStart(p));
    const group = await showSetting(p, 1440);
    check("the setting says the choice stays in this browser", (await group.textContent()).includes("Saved in this browser only"), await group.locator("p").last().textContent());
    await group.locator("select").selectOption("inbox");
    await p.waitForFunction(() => localStorage.getItem("persona.startPage") === "inbox");
    await p.screenshot({ path: join(shots, "setting-old-hub-light-1440.png") });
    const tab = await openRoot(context);
    check("a new tab opens on the page kept in this browser", new URL(tab.url()).pathname === "/inbox", tab.url());
    await context.close();
  } else {
    // a fresh hub: nothing chosen
    const laptop = await device({ width: 1440, height: 900 });
    await putHubStart(laptop.p, "");
    const first = await openRoot(laptop.context);
    check("the default is the main chat", new URL(first.url()).pathname === "/", first.url());
    await first.close();

    const group = await showSetting(laptop.p, 1440);
    const select = group.locator("select");
    check("Open on shows the main chat by default", (await select.inputValue()) === "", await select.inputValue());
    const options = await select.locator("option").allTextContents();
    check("Open on lists Overview and the extension's Today", options[0] === "Main chat" && options.includes("Overview") && options.includes("Today · Example Today"), options);
    const saved = laptop.p.waitForResponse((r) => r.url().endsWith("/api/v1/navigation") && r.request().method() === "PUT");
    await select.selectOption(TODAY);
    check("choosing Today saves it to the hub", (await saved).status() === 200 && (await hubStart(laptop.p)) === TODAY, await hubStart(laptop.p));

    const today = await openRoot(laptop.context);
    check("a new tab opens on Today", new URL(today.url()).pathname === TODAY_PATH, today.url());
    await today.locator("[data-columns]").waitFor({ timeout: 20_000 });
    check("Today's page is drawn", await today.locator("[data-columns] section").count() > 0);
    // the sidebar's Persona row still opens the main chat, and a reload stays there
    await today.locator('[data-sidebar-column] a[href="/"]').first().click();
    await today.waitForURL(`${root}/`);
    await today.reload();
    await today.waitForLoadState("networkidle");
    check("the Persona row and a reload stay on the main chat", new URL(today.url()).pathname === "/", today.url());

    // another device: its first opening is the main chat; it reads the hub's choice and opens on Today from then on
    const phone = await device({ width: 390, height: 844 });
    check("another device opens on the main chat the first time", new URL(phone.p.url()).pathname === "/", phone.p.url());
    await phone.p.waitForFunction((k) => localStorage.getItem("persona.startPage") === k, TODAY, { timeout: 20_000 });
    const phoneToday = await openRoot(phone.context);
    check("another device opens on Today from its next opening", new URL(phoneToday.url()).pathname === TODAY_PATH, phoneToday.url());
    await phone.context.close();

    // the hub no longer lists Today: the opening gives way to the main chat, the choice stays, the setting says why
    const gone = await device({ width: 1440, height: 900, hideToday: true });
    await gone.p.waitForFunction((k) => localStorage.getItem("persona.startPage") === k, TODAY, { timeout: 20_000 });
    const fallback = await gone.context.newPage();
    await fallback.goto(`${root}/`);
    // the app opens on Today from this browser's copy, then the hub's list leaves it out
    await fallback.waitForFunction((t) => (window.__replaced ?? []).includes(t) && location.pathname === "/", TODAY_PATH, { timeout: 20_000 });
    const replaced = await fallback.evaluate(() => window.__replaced ?? []);
    check("a Today the hub no longer lists opens, then gives way to the main chat", new URL(fallback.url()).pathname === "/" && replaced.indexOf(TODAY_PATH) >= 0 && replaced.lastIndexOf("/") > replaced.indexOf(TODAY_PATH), { url: fallback.url(), replaced });
    check("the choice stays", (await hubStart(fallback)) === TODAY && (await fallback.evaluate(() => localStorage.getItem("persona.startPage"))) === TODAY);
    const goneGroup = await showSetting(fallback, 1440);
    const line = await goneGroup.textContent();
    check("the setting says the page isn't available and the app opens on the main chat", line.includes("isn't available now, so Persona opens on the main chat"), line);
    await fallback.screenshot({ path: join(shots, "setting-unavailable-light-1440.png") });
    await gone.context.close();

    // screenshots: the setting and the app opened on Today, light and dark, wide and phone
    for (const theme of ["light", "dark"]) {
      for (const [width, height] of [[1440, 900], [390, 844]]) {
        const d = await device({ width, height, theme });
        await d.p.waitForFunction((k) => localStorage.getItem("persona.startPage") === k, TODAY, { timeout: 20_000 });
        const s = await showSetting(d.p, width);
        await s.locator("select").waitFor();
        // Today at the top of the sidebar too, with Sidebar pages' ↑ (this browser's order, as before this change)
        const up = d.p.getByRole("button", { name: "Move Today up" });
        while (await up.isEnabled()) await up.click();
        await s.evaluate((e) => e.scrollIntoView({ block: "center" }));
        await d.p.screenshot({ path: join(shots, `setting-${theme}-${width}.png`) });
        const t = await openRoot(d.context);
        await t.locator("[data-columns] section").nth(2).waitFor({ timeout: 20_000 });
        await t.waitForTimeout(600); // the cards' fade-up
        check(`${theme} ${width}: opened on Today`, new URL(t.url()).pathname === TODAY_PATH, t.url());
        if (width >= 640) {
          const first = await t.locator('[data-sidebar-column] nav a, [data-sidebar-column] a[href^="/extension/"], [data-sidebar-column] a[href="/overview"]').evaluateAll((as) => as.map((a) => a.getAttribute("href")).filter((h) => h && h !== "/")[0]);
          check(`${theme} ${width}: Today is the sidebar's first page`, first === TODAY_PATH, first);
        }
        await t.screenshot({ path: join(shots, `today-${theme}-${width}.png`) });
        await d.context.close();
      }
    }
    await putHubStart(laptop.p, "");
    await laptop.context.close();
  }
} catch (e) {
  failed = true;
  console.error(e);
}
clearTimeout(watchdog);
await browser.close();
writeFileSync(join(shots, oldHub ? "results-old-hub.json" : "results.json"), JSON.stringify(results, null, 2));
const ok = !failed && results.length > 0 && results.every((r) => r.ok);
console.log(`RESULT: start-page ${ok ? "passed" : "failed"} (${results.filter((r) => r.ok).length}/${results.length})`);
process.exit(ok ? 0 : 1);
