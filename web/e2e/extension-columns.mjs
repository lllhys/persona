// UI version 8's columns layout (#9158) in a real browser against the mock hub: a made-up extension's "Today" page, a
// grid of cards that jsdom cannot lay out. Synthetic data only; it writes nothing to the hub.
//
// Start the mock with MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1 and Vite (or `vite preview`) pointing at it
// (PERSONA_HUB_URL).
// node web/e2e/extension-columns.mjs <loopback web URL> <artifact directory> [Chromium executable]
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [base, shots, executable] = process.argv.slice(2);
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/extension-columns.mjs <loopback web URL> <artifact directory> [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: extension-columns timed out (180s)");
  process.exit(124);
}, 180_000);
const root = base.replace(/\/$/, "");
const PAGE = "/extension/example-today/today";
const results = [];
const check = (name, ok, detail) => {
  results.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${detail === undefined ? "" : ` ${JSON.stringify(detail)}`}`);
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
async function page({ width, height, theme, textSize }) {
  const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme, deviceScaleFactor: 2, serviceWorkers: "block" });
  await context.addInitScript(([theme, textSize]) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "en");
    if (textSize) localStorage.setItem("persona.text-size", textSize);
  }, [theme, textSize]);
  // nothing leaves the loopback origin
  await context.route("**/*", (route) => (new URL(route.request().url()).origin === new URL(root).origin ? route.continue() : route.abort()));
  const p = await context.newPage();
  await p.goto(`${root}/?token=mock`);
  await p.waitForFunction(() => localStorage.getItem("persona.token"));
  await p.goto(root + PAGE);
  await p.locator("[data-columns] section").nth(4).waitFor({ timeout: 20_000 });
  await p.waitForTimeout(600); // the cards' fade-up
  return p;
}

// Each card's box and title, and the grid's columns as the browser laid them out.
const measure = (p) =>
  p.evaluate(() => {
    const grid = document.querySelector("[data-columns]");
    const box = (e) => {
      const r = e.getBoundingClientRect();
      return { x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width), h: Math.round(r.height) };
    };
    const cards = [...grid.children].map((cell) => {
      const section = cell.querySelector("section");
      // the card is the grid's cell; its section is as tall as its own content, without the stretch of its row
      return { title: section.querySelector("h3")?.textContent ?? "", full: cell.dataset.span === "full", card: box(cell), content: box(section).h };
    });
    return { columns: getComputedStyle(grid).gridTemplateColumns.split(" ").length, grid: box(grid), cards };
  });

// The page at this width, as long as its content (the page scrolls inside the app, so a full-page shot would stop at
// the window's height).
async function fullShot(p, width, file) {
  const height = await p.evaluate(() => {
    const scroller = document.querySelector("[data-columns]").closest(".overflow-y-auto");
    return Math.ceil(scroller.getBoundingClientRect().top + scroller.scrollHeight);
  });
  await p.setViewportSize({ width, height });
  await p.waitForTimeout(300);
  await p.screenshot({ path: file });
}

// A card's rows (each row of the mock has an action, so it is a button), not the link beside its title.
const rowsOf = (p, title) =>
  p.locator("[data-columns] section").filter({ has: p.locator("h3", { hasText: new RegExp(`^${title}$`) }) }).locator("button:not(h3 + button)").evaluateAll((els) =>
    els.map((e) => {
      const r = e.getBoundingClientRect();
      return { text: e.innerText.split("\n")[0], x: Math.round(r.x), y: Math.round(r.y), w: Math.round(r.width) };
    }),
  );

let failed = false;
try {
  for (const theme of ["light", "dark"]) {
    // 1440 × 900: two columns, the Needs me card across both with its rows two a line, rows of cards aligned
    {
      const p = await page({ width: 1440, height: 900, theme });
      const m = await measure(p);
      const titles = m.cards.map((c) => c.title);
      check(`1440 ${theme}: five cards in reading order`, titles.join("|") === "Needs me|Calendar|Lark today|WeChat|Running Agents", titles);
      check(`1440 ${theme}: two columns`, m.columns === 2, m.columns);
      const [needs, cal, lark, wechat, agents] = m.cards;
      check(`1440 ${theme}: Needs me spans the row`, needs.full && Math.abs(needs.card.w - m.grid.w) <= 1, { card: needs.card.w, grid: m.grid.w });
      check(`1440 ${theme}: Calendar | Lark today side by side`, cal.card.y === lark.card.y && cal.card.x < lark.card.x && cal.card.y > needs.card.y, { cal: cal.card, lark: lark.card });
      check(`1440 ${theme}: WeChat | Running Agents under them`, wechat.card.y === agents.card.y && wechat.card.x === cal.card.x && agents.card.x === lark.card.x && wechat.card.y > cal.card.y, { wechat: wechat.card, agents: agents.card });
      check(`1440 ${theme}: each row of cards is as tall as its tallest`, cal.card.h === lark.card.h && wechat.card.h === agents.card.h, { cal: cal.card.h, lark: lark.card.h, wechat: wechat.card.h, agents: agents.card.h });
      // the shorter lists' cards are stretched to their row, not as tall by chance
      check(`1440 ${theme}: Lark today and WeChat are stretched to their rows`, lark.content < lark.card.h - 40 && wechat.content < wechat.card.h - 40 && cal.content > lark.content, { lark: [lark.content, lark.card.h], wechat: [wechat.content, wechat.card.h], cal: cal.content });
      const needRows = await rowsOf(p, "Needs me");
      const lines = new Set(needRows.map((r) => r.y));
      check(`1440 ${theme}: Needs me's four rows two a line`, needRows.length === 4 && lines.size === 2 && needRows[0].x < needRows[1].x, needRows);
      const head = await p.locator("[data-columns] section h3", { hasText: "Calendar" }).first().evaluate((h) => {
        const row = h.parentElement;
        return { icon: !!row.querySelector("svg"), link: row.querySelector("button")?.innerText ?? "" };
      });
      check(`1440 ${theme}: a card's head has its icon and its link`, head.icon && head.link === "Calendar", head);
      await p.screenshot({ path: join(shots, `today-1440-${theme}.png`) });
      if (theme === "light") {
        // the link beside the title pushes the card's page; Back comes back to the grid
        await p.locator("[data-columns] section").filter({ has: p.locator("h3", { hasText: /^Calendar$/ }) }).locator("h3 + button").click();
        const pushed = await p.getByText("Everything of this card would be listed here.").waitFor({ timeout: 10_000 }).then(() => true, () => false);
        check("1440 light: the Calendar › link opens the card's page", pushed);
        await p.goBack();
        const back = await p.locator("[data-columns]").waitFor({ timeout: 10_000 }).then(() => true, () => false);
        check("1440 light: Back returns to the grid", back);
        await p.locator("[data-columns] section").nth(4).waitFor({ timeout: 10_000 });
        await p.waitForTimeout(600);
      }
      await fullShot(p, 1440, join(shots, `today-1440-full-${theme}.png`));
      await p.context().close();
    }
    // 390 × 844: one column, each card the page's width, rows one a line, in the same order
    {
      const p = await page({ width: 390, height: 844, theme });
      const m = await measure(p);
      check(`390 ${theme}: one column`, m.columns === 1, m.columns);
      const xs = new Set(m.cards.map((c) => c.card.x));
      const ws = new Set(m.cards.map((c) => c.card.w));
      check(`390 ${theme}: every card the grid's width, one under another`, xs.size === 1 && ws.size === 1 && [...ws][0] === m.grid.w && m.cards.every((c, i) => i === 0 || c.card.y > m.cards[i - 1].card.y), { xs: [...xs], ws: [...ws], grid: m.grid.w });
      const needRows = await rowsOf(p, "Needs me");
      check(`390 ${theme}: Needs me's rows one a line`, new Set(needRows.map((r) => r.y)).size === needRows.length && needRows.length === 4, needRows);
      await p.screenshot({ path: join(shots, `today-390-${theme}.png`) });
      await fullShot(p, 390, join(shots, `today-390-full-${theme}.png`));
      await p.context().close();
    }
  }
  // Settings › Appearance's Large text: the same columns, and nothing wider than its card
  for (const [width, height, columns] of [[1440, 900, 2], [390, 844, 1]]) {
    const p = await page({ width, height, theme: "light", textSize: "large" });
    const m = await measure(p);
    const overflow = await p.evaluate(() => [...document.querySelectorAll("[data-columns] > div")].filter((c) => c.scrollWidth > c.clientWidth).length);
    check(`${width} large text: ${columns} column(s), no card overflows`, m.columns === columns && overflow === 0, { columns: m.columns, overflow });
    await p.screenshot({ path: join(shots, `today-${width}-large-light.png`) });
    await p.context().close();
  }
} catch (e) {
  failed = true;
  console.error(e);
} finally {
  await browser.close();
  clearTimeout(watchdog);
}
writeFileSync(join(shots, "results.json"), JSON.stringify(results, null, 2));
const bad = results.filter((r) => !r.ok);
console.log(`RESULT: ${failed || bad.length ? "FAIL" : "PASS"} (${results.length - bad.length}/${results.length} checks)`);
process.exit(failed || bad.length ? 1 : 0);
