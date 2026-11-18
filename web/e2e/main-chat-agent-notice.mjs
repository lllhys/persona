// Real Chromium against the synthetic mock hub: the current main chat runs Codex while Main chat defaults to Claude.
// The notice and pill name what runs, the existing New main dialog opens, and Models previews the same explanation.
// Same-kind and unknown defaults are negative controls, delivered as live session revisions without reloading.
// Light/dark, desktop standard text and phone large text.
//
//   MOCK_MAIN_ACTIONS=1 MOCK_MODEL_PICK=otherkind PORT=18874 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18874 npx vite --port 5374 --strictPort &)
//   PERSONA_E2E_CHROMIUM=<chrome> node web/e2e/main-chat-agent-notice.mjs http://127.0.0.1:5374 \
//     --hub http://127.0.0.1:18874 --shots <dir>
import assert from "node:assert/strict";
import { mkdirSync, readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name) => {
  const at = args.indexOf(name);
  return at >= 0 ? args[at + 1] : undefined;
};
const base = (args[0] ?? "").replace(/\/$/, "");
const hub = (flag("--hub") ?? "").replace(/\/$/, "");
const shots = resolve(flag("--shots") ?? "/tmp/persona-main-chat-agent-notice");
const executablePath = flag("--chromium") || process.env.PERSONA_E2E_CHROMIUM || process.env.CHROME_BIN;
if (!base || !hub) {
  console.error("usage: node web/e2e/main-chat-agent-notice.mjs <web URL> --hub <mock URL> [--shots <dir>] [--chromium <executable>]");
  process.exit(2);
}
for (const value of [base, hub]) {
  const url = new URL(value);
  assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(url.hostname), "Use only loopback web and mock-hub URLs");
  assert.equal(url.protocol, "http:");
}
mkdirSync(shots, { recursive: true });
const locale = (name) => JSON.parse(readFileSync(new URL(`../src/i18n/locales/en/${name}.json`, import.meta.url), "utf8"));
const noticeWords = locale("assistant-mainagentnotice");
const chatWords = locale("assistant-assistantchat");
const PAGE = "/persona/b2e4d6f8-0000-4000-8000-000000000001";
const watchdog = setTimeout(() => { console.error("TIMEOUT main-chat agent notice (240s)"); process.exit(124); }, 240_000);
let browser;
let screenshots = 0;

async function mock(path) {
  const response = await fetch(`${hub}/mock/${path}`, { signal: AbortSignal.timeout(5000) });
  assert.equal(response.status, 200);
  return response.json();
}

async function openMain(page) {
  const launchers = page.waitForResponse((r) => r.url().includes("/launchers?") && r.url().includes("purpose=assistant") && r.status() === 200);
  await page.goto(`${base}${PAGE}?token=mock`);
  await page.locator("[data-model-pill]").first().waitFor();
  await launchers;
  await page.evaluate(() => document.fonts.ready);
}

async function shot(page, name) {
  assert.equal(await page.locator("vite-error-overlay").count(), 0);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, `${name}: no horizontal overflow`);
  await page.screenshot({ path: join(shots, `${name}.png`) });
  screenshots++;
}

