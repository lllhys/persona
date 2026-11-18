// Search in chat against the mock hub (#6281, docs/design/proposals/assistant-search.md): what the chat shows is found,
// a hidden tag never is, folded work is found without opening it and going to it opens it. Synthetic data only.
// Start the mock with MOCK_CHAT_SEARCH=1 and Vite pointing at it (PERSONA_HUB_URL), then:
// node web/e2e/chat-search.mjs <loopback web URL> <screenshot directory> [Chromium executable]
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [base, shots, executable] = process.argv.slice(2);
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/chat-search.mjs <loopback web URL> <screenshot directory> [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: chat-search timed out (240s)");
  process.exit(124);
}, 240_000);
const root = base.replace(/\/$/, "");
const CHAT = "/persona/b2e4d6f8-0000-4000-8000-000000000004";

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
let failed = false;
try {
  for (const scheme of ["light", "dark"]) {
    const page = await (await browser.newContext({ viewport: { width: 1280, height: 860 }, colorScheme: scheme, locale: "en-US", deviceScaleFactor: 2 })).newPage();
    await page.goto(`${root}/overview?token=mock`);
    await page.waitForFunction(() => localStorage.getItem("persona.token"));
    await page.goto(root + CHAT);
    await page.getByText("LastReplyWord").first().waitFor({ timeout: 20_000 });
    await page.waitForTimeout(800);

    const box = () => page.getByPlaceholder("Search what's loaded");
    const open = async () => {
      if (await box().isVisible().catch(() => false)) return;
      await page.getByRole("button", { name: "More actions" }).first().click();
      await page.getByText("Search in chat", { exact: true }).last().click();
      await box().waitFor();
    };
    const hits = async (word) => {
      await open();
      await box().fill(word);
      await page.waitForTimeout(450);
      return page.locator("[data-search-result]").evaluateAll((els) => els.map((e) => [e.getAttribute("data-search-result"), e.textContent ?? ""]));
    };
    const settledFolds = () => page.getByText("Searching folded steps").waitFor({ state: "hidden", timeout: 15_000 });

    // what the chat shows is found, each in its group
    await open();
    await settledFolds();
    assert.deepEqual((await hits("BlueLane")).map(([c]) => c), ["text"], "the user's words");
    assert.deepEqual((await hits("WisteriaTea")).map(([c]) => c), ["text"], "a reply that is not the last");
    assert.deepEqual((await hits("ThinkWord")).map(([c]) => c), ["thinking"], "a thought");
    assert.deepEqual((await hits("SilverKey")).map(([c]) => c), ["tool_output"], "an unfolded tool output");
    const quoted = await hits("HiddenCtx");
    assert.deepEqual(quoted.map(([c]) => c), ["quoted"], "a quoted context, as Quoted");
    assert.ok(!quoted[0][1].includes("persona-context"), "no tag in the snippet");
    assert.deepEqual((await hits("示例任务验收")).map(([c]) => c), ["event"], "an injected message by its line");
    await page.screenshot({ path: join(shots, `${scheme}-quoted.png`) });

    // a hidden tag, and an injected message's words for the model, are never found
    for (const word of ["ActionSecret", "persona-action", "InjectedRaw", "persona-supervision"]) assert.deepEqual(await hits(word), [], word);

    // folded work: found without opening it
    const folded = await hits("CopperKey");
    assert.deepEqual(folded.map(([c]) => c), ["tool_output"], "a folded tool output");
    await page.screenshot({ path: join(shots, `${scheme}-folded.png`) });
    // going to it opens its fold and rings its row
    await page.locator("[data-search-result]").first().click();
    await page.locator("[data-search-hit]").first().waitFor({ timeout: 10_000 });
    assert.match(await page.locator("[data-search-hit]").first().innerText(), /CopperKey|keys\.md|Read/, "the folded step it went to");
    await page.locator("[data-search-hit]").first().waitFor({ state: "detached", timeout: 10_000 }); // its ring goes after a moment
    await page.screenshot({ path: join(shots, `${scheme}-folded-landed.png`) });

    // early history: after "Load earlier and keep searching"
    assert.deepEqual(await hits("GlacierPost"), [], "not loaded yet");
    await page.getByRole("button", { name: "Load earlier and keep searching" }).click();
    await page.getByText("This is the start of the chat").waitFor({ timeout: 30_000 });
    await settledFolds();
    assert.deepEqual((await hits("GlacierPost")).map(([c]) => c), ["text"], "early history once loaded");
    await page.locator("[data-search-result]").first().click();
    await page.locator("[data-search-hit]", { hasText: "GlacierPost" }).waitFor({ timeout: 10_000 }).catch(async (e) => {
      await page.screenshot({ path: join(shots, `${scheme}-early-missed.png`) });
      throw new Error(`the early message it went to: ${e instanceof Error ? e.message.split("\n")[0] : e}`);
    });
    await page.screenshot({ path: join(shots, `${scheme}-early-landed.png`) });
    await page.close();
  }
  console.log(`RESULT: chat-search passed (screenshots in ${shots})`);
} catch (e) {
  failed = true;
  console.error(`RESULT: chat-search failed: ${e instanceof Error ? e.message : e}`);
} finally {
  clearTimeout(watchdog);
  await browser.close();
}
process.exit(failed ? 1 : 0);
