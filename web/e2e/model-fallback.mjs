// End-to-end check of the refusal fallback (#2528, docs/protocol/protocol.md "Going back to the configured model"): after an
// upstream refusal moved a session to a fallback model that then wasn't there, the transcript shows the refusal as a line of
// its own (both models, category, explanation, the refusal's words) and a card above the composer names both models and
// the error since. A terminal agent's card only says to type /model there, and its API error offers no "Continue". The
// main conversation's card offers "Back to configured model" and "New main chat"; going back answers with a card that
// only says the next start will use the configured model, and offers nothing more. The main conversation is for
// everyday people (#5202): the line and the card each say one sentence with the model's short name, the record under a
// closed Details, which opens to all of it.
//
// Needs the web page against the fake hub with the two refused sessions, and Chrome or Edge (playwright-core drives it):
//
//   MOCK_MODEL_FALLBACK=1 MOCK_MAIN_ACTIONS=1 PORT=18986 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18986 npx vite --port 5986 --strictPort &)
//   node web/e2e/model-fallback.mjs http://127.0.0.1:5986 --hub http://127.0.0.1:18986 [--shots <dir>] [--channel msedge]
//
// Runs wide (1280) and phone width (390), light and dark, in Chinese; before each, the fake hub forgets the last way back
// (GET /mock/model-fallback/reset). Prints one line per check and a verdict; exits 1 on a failure.
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
const channel = flag("--channel") ?? "chrome";
if (!base || !hub) {
  console.error("usage: node web/e2e/model-fallback.mjs <web URL> --hub <fake hub URL> [--shots <dir>] [--channel msedge]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

// the fake hub's refused sessions (scripts/mock/mock_hub.py, MOCK_MODEL_FALLBACK=1)
const REVIEW = ["macbook", "a1f3c2d4-0000-4000-8000-000000000006"]; // a terminal Claude agent
const MAIN = "b2e4d6f8-0000-4000-8000-000000000001"; // the main conversation, headless

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

async function open(browser, theme, width) {
  const ctx = await browser.newContext({ viewport: { width, height: width > 600 ? 800 : 844 }, deviceScaleFactor: 2, colorScheme: theme, locale: "zh-CN", hasTouch: width <= 600 });
  const page = await ctx.newPage();
  await page.goto(`${base}/?token=mock`);
  await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 60000 }); // a cold dev server compiles first
  await page.evaluate((t) => {
    localStorage.setItem("persona.theme", t);
    localStorage.setItem("persona.language", "zh-Hans");
  }, theme);
  await page.reload();
  await page.waitForSelector("main", { timeout: 60000 });
  return { ctx, page };
}

const save = async (page, name) => {
  if (!shots) return;
  await page.waitForTimeout(300);
  await page.screenshot({ path: join(shots, `web-model-fallback-${name}.png`) });
};

/** The notice line in view, then what the page shows from it down to the composer. */
async function showNotice(page) {
  const notice = page.getByTestId("refusal-notice").last();
  await notice.waitFor({ state: "visible", timeout: 15000 });
  await notice.scrollIntoViewIfNeeded();
  return notice;
}

async function checkNotice(page, tag) {
  const notice = await showNotice(page);
  const text = await notice.innerText();
  for (const want of ["claude-opus-5-5", "claude-sonnet-5-5", "cyber", "The upstream provider declined this request."]) {
    if (!text.includes(want)) fail(`${tag}: the refusal line lacks ${want}`);
  }
  const words = await notice.getByTestId("refusal-text").innerText();
  if (!words.startsWith("Claude Code is unable to respond to this request")) fail(`${tag}: the refusal's words are not shown verbatim`);
  if (text.includes("{") || text.includes("model_refusal_fallback")) fail(`${tag}: the line shows the record itself`);
}

