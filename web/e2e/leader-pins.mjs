// End-to-end check of the leader conversations in the sidebar (#3623, docs/web/web.md "Persona chats"): with the hub's
// `assistant.leaders` set, a fresh browser (nothing in localStorage) shows them first among the Persona chats, in the
// hub's order, with the pin and the "Leader" label; a chat pinned in this browser comes after them; a leader's menu has
// no Pin / Unpin.
//
// Needs the web page against the fake hub with leaders, and Chrome (or any Chromium through --executable):
//
//   MOCK_LEADERS=1 PORT=18986 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18986 npx vite --port 5986 --strictPort &)
//   node web/e2e/leader-pins.mjs http://127.0.0.1:5986 [--shots <dir>] [--executable <chromium>]
//
// Runs wide (1280) and phone width (390, the drawer), light and dark, in Chinese; --shots saves each. Prints one line per
// check and a verdict; exits 1 on a failure and after three minutes.
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
const executable = flag("--executable");
if (!base) {
  console.error("usage: node web/e2e/leader-pins.mjs <web URL> [--shots <dir>] [--executable <chromium>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("FAIL timed out after 3 minutes");
  process.exit(1);
}, 180_000).unref();

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

const assistant = await (await fetch(`${base}/api/v1/assistant`, { headers: { Authorization: "Bearer mock" }, signal: AbortSignal.timeout(5000) })).json();
const leaders = assistant.leaders ?? [];
if (leaders.length < 2) fail(`the fake hub has no leaders (start it with MOCK_LEADERS=1): ${JSON.stringify(leaders)}`);
const others = assistant.conversations.filter((c) => c.session_id !== assistant.main && !leaders.includes(c.session_id));
const local = others[others.length - 1].session_id;

// the Persona chats' rows in the order shown, without New chat
const rowIds = (page) => page.$$eval('aside a[href^="/persona/"]', (as) => as.map((a) => a.getAttribute("href").slice("/persona/".length)).filter((id) => id !== "new"));

async function open(browser, theme, width, pins) {
  const ctx = await browser.newContext({ viewport: { width, height: width > 600 ? 820 : 844 }, deviceScaleFactor: 2, colorScheme: theme, locale: "zh-CN", hasTouch: width <= 600 });
  const page = await ctx.newPage();
  await page.goto(`${base}/?token=mock`);
  await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15000 });
  await page.evaluate(([t, p]) => {
    localStorage.setItem("persona.theme", t);
    localStorage.setItem("persona.language", "zh-Hans");
    if (p) localStorage.setItem("assistant.pinned", JSON.stringify(p));
    else localStorage.removeItem("assistant.pinned");
  }, [theme, pins]);
  await page.goto(`${base}/overview`);
  await page.waitForSelector("main", { timeout: 15000 });
  if (width <= 600) {
    await page.locator("main").getByRole("button", { name: "侧边栏" }).first().click();
    await page.waitForTimeout(500); // the drawer slides in
  }
  await page.locator(`aside a[href="/persona/${leaders[0]}"]`).first().waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${theme} ${width}: no row for the first leader`));
  return { ctx, page };
}

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
try {
  for (const theme of ["light", "dark"]) {
    for (const width of [1280, 390]) {
      const where = `${theme} ${width}px`;
      const { ctx, page } = await open(browser, theme, width, null);
      const ids = await rowIds(page);
      if (JSON.stringify(ids.slice(0, leaders.length)) !== JSON.stringify(leaders)) fail(`${where}: leaders not first in the hub's order: ${ids.join(",")}`);
      for (const id of leaders) {
        const row = page.locator(`aside a[href="/persona/${id}"]`).first();
        if (!(await row.textContent()).includes("主管")) fail(`${where}: leader ${id} has no 主管 label`);
      }
      if (!(await page.locator("aside").getByText("置顶", { exact: true }).count())) fail(`${where}: no 置顶 heading`);
      ok(`${where}: a fresh browser shows the ${leaders.length} leaders first, labeled 主管, under 置顶`);
      if (shots) await page.screenshot({ path: join(shots, `web-leader-pins-${width > 600 ? "wide" : "narrow"}-${theme}.png`) });
      await ctx.close();
    }
  }

  const { ctx, page } = await open(browser, "light", 1280, [local]);
  const ids = await rowIds(page);
  if (JSON.stringify(ids.slice(0, leaders.length + 1)) !== JSON.stringify([...leaders, local])) fail(`a local pin is not right after the leaders: ${ids.join(",")}`);
  if ((await page.locator(`aside a[href="/persona/${local}"]`).first().textContent()).includes("主管")) fail("the local pin is labeled 主管");
  ok("a chat pinned in this browser comes right after the leaders, without the label");

  const menuOf = async (id) => {
    await page.locator(`aside a[href="/persona/${id}"]`).first().click({ button: "right" });
    const menu = page.locator('[role="menu"]').last();
    await menu.waitFor({ state: "visible", timeout: 5000 });
    const items = await menu.locator('[role="menuitem"]').allTextContents();
    await page.keyboard.press("Escape");
    return items;
  };
  const leadMenu = await menuOf(leaders[0]);
  if (leadMenu.some((x) => x.includes("置顶"))) fail(`a leader's menu has Pin / Unpin: ${leadMenu.join(" | ")}`);
  const localMenu = await menuOf(local);
  if (!localMenu.some((x) => x.includes("取消置顶"))) fail(`the local pin's menu has no Unpin: ${localMenu.join(" | ")}`);
  ok("a leader's menu has no Pin / Unpin; the local pin's has Unpin");
  if (shots) await page.screenshot({ path: join(shots, "web-leader-pins-wide-with-local-pin.png") });
  await ctx.close();
} finally {
  await browser.close();
}
console.log("PASS web leader pins e2e");
process.exit(0);
