// Side sessions under their parent in the paged Agents list (#5935, docs/web/web.md "Paging hubs"): a Claude desktop
// app's side chat or /btw question (parent_agent_id) is drawn right under the session that started it, a level in, the
// group where its newest member is, and every row and count stays the hub's. Checked against the fake hub, with
// screenshots on a wide window and a phone, light and dark.
//
// Needs the web page against the fake hub with the paged list, its side sessions (as an older hub sends them, without
// side folding: MOCK_SIDE_FOLD=0; a hub that folds them puts them in their parent's chat, #7944,
// scripts/test/web_side_sessions_e2e.py) and one assistant, and Chrome:
//
//   MOCK_SESSIONS_LIST=1 MOCK_SIDE_SESSIONS=1 MOCK_SIDE_FOLD=0 MOCK_MANAGERS=0 PORT=18795 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18795 npx vite --port 5195 --strictPort &)
//   node web/e2e/side-sessions.mjs http://127.0.0.1:5195 <artifact directory> [--before] [--channel chrome]
//
// --before checks a web without the nesting (the flat list, for before/after screenshots). The fake hub pins a space by
// default, which keeps the sidebar on its complete list: the runner clears the pins and puts them back. Prints one line
// per check and a verdict; exits 1 on a failed check and 2 on its three-minute timeout.
import { mkdirSync } from "node:fs";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name, fallback) => {
  const i = args.indexOf(name);
  return i >= 0 ? args[i + 1] : fallback;
};
const base = (args[0] ?? "").replace(/\/$/, "");
const out = args[1];
const before = args.includes("--before");
const channel = flag("--channel", "chrome");
if (!base || !out || out.startsWith("--")) {
  console.error("usage: node web/e2e/side-sessions.mjs <web URL> <artifact directory> [--before] [--channel chrome]");
  process.exit(2);
}
mkdirSync(out, { recursive: true });
const watchdog = setTimeout(() => { console.log("VERDICT TIMEOUT side sessions"); process.exit(2); }, 180_000);

const failures = [];
const check = (ok, what, detail = "") => {
  console.log(`${ok ? "ok  " : "FAIL"} ${what}${detail ? `: ${detail}` : ""}`);
  if (!ok) failures.push(what);
};
const hubGet = async (path, init) => (await fetch(`${base}/api/v1${path}`, { ...init, headers: { Authorization: "Bearer mock", "Content-Type": "application/json", ...init?.headers }, signal: AbortSignal.timeout(10_000) })).json();

// What the hub lists: its total, how many need the user, and each side session's parent.
const all = await hubGet("/sessions?summary=1&limit=200");
const rows = all.sessions ?? [];
const needs = rows.filter((s) => s.state === "needs_you").length;
const sides = rows.filter((s) => s.parent_agent_id);
const parent = rows.find((s) => sides[0] && s.session_id === sides[0].parent_agent_id);
check(sides.length === 3 && Boolean(parent), "the fake hub lists three side sessions and their parent (MOCK_SIDE_SESSIONS=1)");
const pins = await hubGet("/pins");
await hubGet("/pins", { method: "PUT", body: JSON.stringify({ spaces: [], sessions: [] }) });