/** A Persona conversation's line (#5202): one sentence, the record only once Details is open. */
async function checkPlainNotice(page, tag) {
  const notice = await showNotice(page);
  const text = await notice.innerText();
  if (!text.includes("上游拒绝了这次请求，已改用 Sonnet 5.5。")) fail(`${tag}: the refusal line lacks its plain sentence`);
  for (const hidden of ["claude-sonnet-5-5", "cyber", "拒绝原文"]) {
    if (text.includes(hidden)) fail(`${tag}: the closed line shows ${hidden}`);
  }
  await notice.getByTestId("refusal-details").locator("summary").click();
  await checkNotice(page, tag);
}

const browser = await chromium.launch({ channel, headless: true });
try {
  for (const width of [1280, 390]) {
    for (const theme of ["light", "dark"]) {
      const tag = `${width}-${theme}`;
      const reset = await fetch(`${hub}/mock/model-fallback/reset`, { signal: AbortSignal.timeout(5000) });
      if (!reset.ok) fail(`${tag}: the fake hub has no refused sessions (MOCK_MODEL_FALLBACK=1)`);
      const { ctx, page } = await open(browser, theme, width);

      // a terminal agent: the line, the card with the manual step only, no "Continue"
      await page.goto(`${base}/s/${REVIEW[0]}/${REVIEW[1]}`);
      await page.waitForSelector("header h1", { timeout: 15000 });
      await checkNotice(page, `${tag} agent`);
      const card = page.getByTestId("model-fallback");
      await card.waitFor({ state: "visible", timeout: 10000 });
      const cardText = await card.innerText();
      if (!cardText.includes("claude-opus-5-5") || !cardText.includes("claude-sonnet-5-5")) fail(`${tag} agent: the card doesn't name both models`);
      if (!cardText.includes("/model")) fail(`${tag} agent: the card lacks the manual /model step`);
      if ((await card.getByRole("button").count()) !== 0) fail(`${tag} agent: a terminal session's card offers a button`);
      if ((await page.getByRole("button", { name: /^继续$/ }).count()) !== 0) fail(`${tag} agent: the missing fallback model offers "Continue"`);
      await save(page, `agent-${tag}`);
      ok(`${tag}: the agent's refusal line and its terminal card`);

      // the main conversation: both ways out, then going back
      await page.goto(`${base}/persona/${MAIN}`, { waitUntil: "domcontentloaded", timeout: 30000 });
      const main = page.getByTestId("model-fallback");
      await main.waitFor({ state: "visible", timeout: 15000 });
      const plainCard = await main.innerText();
      if (!plainCard.includes("这个对话现在用 Sonnet 5.5 回复")) fail(`${tag} main: the card lacks its plain sentence`);
      if (plainCard.includes("claude-sonnet-5-5") || plainCard.includes("API Error")) fail(`${tag} main: the closed card shows the record`);
      await save(page, `main-${tag}`);
      await checkPlainNotice(page, `${tag} main`);
      await main.getByTestId("model-fallback-details").locator("summary").click();
      if (!(await main.innerText()).includes("回退模型：claude-sonnet-5-5")) fail(`${tag} main: Details doesn't open to the record`);
      await save(page, `main-details-${tag}`);
      const back = main.getByRole("button", { name: "回到配置的模型" });
      if ((await back.count()) !== 1) fail(`${tag} main: no "Back to configured model"`);
      if ((await main.getByRole("button", { name: "新主对话" }).count()) !== 1) fail(`${tag} main: no "New main chat"`);
      await back.click();
      await main.getByText("下次启动将使用配置的模型").first().waitFor({ state: "visible", timeout: 10000 });
      const pending = await main.innerText();
      if (!pending.includes("配置的模型：Opus 5.5")) fail(`${tag} main: the pending card doesn't name the configured model`);
      if ((await back.count()) !== 0) fail(`${tag} main: going back is still offered`);
      if (/已恢复|已切换回/.test(pending)) fail(`${tag} main: the pending card claims the model was restored`);
      await save(page, `main-pending-${tag}`);
      ok(`${tag}: the main conversation's way back, then a card that claims nothing`);
      await ctx.close();
    }
  }
  console.log("VERDICT pass");
} finally {
  await browser.close();
}