async function run(theme, large) {
  await mock("model_pick?reset=1");
  const tag = `${theme}-${large ? "phone-large" : "desktop-standard"}`;
  const context = await browser.newContext({
    viewport: large ? { width: 390, height: 844 } : { width: 1280, height: 900 },
    deviceScaleFactor: 2,
    colorScheme: theme,
    reducedMotion: "reduce",
    locale: "en-US",
    hasTouch: large,
    isMobile: large,
  });
  await context.addInitScript(({ theme, large }) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "en");
    localStorage.setItem("persona.text-size", large ? "large" : "standard");
  }, { theme, large });
  const page = await context.newPage();
  page.setDefaultTimeout(20_000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(String(error)));

  await openMain(page);
  const notice = page.locator("[data-main-chat-agent-notice]");
  await notice.waitFor();
  const text = await notice.innerText();
  assert.ok(text.includes("codex") && text.includes("claude"), `${tag}: both actual and configured launchers`);
  assert.ok(text.includes(noticeWords.main_other_kind.split("{actual}")[0]), `${tag}: localized explanation`);
  const pill = page.locator("[data-model-pill]").first();
  const pillLabel = ((await pill.getAttribute("aria-label")) || "").toLowerCase();
  assert.equal(await pill.locator("[data-pill-engine]").getAttribute("data-pill-engine"), "codex", `${tag}: actual Codex engine`);
  assert.ok(pillLabel.includes("5.5"), `${tag}: actual session model: ${pillLabel}`);
  assert.ok(!pillLabel.includes("opus"), `${tag}: the configured Claude model is not presented as running`);
  const action = notice.getByRole("button", { name: chatWords.new_main_ellipsis, exact: true });
  assert.equal(await action.evaluate((el) => getComputedStyle(el).whiteSpace), "nowrap");
  assert.ok(await action.evaluate((el) => el.scrollWidth <= el.clientWidth + 1), `${tag}: New main button is not clipped`);
  if (large) {
    const hitHeight = await action.evaluate((el) => {
      const before = getComputedStyle(el, "::before");
      return el.getBoundingClientRect().height - (parseFloat(before.top) || 0) - (parseFloat(before.bottom) || 0);
    });
    assert.ok(hitHeight >= 44, `${tag}: touch target is ${hitHeight}px, want at least 44px`);
  }
  await shot(page, `main-chat-agent-notice-${tag}`);

  await action.click();
  const newMain = page.getByRole("dialog", { name: chatWords.new_main_title, exact: true });
  await newMain.waitFor();
  await shot(page, `new-main-dialog-${tag}`);
  await newMain.getByRole("button", { name: "Cancel", exact: true }).click();
  await newMain.waitFor({ state: "detached" });
  // A phone sheet closes its history entry with an asynchronous history.back(); do not race the next navigation.
  await page.waitForFunction(() => !history.state?.personaOverlay);

  await page.goto(`${base}/settings?tab=model-config`);
  const purpose = page.locator('[data-model-purpose="assistant"]');
  await purpose.waitFor();
  await purpose.getByRole("button").first().click();
  const editor = page.locator('[role="dialog"]');
  const settingNotice = editor.locator("[data-main-chat-agent-setting]");
  await settingNotice.waitFor();
  assert.ok((await settingNotice.innerText()).includes("codex") && (await settingNotice.innerText()).includes("claude"));
  await shot(page, `model-setting-agent-notice-${tag}`);
  const provider = editor.locator("select").first();
  await provider.selectOption("launcher.codex");
  await settingNotice.waitFor({ state: "detached" });
  await provider.selectOption("launcher.claude");
  await settingNotice.waitFor();
  await editor.getByRole("button", { name: "Cancel", exact: true }).click();
  await editor.waitFor({ state: "detached" });
  await page.waitForFunction(() => !history.state?.personaOverlay);

  // The default changes elsewhere while this page stays open. A pushed session revision must refresh the cached
  // launchers: a same-kind default removes the notice, an unknown one stays silent, and Claude restores the mismatch.
  await openMain(page);
  await notice.waitFor();
  const sameKindRead = page.waitForResponse((r) => r.url().includes("/launchers?") && r.url().includes("purpose=assistant") && r.status() === 200);
  await mock("model_pick?default=codex");
  await sameKindRead;
  await notice.waitFor({ state: "detached" });
  assert.equal(await notice.count(), 0, `${tag}: same-kind default removes the stale notice without a reload`);
  const unknownRead = page.waitForResponse((r) => r.url().includes("/launchers?") && r.url().includes("purpose=assistant") && r.status() === 200);
  await mock("model_pick?default=gone");
  await unknownRead;
  await page.waitForTimeout(400);
  assert.equal(await notice.count(), 0, `${tag}: unknown default stays silent`);
  const restoredRead = page.waitForResponse((r) => r.url().includes("/launchers?") && r.url().includes("purpose=assistant") && r.status() === 200);
  await mock("model_pick?default=claude");
  await restoredRead;
  await notice.waitFor();
  assert.deepEqual(errors, [], `${tag}: no browser exceptions`);
  console.log(`PASS ${tag}: actual pill, notice, New main dialog, Models preview, live same-kind/unknown suppression and restoration`);
  await context.close();
}

try {
  browser = await chromium.launch({
    ...(executablePath ? { executablePath } : { channel: "chrome" }),
    headless: true,
    args: ["--no-sandbox", "--disable-dev-shm-usage"],
  });
  for (const large of [false, true]) for (const theme of ["light", "dark"]) await run(theme, large);
  console.log(`PASS main-chat agent notice: 4 real-browser scenarios, ${screenshots} screenshots, synthetic mock only`);
} catch (error) {
  console.error("FAIL main-chat agent notice", error);
  process.exitCode = 1;
} finally {
  clearTimeout(watchdog);
  await browser?.close();
}
