// End-to-end check of the web client's parts of the main chat's availability follow-ups (docs/web/web.md "Suggested
// rules", "Decisions held for the briefing", "Model configuration"): a suggested rule's line under each kind of card
// (#5802: the open plan, a decision, a question a supervisor passed up) with what Always stores, Always and Not now
// answered on the hub, what Always made and where it is listed said under each card (and still there once the hub
// sent the card again without the suggestion), and a stale suggestion (404) that only clears the line; the mark on a
// decision held for the briefing (#5803), on its card in Needs you, on its To-dos row and on the task panel's banner;
// and each model purpose's description under its name, with the background supervisor right after Main chat (#5806).
//
// Needs the web page against the fake hub with the suggestions, and Chrome (or any Chromium through --executable):
//
//   MOCK_TASKS=1 MOCK_TODO_ASKS=1 MOCK_RULE_SUGGESTIONS=1 MOCK_ASK_HELD=1 MOCK_SUPERVISE_LISTED=1 PORT=18992 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18992 npx vite --port 5992 --strictPort &)
//   node web/e2e/rule-suggestions.mjs http://127.0.0.1:5992 --hub http://127.0.0.1:18992 [--shots <dir>] [--executable <chromium>]
//
// Runs wide (1280) in light and dark in English and once each in 简体中文 and 日本語 for the strings, then the tray at a
// phone's width (390) in the same four; --shots saves each screen. The fake hub's suggestion sentences and purpose
// descriptions are English in every language (the page's own words follow the language). The hub's suggestions are
// put back (GET /mock/reset) before each pass and at the end. Prints one line per check and a verdict; exits 1 on a
// failure and after four minutes.
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
const mockHub = (flag("--hub") ?? "").replace(/\/$/, "");
if (!base || !mockHub) {
  console.error("usage: node web/e2e/rule-suggestions.mjs <web URL> --hub <fake hub URL> [--shots <dir>] [--executable <chromium>]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("FAIL timed out after 4 minutes");
  process.exit(1);
}, 240_000).unref();

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}
const check = (cond, msg) => {
  if (!cond) fail(msg);
};

const hub = (path, init) => fetch(`${base}${path}`, { ...init, headers: { Authorization: "Bearer mock", "Content-Type": "application/json" }, signal: AbortSignal.timeout(5000) }).then((r) => r.json());
/** The fake hub's own routes (/mock/…), which the web server does not pass on. */
const mock = (path) => fetch(`${mockHub}/mock/${path}`, { signal: AbortSignal.timeout(5000) }).then((r) => r.json());
const reset = () => mock("reset");
/** Answers a suggestion on the hub as another device would; the status. */
const answerOnHub = (id, which) => fetch(`${base}/api/v1/assistant/rule-suggestions/${id}/${which}`, { method: "POST", headers: { Authorization: "Bearer mock" }, signal: AbortSignal.timeout(5000) }).then((r) => r.status);
const assistant = await hub("/api/v1/assistant");
await reset();
const HELD = "todo_review"; // the PR check's decision: held for the briefing, and it carries the standing authorization's suggestion
{
  const ask = (await hub("/api/v1/todos")).todos.find((t) => t.id === HELD)?.ask;
  if (!ask?.suggestion || ask.for_briefing !== true) fail("the fake hub has no suggested rule on a held decision (start it with MOCK_TASKS=1 MOCK_TODO_ASKS=1 MOCK_RULE_SUGGESTIONS=1 MOCK_ASK_HELD=1 MOCK_SUPERVISE_LISTED=1)");
}