const browser = await chromium.launch({ channel, headless: true });
try {
  for (const { name, theme, viewport, sidebar, touch } of [
    { name: "wide-light", theme: "light", viewport: { width: 1280, height: 1200 }, sidebar: 340 },
    { name: "wide-dark", theme: "dark", viewport: { width: 1280, height: 1200 }, sidebar: 340 },
    { name: "phone-light", theme: "light", viewport: { width: 390, height: 844 }, touch: true },
    { name: "phone-dark", theme: "dark", viewport: { width: 390, height: 844 }, touch: true },
  ]) {
    const ctx = await browser.newContext({ viewport, deviceScaleFactor: 2, locale: "en-US", colorScheme: theme, hasTouch: Boolean(touch), isMobile: Boolean(touch) });
    const page = await ctx.newPage();
    const errors = [];
    page.on("pageerror", (e) => errors.push(String(e)));
    await page.goto(`${base}/?token=mock`);
    await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15_000 });
    await page.evaluate(([theme, sidebar]) => {
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.language", "en");
      if (sidebar) localStorage.setItem("persona.sidebarWidth", String(sidebar));
      for (const key of Object.keys(localStorage)) if (key.endsWith("BrowseBy") || key.endsWith("ActiveWindow") || key === "persona.agentGrouping") localStorage.removeItem(key);
    }, [theme, sidebar ?? 0]);
    await page.goto(touch ? `${base}/agents` : `${base}/`);
    const list = page.locator("[data-paged-sessions='runtime']").first();
    await list.waitFor({ timeout: 20_000 });
    const drawn = list.locator("[data-virtual-sessions] [data-session-row]");
    await drawn.filter({ hasText: sides[0].title }).first().waitFor({ timeout: 20_000 });
    // the group is in view: the rows the virtualized list draws around it
    await drawn.filter({ hasText: parent.title }).first().scrollIntoViewIfNeeded();
    await page.waitForTimeout(200);
    const order = await drawn.evaluateAll((els) => els.map((el) => ({ text: el.textContent ?? "", nested: Boolean(el.querySelector("[data-nested-session]")) })));
    const at = (title) => order.findIndex((r) => r.text.includes(title));
    const p = at(parent.title);
    if (before) {
      check(order.every((r) => !r.nested), `${name}: nothing is nested`);
    } else {
      check(p >= 0 && sides.every((s) => order[at(s.title)]?.nested), `${name}: both side sessions are drawn a level in`);
      check(sides.map((s) => at(s.title)).sort((a, b) => a - b).join() === [p + 1, p + 2].join(), `${name}: they come right under the session that started them`,
        order.map((r) => `${r.nested ? "  " : ""}${r.text.slice(0, 18)}`).join(" | "));
      check(order.filter((r) => r.nested).length === sides.length, `${name}: no other row is nested`);
      // the group sits where its newest member is: right after the rows newer than that member
      const newest = Math.max(...[parent, ...sides].map((s) => Date.parse(s.updated_at)));
      const newer = rows.filter((s) => Date.parse(s.updated_at) > newest && ![parent, ...sides].includes(s)).length;
      const position = Number(await drawn.filter({ hasText: parent.title }).first().evaluate((el) => el.closest("[data-index]")?.getAttribute("data-index")));
      check(position === newer, `${name}: the group sits where its newest member is`, `at ${position}, ${newer} newer rows`);
    }
    const total = (await list.locator("[data-session-total]").textContent()) ?? "";
    check(Number(total.replace(/\D+/g, "")) === (all.total ?? rows.length), `${name}: the total is the hub's`, total);
    const chip = await list.getByRole("radio", { name: /^Needs you/ }).getAttribute("aria-label");
    check(chip === `Needs you ${needs}`, `${name}: the Needs you count is the hub's`, chip ?? "");
    // the list is virtualized: what it holds, not what it draws in view
    const loaded = Number(await list.getAttribute("data-loaded-sessions"));
    check(loaded === rows.length, `${name}: every listed session is a row of the list`, `${loaded} of ${rows.length}`);
    // the group in the middle of the sidebar, clear of the usage panel at its foot
    await drawn.filter({ hasText: parent.title }).first().evaluate((el) => el.scrollIntoView({ block: "center" }));
    await page.waitForTimeout(300);
    await page.screenshot({ path: `${out}/${before ? "before" : "after"}-${name}.png`, ...(touch ? {} : { clip: { x: 0, y: 0, width: (sidebar ?? 340) + 40, height: viewport.height } }) });
    check(errors.length === 0, `${name}: no page errors`, errors.join("; "));
    await ctx.close();
  }
} finally {
  await browser.close();
  await hubGet("/pins", { method: "PUT", body: JSON.stringify(pins) }).catch(() => {});
  clearTimeout(watchdog);
}
console.log(`VERDICT ${failures.length ? "FAIL" : "PASS"} side sessions${before ? " (before)" : ""}${failures.length ? `: ${failures.length} failed` : ""}; screenshots in ${out}`);
process.exit(failures.length ? 1 : 0);
