// End-to-end check of the background supervisor on the web (#5082, docs/web/web.md "Background supervisor"): the
// built-in conversation is listed with the scheduled tasks' chats under 全部对话 (never in the sidebar's 常驻 or 最近),
// marked built in, with a menu that only copies its id; Settings › Assistant has its off switch, which saves on the hub
// and goes back; Model settings lists its purpose after Main chat with what it is for; and the main chat draws no line
// for the reviews it made (#8237: how a task went is its row in the trays).
//
// Needs the web page against the fake hub with the background supervisor, and Chrome (or any Chromium through --executable):
//
//   MOCK_BACKGROUND_SUPERVISOR=1 MOCK_TASKS=1 MOCK_TASKS_REPORTS=1 MOCK_ORIGINS=1 MOCK_RECENT=1 MOCK_LEADERS=1 PORT=18982 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18982 npx vite --port 5982 --strictPort &)
//   node web/e2e/background-supervisor.mjs http://127.0.0.1:5982 [--shots <dir>] [--executable <chromium>] [--expect before]
//
// Runs wide (1280) in light and dark in English, and once each in 简体中文 and 日本語 for the strings; --shots saves
// each screen. --expect before only takes the screenshots (a build or a hub without the feature). Prints one line per check
// and a verdict; exits 1 on a failure and after three minutes.
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
const before = flag("--expect") === "before";
if (!base) {
  console.error("usage: node web/e2e/background-supervisor.mjs <web URL> [--shots <dir>] [--executable <chromium>] [--expect before]");
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
const check = (cond, msg) => {
  if (before) return;
  if (!cond) fail(msg);
};

const hub = (path, init) => fetch(`${base}/api/v1${path}`, { ...init, headers: { Authorization: "Bearer mock", "Content-Type": "application/json" }, signal: AbortSignal.timeout(5000) }).then((r) => r.json());
const assistant = await hub("/assistant");
const bg = assistant.background_supervisor;
if (!before && !bg) fail("the fake hub has no background supervisor (start it with MOCK_BACKGROUND_SUPERVISOR=1)");

const WORDS = {
  en: { name: "Background supervisor", builtIn: "Built-in", scheduled: "Scheduled", setting: "The main chat supervises its own tasks", hint: "Off by default", follows: "Follows ", copy: "Copy session ID", accepted: "Accepted", more: "More" },
  "zh-Hans": { name: "后台督办", builtIn: "内置", scheduled: "定时任务", setting: "主对话自己督办它派出的任务", hint: "默认关闭", follows: "跟随 ", copy: "复制会话 ID", accepted: "已验收", more: "更多" },
  ja: { name: "バックグラウンド監督", builtIn: "内蔵", scheduled: "定期タスク", setting: "メインチャットが自分のタスクを監督する", hint: "既定はオフ", follows: "に従う", copy: "セッションIDをコピー", accepted: "検収済み", more: "その他" },
};

async function open(browser, theme, lang) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, deviceScaleFactor: 2, colorScheme: theme, reducedMotion: "reduce" });
  const page = await ctx.newPage();
  await page.goto(`${base}/?token=mock`);
  await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15000 });
  await page.evaluate(([t, l]) => {
    localStorage.setItem("persona.theme", t);
    localStorage.setItem("persona.language", l);
  }, [theme, lang]);
  return { ctx, page };
}
const shot = async (page, name, target) => {
  if (!shots) return;
  await page.waitForTimeout(350); // fonts and the row highlight settle
  if (target) await target.screenshot({ path: join(shots, `${name}.png`) });
  else await page.screenshot({ path: join(shots, `${name}.png`) });
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
try {
  for (const [theme, lang] of [["light", "en"], ["dark", "en"], ["light", "zh-Hans"], ["light", "ja"]]) {
    const w = WORDS[lang];
    const where = `${theme} ${lang}`;
    const { ctx, page } = await open(browser, theme, lang);

    // 1. the sidebar and 全部对话 › 定时任务
    await page.goto(`${base}/history/task`);
    await page.waitForSelector("main a[href^='/persona/']", { timeout: 15000 });
    const side = await page.$$eval('aside a[href^="/persona/"]', (as) => as.map((a) => a.getAttribute("href").slice("/persona/".length).split("?")[0]));
    check(!side.includes(bg), `${where}: the sidebar lists the background supervisor among 常驻 / 最近: ${side.join(",")}`);
    const row = page.locator(`main a[href^="/persona/${bg}"]`).first();
    if (!before) await row.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: 全部对话 › ${w.scheduled} has no row for the background supervisor`));
    await shot(page, `history-${theme}-${lang}`);
    if (!before) {
      check((await row.locator('[data-testid="built-in"]').textContent()) === w.builtIn, `${where}: its row is not marked ${w.builtIn}`);
      await row.locator("..").getByRole("button", { name: w.more }).click();
      const items = await page.$$eval('[role="menuitem"]', (xs) => xs.map((x) => x.textContent.trim()));
      check(items.length === 1 && items[0].startsWith(w.copy), `${where}: its menu is ${items.join(" | ")}, not only ${w.copy}`);
      await shot(page, `history-menu-${theme}-${lang}`);
      await page.keyboard.press("Escape");
      ok(`${where}: listed under 全部对话 › ${w.scheduled}, marked ${w.builtIn}, not in the sidebar's chats; its menu only copies its id`);
    }

    // opening it: its conversation, as a scheduled task's
    if (!before) {
      await row.click();
      await page.waitForURL(new RegExp(`/persona/${bg}`), { timeout: 10000 });
      await page.waitForSelector("main header", { timeout: 10000 });
      await shot(page, `conversation-${theme}-${lang}`);
      ok(`${where}: its row opens its conversation`);
    }

    // 2. Settings › Assistant: the switch
    await page.goto(`${base}/settings?tab=persona`);
    await page.waitForSelector("main", { timeout: 15000 });
    const sw = page.getByRole("switch", { name: w.setting });
    if (!before) {
      await sw.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: Settings › Assistant has no switch “${w.setting}”`));
      await sw.scrollIntoViewIfNeeded();
      check((await sw.getAttribute("aria-checked")) === "false", `${where}: the switch is not off by default`);
      check((await page.locator("main").textContent()).includes(w.hint), `${where}: no sentence under the switch (${w.hint}…)`);
      await shot(page, `setting-off-${theme}-${lang}`);
      await sw.click();
      await page.waitForFunction((name) => document.querySelector(`[role="switch"][aria-label="${name}"]`)?.getAttribute("aria-checked") === "true", w.setting, { timeout: 5000 }).catch(() => fail(`${where}: the switch did not turn on`));
      check((await hub("/assistant")).main_supervises_own_tasks === true, `${where}: the hub did not save the switch`);
      await shot(page, `setting-on-${theme}-${lang}`);
      await sw.click();
      await page.waitForFunction((name) => document.querySelector(`[role="switch"][aria-label="${name}"]`)?.getAttribute("aria-checked") === "false", w.setting, { timeout: 5000 }).catch(() => fail(`${where}: the switch did not turn back off`));
      check((await hub("/assistant")).main_supervises_own_tasks === false, `${where}: the hub did not take the switch back`);
      ok(`${where}: Settings › Assistant has the switch, off by default with its sentence; it saves on the hub and goes back`);
    } else {
      await page.getByText(/Agent follow-up|Agent 跟进|エージェントのフォロー/).first().scrollIntoViewIfNeeded().catch(() => {});
      await shot(page, `setting-off-${theme}-${lang}`);
    }

    // 3. Model settings: the purpose after Main chat
    await page.goto(`${base}/settings?tab=model-config`);
    await page.waitForSelector("[data-model-purpose]", { timeout: 15000 });
    const ids = await page.$$eval("[data-model-purpose]", (xs) => xs.map((x) => x.dataset.modelPurpose));
    if (!before) {
      check(ids[ids.indexOf("assistant") + 1] === "assistant.supervise", `${where}: Model settings lists ${ids.join(",")}: no background supervisor after Main chat`);
      const purpose = page.locator('[data-model-purpose="assistant.supervise"]');
      const text = await purpose.textContent();
      check(text.includes(w.follows), `${where}: the purpose does not say what it follows: ${text}`);
      check((await purpose.locator('[data-testid="purpose-description"]').count()) === 1, `${where}: the purpose has no description`);
      const sameGroup = await page.evaluate(() => document.querySelector('[data-model-purpose="assistant.supervise"]').parentElement === document.querySelector('[data-model-purpose="assistant"]').parentElement);
      check(sameGroup, `${where}: the purpose is not in Main chat's group`);
      await purpose.scrollIntoViewIfNeeded();
      ok(`${where}: Model settings lists the background supervisor after Main chat, in its group, following it, with what it is for`);
    }
    await shot(page, `model-${theme}-${lang}`);
    if (!before) {
      await page.locator('[data-model-purpose="assistant.supervise"] button').first().click();
      await page.getByRole("dialog").waitFor({ state: "visible", timeout: 10000 });
      await page.waitForTimeout(600); // the model list loads
      await shot(page, `model-editor-${theme}-${lang}`);
      await page.keyboard.press("Escape");
    }

    // 4. the main chat: no line for the reviews the background supervisor made (#8237: the task's row says how it went)
    await page.goto(`${base}/persona/${assistant.main}`);
    await page.waitForSelector("main", { timeout: 15000 });
    await page.waitForTimeout(1500);
    if (!before) {
      check((await page.locator('[data-testid="review-line"]').count()) === 0, `${where}: the main chat draws a line for a review`);
      ok(`${where}: the main chat draws no review line`);
    }
    await shot(page, `feed-${theme}-${lang}`);
    await ctx.close();
  }
  console.log(before ? "DONE screenshots only (--expect before)" : "PASS background supervisor on the web");
} finally {
  await browser.close();
}