const WORDS = {
  en: { open: "Open", always: "Always", notNow: "Not now", creates: "Always adds a standing authorization", trust: "Always remembers this kind of task", none: "Always stores no rule and turns nothing on", made: "Standing authorization added.", where: "Standing authorizations", madeTrust: "Remembered: tasks like this now start without asking.", whereTrust: "Remembered auto-start rules", madeNone: "No rule was stored and nothing was turned on.", hidden: "Not suggested again for 30 days.", briefing: "With the next briefing", pushed: "nothing was pushed" },
  "zh-Hans": { open: "打开", always: "总是", notNow: "暂不", creates: "新增一条长期授权", trust: "会记住这类任务", none: "不会存下规则，也不会开启任何设置", made: "已新增长期授权。", where: "长期授权", madeTrust: "已记住：这类任务以后不再询问，直接开始。", whereTrust: "记住的", madeNone: "没有存下规则，也没有开启任何设置。", hidden: "30 天内不再建议。", briefing: "随下次简报送达", pushed: "没有推送提醒" },
  ja: { open: "開く", always: "常に", notNow: "今はしない", creates: "継続的な承認を追加します", trust: "この種類のタスクを記憶します", none: "ルールは保存されず、設定もオンになりません", made: "継続的な承認を追加しました。", where: "継続的な承認", madeTrust: "記憶しました。この種類のタスクは確認なしで開始されます。", whereTrust: "", madeNone: "ルールは保存されず、設定もオンになっていません。", hidden: "30 日間は提案しません。", briefing: "次のブリーフィングで届きます", pushed: "通知は送っていません" },
};

async function open(browser, theme, lang, width = 1280, height = 900) {
  const ctx = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 2, colorScheme: theme, reducedMotion: "reduce", hasTouch: width < 600, locale: lang === "zh-Hans" ? "zh-CN" : lang });
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

/** Opens every folded row of the tray (#5569: a row with something to answer is one line until opened). */
async function openRows(page) {
  await page.locator("button[data-needs-row]").first().waitFor({ state: "visible", timeout: 15000 }).catch(() => {});
  for (let i = 0; i < 30; i++) {
    const folded = page.locator('button[data-needs-row][aria-expanded="false"]').first();
    if (!(await folded.count())) break;
    await folded.click();
  }
}

/** The main chat with the Needs you tray open and its rows opened, once every card with a suggestion drew its line. */
async function openTray(page, where) {
  await page.goto(`${base}/persona/${assistant.main}`);
  const entry = page.locator('[data-tray-entry="needs"]');
  await entry.waitFor({ state: "visible", timeout: 20000 }).catch(() => fail(`${where}: the main chat has no Needs you entry`));
  if ((await entry.getAttribute("aria-expanded")) !== "true") await entry.click();
  await openRows(page);
  await page.locator("[data-rule-suggestion]").first().waitFor({ state: "visible", timeout: 15000 }).catch(() => fail(`${where}: no card in Needs you carries a suggested rule's line`));
  await page.waitForFunction(() => document.querySelectorAll("[data-rule-suggestion]").length >= 3, null, { timeout: 10000 }).catch(() => {});
}
const line = (page, kind) => page.locator(`[data-rule-suggestion="${kind}"]`).first();
/** A picture of a line with the card it is under (its previous sibling, or what that one holds when it is `contents`),
 *  as much of both as is on screen. */
async function shotWithCard(page, name, l) {
  if (!shots) return;
  await l.scrollIntoViewIfNeeded();
  await page.waitForTimeout(350);
  const clip = await l.evaluate((el) => {
    const own = el.getBoundingClientRect();
    let card = el.previousElementSibling;
    while (card && !card.getBoundingClientRect().height) card = card.firstElementChild;
    const c = (card ?? el).getBoundingClientRect();
    const top = Math.max(0, Math.min(own.top, c.top) - 4), left = Math.max(0, Math.min(own.left, c.left) - 4);
    return { x: left, y: top, width: Math.min(innerWidth, Math.max(own.right, c.right) + 4) - left, height: Math.min(innerHeight, own.bottom + 4) - top };
  });
  await page.screenshot({ path: join(shots, `${name}.png`), clip });
}
/** Nothing of the line is wider than the screen, and its buttons are one line each. */
async function fits(page, l, where) {
  const m = await l.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const buttons = [...el.querySelectorAll("button")].map((b) => ({ h: b.getBoundingClientRect().height, wraps: getComputedStyle(b).whiteSpace !== "nowrap", right: b.getBoundingClientRect().right }));
    return { left: r.left, right: r.right, width: innerWidth, scroll: el.scrollWidth - el.clientWidth, buttons };
  });
  check(m.left >= 0 && m.right <= m.width + 0.5 && m.scroll <= 1, `${where}: the line is wider than the screen (${JSON.stringify(m)})`);
  check(m.buttons.length === 2 && m.buttons.every((b) => !b.wraps && b.h <= 34 && b.right <= m.width + 0.5), `${where}: the two buttons are not single-line inside the screen (${JSON.stringify(m.buttons)})`);
}

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
try {
  for (const [theme, lang] of [["light", "en"], ["dark", "en"], ["light", "zh-Hans"], ["light", "ja"]]) {
    const w = WORDS[lang];
    const where = `${theme} ${lang}`;
    await reset();
    const { ctx, page } = await open(browser, theme, lang);

    // 1. Needs you: a line under each kind of card, saying what Always stores
    await openTray(page, where);
    const kinds = await page.$$eval("[data-rule-suggestion]", (xs) => xs.map((x) => x.dataset.ruleSuggestion));
    for (const kind of ["plan_start", "standing_authorization", "delegate"]) check(kinds.includes(kind), `${where}: no ${kind} line in Needs you (${kinds.join(",")})`);
    for (const [kind, creates] of [["plan_start", w.trust], ["standing_authorization", w.creates], ["delegate", w.none]]) {
      const l = line(page, kind);
      const text = await l.locator("[data-rule-creates]").textContent();
      check(text.includes(creates), `${where}: the ${kind} line does not say what Always stores: ${text}`);
      const buttons = await l.locator("button").allTextContents();
      check(buttons.join("|") === `${w.always}|${w.notNow}`, `${where}: the ${kind} line's buttons are ${buttons.join("|")}`);
      await fits(page, l, `${where} ${kind}`);
      await l.scrollIntoViewIfNeeded();
      await shotWithCard(page, `suggestion-${kind}-${theme}-${lang}`, l);
    }
    const quote = await line(page, "standing_authorization").locator("[data-rule-creates]").textContent();
    check(/[“「].+→.+[”」]/.test(quote), `${where}: the standing authorization's line does not quote the owner's words: ${quote}`);
    const plain = await line(page, "delegate").locator("[data-rule-creates]").textContent();
    check(!/agents\.delegate|manager\.json/.test(plain), `${where}: the delegate line shows a setting key or a file name: ${plain}`);
    await line(page, "plan_start").scrollIntoViewIfNeeded();
    await shot(page, `needs-you-${theme}-${lang}`);
    ok(`${where}: the open plan, the decision and the passed-up question each carry a line saying what Always stores, with ${w.always} / ${w.notNow}`);

    // 2. the decision held for the briefing: its folded row says so, and its card with what that means
    {
      const head = page.locator(`[data-needs-row="task:${HELD}"] > button[aria-expanded]`);
      await head.click(); // fold it again
      const folded = page.locator(`button[data-needs-row="task:${HELD}"]`);
      await folded.waitFor({ state: "visible", timeout: 5000 }).catch(() => fail(`${where}: the held decision's row does not fold`));
      check((await folded.textContent()).includes(w.briefing), `${where}: the folded row of the held decision does not say it comes with the briefing: ${await folded.textContent()}`);
      const others = await page.$$eval("button[data-needs-row]", (xs, word) => xs.filter((x) => x.textContent.includes(word)).length, w.briefing);
      check(others === 1, `${where}: ${others} folded rows say they come with the briefing`);
      await shot(page, `held-folded-${theme}-${lang}`, folded);
      await folded.click();
    }
    const held = page.locator(`[data-needs-row="task:${HELD}"]`);
    await held.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: the held decision is not in Needs you`));
    const mark = held.locator("[data-for-briefing]");
    check((await mark.count()) === 1, `${where}: the held decision's card has ${await mark.count()} marks`);
    const said = await mark.textContent();
    check(said.includes(w.briefing) && said.includes(w.pushed), `${where}: the mark does not say it comes with the briefing and why: ${said}`);
    check((await page.locator("[data-needs-row] [data-for-briefing]").count()) === 1, `${where}: a decision that was pushed carries the mark`);
    await held.scrollIntoViewIfNeeded();
    await shot(page, `held-card-${theme}-${lang}`, held);
    ok(`${where}: the held decision's card says “${w.briefing}” with its definition; no other card does`);

    // 3. the question's line. In English, Always: nothing is stored, and that is said under the card, still there once
    // the hub sent the card again without the suggestion. In the other two, Not now: the line goes, a word says for how long.
    const question = line(page, "delegate");
    if (lang === "en") {
      await question.getByRole("button", { name: w.always, exact: true }).click();
      const result = page.locator("[data-rule-result]").first();
      await result.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: nothing said what Always did under the question`));
      const text = await result.textContent();
      check(text.includes(w.madeNone) && text.includes("agents.delegate") && !text.includes("manager.json"), `${where}: the question's result is ${text}`);
      check((await result.locator("button").count()) === 0, `${where}: the question's result offers a page to open, and this app has none for it`);
      await page.waitForTimeout(1200); // the hub sent the card again without the suggestion
      check(await result.isVisible() && (await page.locator('[data-rule-suggestion="delegate"]').count()) === 0, `${where}: the question's result went when the hub took the suggestion off the card`);
      await result.scrollIntoViewIfNeeded();
      await shotWithCard(page, `result-delegate-${theme}-${lang}`, result);
      check((await answerOnHub("rs_mock_delegate", "always")) === 404, `${where}: the hub did not get Always for the question`);
      ok(`${where}: Always on the question's line stores nothing and says so under the card, which stays`);
    } else {
      await question.getByRole("button", { name: w.notNow, exact: true }).click();
      await page.waitForFunction(() => !document.querySelector(`[data-rule-suggestion="delegate"]`), null, { timeout: 10000 }).catch(() => fail(`${where}: Not now did not clear the line`));
      await page.getByText(w.hidden).first().waitFor({ state: "visible", timeout: 5000 }).catch(() => fail(`${where}: nothing said “${w.hidden}”`));
      check((await answerOnHub("rs_mock_delegate", "not-now")) === 404, `${where}: the hub did not get Not now`);
      check((await page.locator("[data-rule-result]").count()) === 0, `${where}: Not now said a rule was made`);
      ok(`${where}: Not now clears the line, says for how long, and is saved on the hub`);
    }

    // 4. Always on the plan's line: the kind of task is remembered, and the card says so and where it is listed
    const planLine = line(page, "plan_start");
    await planLine.scrollIntoViewIfNeeded();
    await planLine.getByRole("button", { name: w.always, exact: true }).click();
    await page.waitForFunction(() => !document.querySelector(`[data-rule-suggestion="plan_start"]`), null, { timeout: 10000 }).catch(() => fail(`${where}: Always did not clear the plan's line`));
    const planResult = page.locator("[data-rule-result]", { hasText: w.madeTrust }).first();
    await planResult.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: nothing under the plan card said what Always made`));
    check((await planResult.locator("button").count()) === 1, `${where}: the plan's result has no way to the list`);
    check((await hub("/api/v1/task-trust")).kinds.some((k) => k.cwd === "/Users/me/code/weather"), `${where}: the hub did not remember the kind of task`);
    await planResult.scrollIntoViewIfNeeded();
    await shotWithCard(page, `result-plan-${theme}-${lang}`, planResult);
    ok(`${where}: Always on the plan's line remembers the kind of task and says “${w.madeTrust}” under the card`);

    // 5. Always on the decision's line: the rule is made and the line says where it is listed and removed
    const merge = line(page, "standing_authorization");
    await merge.getByRole("button", { name: w.always, exact: true }).click();
    await page.waitForFunction(() => !document.querySelector(`[data-rule-suggestion="standing_authorization"]`), null, { timeout: 10000 }).catch(() => fail(`${where}: Always did not clear the line`));
    const result = page.locator(`[data-needs-row="task:${HELD}"] [data-rule-result]`).first();
    await result.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: nothing said what Always made`));
    const text = await result.textContent();
    check(text.includes(w.made) && text.includes(w.where), `${where}: the result does not say what was made and where it is listed: ${text}`);
    check((await answerOnHub("rs_mock_merge", "always")) === 404, `${where}: the hub did not get Always for the decision`);
    // the to-dos were read again without the suggestion: what was made stays said, and the plan's too
    await page.waitForTimeout(1200);
    check(await result.isVisible() && await planResult.isVisible(), `${where}: a result went when the hub took the suggestion off the card`);
    await result.scrollIntoViewIfNeeded();
    await shotWithCard(page, `result-${theme}-${lang}`, result);
    await shot(page, `needs-you-after-${theme}-${lang}`);
    // where it is listed: Settings › Assistant profile
    await result.getByRole("button").click();
    await page.waitForURL(/\/settings\?tab=persona/, { timeout: 10000 }).catch(() => fail(`${where}: the result's button did not open Settings`));
    const listed = page.locator('[data-settings-group="standing-authorizations"]');
    await listed.waitFor({ state: "visible", timeout: 15000 }).catch(() => fail(`${where}: Settings has no standing authorizations`));
    const grants = (await hub("/api/v1/assistant/standing-authorizations")).authorizations;
    check(grants.length === 1 && grants[0].actions[0] === "merge", `${where}: the hub has ${grants.length} standing authorizations after Always`);
    await listed.getByText(grants[0].quote).first().waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: the grant Always made is not listed in Settings`));
    await page.waitForTimeout(600); // the page's lists load and draw the group again
    await listed.scrollIntoViewIfNeeded().catch(() => listed.scrollIntoViewIfNeeded());
    await shot(page, `listed-${theme}-${lang}`);
    ok(`${where}: Always makes the standing authorization, the line says “${w.made}” and where it is listed, its button opens that page, and the grant is listed there`);

    // 6. To-dos: the held decision's row carries the quiet mark, with its definition on hover; its page writes it out
    await page.goto(`${base}/todos`);
    const rowMark = page.locator('[data-testid="todo-ask"] [data-for-briefing="mark"]').first();
    await rowMark.waitFor({ state: "visible", timeout: 20000 }).catch(() => fail(`${where}: no To-dos row carries the held-for-briefing mark`));
    check((await rowMark.textContent()) === w.briefing, `${where}: the row's mark says ${await rowMark.textContent()}`);
    check((await rowMark.getAttribute("title")).includes(w.pushed), `${where}: the row's mark has no definition on hover`);
    check((await page.locator('[data-for-briefing="mark"]').count()) === 1, `${where}: more than the held decision's row carries the mark`);
    await rowMark.scrollIntoViewIfNeeded();
    await shot(page, `todos-${theme}-${lang}`);
    await rowMark.locator("xpath=ancestor::*[@data-testid='todo-ask']").locator("span").first().click({ position: { x: 4, y: 4 } }).catch(() => {});
    const caption = page.locator('[data-for-briefing="caption"]').first();
    await caption.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: the to-do's page does not write the definition out`));
    check((await caption.textContent()).includes(w.pushed), `${where}: the to-do's page says ${await caption.textContent()}`);
    await caption.scrollIntoViewIfNeeded();
    await shot(page, `todo-page-${theme}-${lang}`);
    ok(`${where}: the held decision's To-dos row carries “${w.briefing}”, defined on hover; no other row does; its page writes the definition out`);

    // 7. the task panel beside the main chat: the same mark on its ask banner
    await page.goto(`${base}/persona/${assistant.main}`);
    const entry = page.locator('[data-tray-entry="needs"]');
    await entry.waitFor({ state: "visible", timeout: 20000 });
    if ((await entry.getAttribute("aria-expanded")) !== "true") await entry.click();
    const again = page.locator(`[data-needs-row="task:${HELD}"]`);
    await again.waitFor({ state: "visible", timeout: 10000 });
    if (await again.evaluate((el) => el.matches('button[aria-expanded="false"]'))) await again.click();
    await page.locator(`[data-needs-row="task:${HELD}"]`).getByRole("button", { name: w.open, exact: true }).first().click(); // Open: the task's panel
    const banner = page.locator('[data-testid="todo-ask"] [data-for-briefing="mark"]').first();
    await banner.waitFor({ state: "visible", timeout: 15000 }).catch(() => fail(`${where}: the task panel's ask banner has no held-for-briefing mark`));
    check((await banner.textContent()) === w.briefing && (await banner.getAttribute("title")).includes(w.pushed), `${where}: the banner's mark says ${await banner.textContent()}`);
    await shot(page, `task-panel-${theme}-${lang}`);
    ok(`${where}: the task panel's ask banner carries the same mark`);

    // 8. Model settings: the purposes' descriptions under their names; the background supervisor right after Main chat
    await page.goto(`${base}/settings?tab=model-config`);
    await page.waitForSelector("[data-model-purpose]", { timeout: 15000 });
    const ids = await page.$$eval("[data-model-purpose]", (xs) => xs.map((x) => x.dataset.modelPurpose));
    check(ids[ids.indexOf("assistant") + 1] === "assistant.supervise", `${where}: Model settings lists ${ids.join(",")}: the background supervisor is not right after Main chat`);
    check(ids.filter((id) => id === "assistant.supervise").length === 1, `${where}: the background supervisor is listed ${ids.filter((id) => id === "assistant.supervise").length} times`);
    const described = Object.fromEntries(await page.$$eval("[data-model-purpose]", (xs) => xs.map((x) => [x.dataset.modelPurpose, x.querySelector('[data-testid="purpose-description"]')?.textContent ?? ""])));
    // the fake hub describes these, in English whatever the language
    check(described.assistant === "Talks with you, splits work and hands it out.", `${where}: Main chat's description is not the hub's: ${described.assistant}`);
    check(described["assistant.supervise"] === "Follows up the tasks no leader supervises, in the background.", `${where}: the background supervisor's description is not the hub's: ${described["assistant.supervise"]}`);
    for (const id of ["agent.new", "kb", "inbox.triage"]) check(described[id]?.length > 10, `${where}: ${id} has no description`);
    const under = await page.$$eval("[data-model-purpose]", (xs) => xs.every((x) => {
      const d = x.querySelector('[data-testid="purpose-description"]');
      return !d || d.parentElement.firstElementChild === d;
    }));
    check(under, `${where}: a description is not the first line under its purpose's name`);
    await shot(page, `models-${theme}-${lang}`);
    ok(`${where}: Model settings shows the hub's description under each purpose's name, the background supervisor once, right after Main chat`);
    await ctx.close();
  }

  // 9. a phone's width (390): the line wraps inside the screen and its two buttons stay whole on one line each
  for (const [theme, lang] of [["light", "en"], ["dark", "en"], ["light", "zh-Hans"], ["light", "ja"]]) {
    const where = `390 ${theme} ${lang}`;
    const w = WORDS[lang];
    await reset();
    const { ctx, page } = await open(browser, theme, lang, 390, 844);
    await openTray(page, where);
    for (const kind of ["plan_start", "standing_authorization", "delegate"]) {
      const l = line(page, kind);
      await l.scrollIntoViewIfNeeded();
      await fits(page, l, `${where} ${kind}`);
      await shot(page, `narrow-${kind}-${theme}-${lang}`);
    }
    const held = page.locator(`[data-needs-row="task:${HELD}"]`);
    await held.scrollIntoViewIfNeeded();
    const m = await held.locator("[data-for-briefing]").evaluate((el) => ({ right: el.getBoundingClientRect().right, width: innerWidth, scroll: el.scrollWidth - el.clientWidth }));
    check(m.right <= m.width + 0.5 && m.scroll <= 1, `${where}: the held decision's caption is wider than the screen (${JSON.stringify(m)})`);
    await shot(page, `narrow-held-${theme}-${lang}`);
    for (const kind of ["standing_authorization", "plan_start", "delegate"]) {
      const l = line(page, kind);
      await l.scrollIntoViewIfNeeded();
      await l.getByRole("button", { name: w.always, exact: true }).click();
      await page.waitForFunction((k) => !document.querySelector(`[data-rule-suggestion="${k}"]`), kind, { timeout: 10000 }).catch(() => fail(`${where}: Always did not clear the ${kind} line`));
      const said = { standing_authorization: w.made, plan_start: w.madeTrust, delegate: w.madeNone }[kind];
      const result = page.locator("[data-rule-result]", { hasText: said }).first();
      await result.waitFor({ state: "visible", timeout: 10000 }).catch(() => fail(`${where}: nothing said what Always made for ${kind}`));
      const r = await result.evaluate((el) => ({ right: el.getBoundingClientRect().right, width: innerWidth, scroll: el.scrollWidth - el.clientWidth, buttons: [...el.querySelectorAll("button")].map((b) => getComputedStyle(b).whiteSpace) }));
      check(r.right <= r.width + 0.5 && r.scroll <= 1 && r.buttons.every((x) => x === "nowrap"), `${where}: the ${kind} result is wider than the screen or its button wraps (${JSON.stringify(r)})`);
      await result.scrollIntoViewIfNeeded();
      await shot(page, `narrow-result-${kind}-${theme}-${lang}`);
    }
    ok(`${where}: every line, every result and the held decision's caption stay inside the screen, the buttons on one line`);
    await ctx.close();
  }

  // 10. a stale suggestion: answered on another device while this page still draws its line. Always is answered 404,
  // the line goes without a word and nothing says a rule was made.
  {
    const where = "stale";
    await reset();
    const { ctx, page } = await open(browser, "light", "en");
    await openTray(page, where);
    const l = line(page, "plan_start"); // a plan is only worded when read: the page keeps the line
    await l.scrollIntoViewIfNeeded();
    check((await answerOnHub("rs_mock_start", "not-now")) === 200, `${where}: the hub did not take the other device's answer`);
    const before = (await hub("/api/v1/task-trust")).kinds.length;
    await page.waitForTimeout(800);
    check(await l.isVisible(), `${where}: the plan's line went before it was answered here (nothing to check)`);
    await l.getByRole("button", { name: "Always", exact: true }).click();
    await page.waitForFunction(() => !document.querySelector(`[data-rule-suggestion="plan_start"]`), null, { timeout: 10000 }).catch(() => fail(`${where}: a 404 did not clear the line`));
    await page.waitForTimeout(600);
    check((await page.locator("[data-rule-result]").count()) === 0, `${where}: a stale suggestion (404) still said a rule was made`);
    check((await hub("/api/v1/task-trust")).kinds.length === before, `${where}: the hub made a rule for a stale suggestion`);
    await shot(page, "stale-light-en");
    ok(`${where}: a suggestion answered elsewhere (404) clears the line and says nothing was made`);
    await ctx.close();
  }
  console.log("PASS");
} finally {
  await reset().catch(() => {});
  await browser.close();
}
