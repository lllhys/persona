// End-to-end check of Forward (#3899, docs/web/web.md "Forwarding a message"): a reply forwarded from one Agent's chat
// to another Agent and to Persona, as the user does it. The reply's actions (the right-click menu on a wide screen, the
// reply's ⋯ on a phone) › Forward › Forward to (a dialog; a sheet from the bottom on a phone) › search "Astro" › the
// target's chat opens with the forward over its box and the line saying which computer it comes from and goes to; a
// note typed and sent goes out as one message, the `<persona-context kind="forward">` block before the note
// (spec/fixtures/forward); the sent bubble shows the forward as a card, and a click on it opens the original chat.
// Then the same with Persona (the main chat) as the target. Several at once (#3838): Forward to's Select messages… ›
// the reply is picked and the bar takes the composer's place › Cancel leaves the chat as it was › again from the message
// menu's last row, Select messages (#9469), which never shows Forward to › with none picked Forward is off › your message
// and the reply › Forward › the blog's Agent: one card over its box, and the hub gets two blocks in the chat's order, then
// the note; the sent bubble shows them as one card.
//
// Needs the web page against the fake hub, which echoes what is sent into the transcript, and Chrome (Edge with
// --channel msedge, any Chromium through --executable):
//
//   MOCK_ECHO_SENT=1 PORT=18931 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18931 npx vite --host 127.0.0.1 --port 5931 --strictPort &)
//   node web/e2e/forward-message.mjs http://127.0.0.1:5931 --hub http://127.0.0.1:18931 [--shots <dir>] [--channel msedge | --executable <chromium>]
//
// Runs wide (1440×900) and phone width (390×844, touch), light and dark, in English; --shots saves the menu, the dialog,
// the composer and the sent bubble of each. Prints one line per check and a verdict; exits 1 on a failure and after
// eight minutes. Every request outside the two loopback servers is refused.
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
const executable = flag("--executable");
const channel = flag("--channel") ?? "chrome";
if (!base || !hub) {
  console.error("usage: node web/e2e/forward-message.mjs <web URL> --hub <mock hub URL> [--shots <dir>] [--channel msedge | --executable <chromium>]");
  process.exit(2);
}
for (const url of [base, hub]) {
  if (!["127.0.0.1", "localhost"].includes(new URL(url).hostname)) {
    console.error(`only a loopback mock is allowed: ${url}`);
    process.exit(2);
  }
}
if (shots) mkdirSync(shots, { recursive: true });
setTimeout(() => {
  console.log("FAIL timed out after 8 minutes");
  process.exit(1);
}, 480_000).unref();

const ok = (msg) => console.log(`ok  ${msg}`);
function fail(msg) {
  console.log(`FAIL ${msg}`);
  process.exit(1);
}

// the mock hub's data (scripts/mock/mock_hub.py): the PR review on the MacBook, the blog on the Mac Studio
const REVIEW = "a1f3c2d4-0000-4000-8000-000000000006";
const BLOG = "a1f3c2d4-0000-4000-8000-000000000003";
const REPLY = "PR #42 看完了"; // i18n-ignore: the mock hub's reply in that chat
const ASK = "看一下 PR #42"; // i18n-ignore: the mock hub's message of yours before it
const get = async (path) => (await fetch(`${hub}${path}`, { headers: { Authorization: "Bearer mock" }, signal: AbortSignal.timeout(5000) })).json();
const assistant = await get("/api/v1/assistant");
if (!assistant.main) fail("the fake hub has no main conversation");
const sentTo = async () => (await get("/mock/sent_to")).sent ?? [];
// this run's notes are its own: a fake hub that ran the script before holds the earlier runs' bubbles
const run = Date.now().toString(36).slice(-4);

async function open(browser, theme, phone) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: 2,
    colorScheme: theme,
    locale: "en-US",
    hasTouch: phone,
    isMobile: phone,
  });
  await ctx.route("**/*", (route) => {
    const origin = new URL(route.request().url()).origin;
    return [new URL(base).origin, new URL(hub).origin].includes(origin) ? route.continue() : route.abort();
  });
  await ctx.addInitScript((theme) => {
    localStorage.setItem("persona.language", "en");
    localStorage.setItem("persona.theme", theme);
  }, theme);
  const page = await ctx.newPage();
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(`${base}/?token=mock`);
  await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15000 });
  return { ctx, page, errors };
}

/** Moves to an in-app path the way a link does (no reload: the page keeps what it holds). */
const go = (page, path) =>
  page.evaluate((path) => {
    history.pushState(null, "", path);
    dispatchEvent(new PopStateEvent("popstate"));
  }, path);

/** The reply's actions: the right-click menu on a wide screen, its ⋯ on a phone. */
async function openActions(page, phone) {
  const reply = page.locator("main [data-answer]").filter({ hasText: REPLY }).last();
  await reply.waitFor({ state: "visible" });
  await reply.scrollIntoViewIfNeeded();
  if (phone) {
    await reply.locator("xpath=../..").getByRole("button", { name: "More", exact: true }).last().click();
  } else {
    await reply.hover();
    await reply.click({ button: "right", position: { x: 40, y: 8 } });
  }
  const menu = page.locator('[role="menu"]').last();
  await menu.waitFor({ state: "visible" });
  return menu;
}

const shot = async (page, name) => {
  if (!shots) return;
  await page.waitForTimeout(350); // let the open animations finish
  await page.screenshot({ path: join(shots, `${name}.png`) });
};

async function forwardTo(page, phone, where, target) {
  const dialog = await openForward(page, phone, where);
  if (target === "persona") {
    // the chat forwarded to a moment ago leads the list, under Recent
    if (!(await dialog.locator(`[data-forward-section="recent"] [data-forward-target="studio/${BLOG}"]`).count())) fail(`${where}: the last target is not under Recent`);
    await dialog.locator('[data-forward-target="persona"]').click();
  } else {
    await dialog.getByRole("textbox", { name: "Search chats, computers or projects" }).fill("Astro");
    const hit = dialog.locator(`[data-forward-target="studio/${BLOG}"]`);
    await hit.waitFor({ state: "visible" });
    const count = await dialog.locator("[data-forward-target]").count();
    if (count !== 1) fail(`${where}: searching Astro found ${count} chats`);
    await hit.click();
  }
  await dialog.waitFor({ state: "hidden" });
}

async function openForward(page, phone, where, quiet = false) {
  const menu = await openActions(page, phone);
  const item = menu.getByRole("menuitem", { name: "Forward", exact: true });
  if (!(await item.count())) fail(`${where}: no Forward in the reply's actions`);
  if (await menu.getByRole("menuitem", { name: "Hand to Persona" }).count()) fail(`${where}: Hand to Persona is still there`);
  if (!quiet) await shot(page, `${where}-1-menu`);
  await item.click();
  const dialog = page.locator('[role="dialog"]').filter({ has: page.locator("[data-forward-dialog]") });
  await dialog.waitFor({ state: "visible" });
  if (!(await dialog.getByText("Forward to", { exact: true }).count())) fail(`${where}: the dialog is not titled Forward to`);
  const rows = dialog.locator("[data-forward-target]");
  const firstTwo = await rows.evaluateAll((els) => els.slice(0, 2).map((e) => e.getAttribute("data-forward-target")));
  if (JSON.stringify(firstTwo) !== JSON.stringify(["persona", "new"])) fail(`${where}: the list does not start with Persona, New agent: ${firstTwo}`);
  if (await dialog.locator(`[data-forward-target="macbook/${REVIEW}"]`).count()) fail(`${where}: the chat it comes from is offered`);
  if (!(await dialog.locator("[data-forward-bar]").filter({ hasText: "Forwarded from Claude" }).count())) fail(`${where}: no preview of the message`);
  if (!quiet) await shot(page, `${where}-2-dialog`);
  return dialog;
}

/** Forward to's Select messages… (#3838): it closes, and the chat picks with the reply picked. */
async function startPicking(page, phone, where, quiet) {
  const dialog = await openForward(page, phone, where, quiet);
  const select = dialog.locator("[data-forward-select]");
  if ((await select.textContent())?.trim() !== "Select messages…") fail(`${where}: Forward to has no Select messages…`);
  await select.click();
  await dialog.waitFor({ state: "hidden" });
  const bar = page.locator("main [data-pick-bar]");
  await bar.waitFor({ state: "visible" });
  return bar;
}

/** The message menu's last row, Select messages (#9469): the chat picks with the reply picked, Forward to never shows. */
async function startPickingFromMenu(page, phone, where) {
  const menu = await openActions(page, phone);
  const last = menu.getByRole("menuitem").last();
  if ((await last.textContent())?.trim() !== "Select messages") fail(`${where}: the reply's actions do not end in Select messages`);
  await last.click();
  const bar = page.locator("main [data-pick-bar]");
  await bar.waitFor({ state: "visible" });
  if (await page.locator("[data-forward-dialog]").count()) fail(`${where}: Select messages showed Forward to`);
  return bar;
}

/** Several at once (#3838), from the review chat to the blog's Agent. */
async function forwardSeveral(page, phone, tag) {
  const where = `several-${tag}`;
  // the checkbox over a message is named by its words
  const box = (text) => page.locator(`main [role="checkbox"][aria-label^="${text}"]`).last();
  const count = async (bar) => (await bar.locator("[data-pick-count]").textContent())?.trim();
  await go(page, `/s/macbook/${REVIEW}?view=chat`);
  let bar = await startPicking(page, phone, where, false);
  if (await page.locator("main textarea").last().isVisible()) fail(`${where}: the composer is still there while picking`);
  if ((await count(bar)) !== "1 selected" || (await box(REPLY).getAttribute("aria-checked")) !== "true") fail(`${where}: the reply Forward to was opened for is not the one picked`);
  await box(ASK).click();
  if ((await count(bar)) !== "2 selected" || (await box(ASK).getAttribute("aria-checked")) !== "true") fail(`${where}: a click did not pick your message`);
  await shot(page, `${where}-3-picking`);
  // Cancel: the chat as it was
  await bar.getByRole("button", { name: "Cancel", exact: true }).click();
  await bar.waitFor({ state: "hidden" });
  if (await page.locator('main [role="checkbox"]').count()) fail(`${where}: messages are still checkboxes after Cancel`);
  await page.locator("main textarea").last().waitFor({ state: "visible" });
  ok(`${where}: Select messages… picked the reply; Cancel left the chat as it was`);
  // again, from the message menu: with none picked Forward is off
  bar = await startPickingFromMenu(page, phone, where);
  if ((await count(bar)) !== "1 selected" || (await box(REPLY).getAttribute("aria-checked")) !== "true") fail(`${where}: the menu's Select messages did not pick the reply`);
  ok(`${where}: the message menu's Select messages picked the reply`);
  await box(REPLY).click();
  const forward = bar.getByTestId("pick-forward");
  if ((await count(bar)) !== "0 selected" || !(await forward.isDisabled())) fail(`${where}: Forward is not off with none picked`);
  await shot(page, `${where}-4-none`);
  await box(ASK).click();
  await box(REPLY).click();
  if ((await count(bar)) !== "2 selected" || (await forward.textContent())?.trim() !== "Forward") fail(`${where}: the bar says ${await count(bar)}, ${await forward.textContent()}`);
  await forward.click();
  const dialog = page.locator('[role="dialog"]').filter({ has: page.locator("[data-forward-dialog]") });
  await dialog.waitFor({ state: "visible" });
  if (await dialog.locator("[data-forward-select]").count()) fail(`${where}: Select messages… is offered for the picked ones`);
  const preview = dialog.locator('[data-forward-bar][data-forward-group="2"]');
  if (!(await preview.count()) || !(await preview.textContent()).includes("2 forwarded messages")) fail(`${where}: the preview is not one card of two`);
  await shot(page, `${where}-5-dialog`);
  await dialog.getByRole("textbox", { name: "Search chats, computers or projects" }).fill("Astro");
  await dialog.locator(`[data-forward-target="studio/${BLOG}"]`).click();
  await dialog.waitFor({ state: "hidden" });
  await page.waitForURL((u) => new URL(u).pathname.endsWith(`/${BLOG}`));
  if (await page.locator("main [data-pick-bar]").count()) fail(`${where}: still picking after they went`);
  const over = page.locator('main [data-forward-bar][data-forward-group="2"]').last();
  await over.waitFor({ state: "visible" });
  const route = (await over.locator("[data-forward-route]").textContent())?.trim();
  if (route !== "From MacBook · to Claude on Mac Studio") fail(`${where}: the route line says ${JSON.stringify(route)}`);
  const lines = await over.textContent();
  if (!lines.includes(`Me: ${ASK}`) || !lines.includes(`Claude: ${REPLY}`)) fail(`${where}: the card over the box says ${JSON.stringify(lines)}`);
  ok(`${where}: both are one card over the blog's box, "${route}"`);
  const note = `Same for the blog (${tag} ${run})`;
  const input = page.locator("main textarea").last();
  await input.click();
  await input.fill(note);
  await shot(page, `${where}-6-composer`);
  const before = (await sentTo()).length;
  await page.locator("main").getByRole("button", { name: /^(Send|Queue message)$/ }).last().click();
  let sent;
  for (let i = 0; i < 50 && !sent; i++) {
    const list = await sentTo();
    if (list.length > before) sent = list.at(-1);
    else await page.waitForTimeout(100);
  }
  if (!sent) fail(`${where}: nothing reached the hub`);
  const blocks = [...sent.text.matchAll(/<(?:persona|jarvis)-context kind="forward" [^>]*role="(\w+)"[^>]*>\n([^\n]*)/g)].map((m) => [m[1], m[2].slice(0, ASK.length)]);
  if (JSON.stringify(blocks) !== JSON.stringify([["user", ASK], ["agent", REPLY.slice(0, ASK.length)]])) fail(`${where}: the blocks are ${JSON.stringify(blocks)}`);
  if (!/-context>\n<(persona|jarvis)-context kind="forward"/.test(sent.text) || !sent.text.endsWith(`-context>\n\n${note}`)) fail(`${where}: not a block per line, then the note: ${sent.text.slice(-300)}`);
  ok(`${where}: the hub got two blocks in the chat's order, then the note`);
  const bubble = page.locator("main [data-user-bubble]:not([data-send-state])").filter({ hasText: note }).last();
  await bubble.waitFor({ state: "visible" });
  if (!(await bubble.locator('[data-forward-group="2"]').count())) fail(`${where}: the sent bubble does not show them as one card`);
  await bubble.scrollIntoViewIfNeeded();
  ok(`${where}: the sent bubble shows them as one card`);
  await shot(page, `${where}-7-sent`);
}

async function sendNote(page, where, expect) {
  const bar = page.locator("main [data-forward-bar]").last();
  await bar.waitFor({ state: "visible" });
  const route = (await bar.locator("[data-forward-route]").textContent())?.trim();
  if (route !== expect.route) fail(`${where}: the route line says ${JSON.stringify(route)}, not ${JSON.stringify(expect.route)}`);
  if (!(await bar.textContent()).includes(REPLY)) fail(`${where}: the forward over the box does not show the reply`);
  ok(`${where}: the forward is over the box, "${route}"`);
  const input = page.locator("main textarea").last();
  await input.click();
  await input.fill(expect.note);
  await shot(page, `${where}-3-composer`);
  const before = (await sentTo()).length;
  // the round button is "Queue message" while the chat's agent works (#5324)
  await page.locator("main").getByRole("button", { name: /^(Send|Queue message)$/ }).last().click();
  // what the hub got: one message, the forward block, then the note
  let sent;
  for (let i = 0; i < 50 && !sent; i++) {
    const list = await sentTo();
    if (list.length > before) sent = list.at(-1);
    else await page.waitForTimeout(100);
  }
  if (!sent) fail(`${where}: nothing reached the hub`);
  if (sent.machine_id !== expect.mid || sent.session_id !== expect.sid) fail(`${where}: sent to ${sent.machine_id}/${sent.session_id}`);
  // the tag's and the link's old names until the hub says it takes the new ones (lib/compatibility.ts messageForHub)
  const head = new RegExp(
    `^<(persona|jarvis)-context kind="forward" title="Forwarded from Claude · [^"]+" url="(persona|jarvis)://session/macbook/${REVIEW}\\?at=[^"]+" msg="[^"]+" role="agent" author="Claude" chat="[^"]+" project="[^"]+" machine="MacBook" time="\\d{4}-\\d\\d-\\d\\dT\\d\\d:\\d\\d:\\d\\d(Z|[+-]\\d\\d:\\d\\d)">\\n${REPLY}`,
  );
  if (!head.test(sent.text)) fail(`${where}: the block is not the shared format: ${sent.text.slice(0, 400)}`);
  if (!/<\/(persona|jarvis)-context>\n\n/.test(sent.text) || !sent.text.endsWith(`-context>\n\n${expect.note}`)) fail(`${where}: the note does not follow the block: ${sent.text.slice(-200)}`);
  ok(`${where}: the hub got the forward block, then the note`);
  // the sent bubble, once the transcript has it (the fake hub writes it in a moment later)
  const bubble = page.locator("main [data-user-bubble]:not([data-send-state])").filter({ hasText: expect.note }).last();
  await bubble.waitFor({ state: "visible" });
  const card = bubble.locator("[data-forward]");
  if (!(await card.count())) fail(`${where}: the sent bubble has no forward card`);
  const text = await card.textContent();
  if (!text.includes("Forwarded from Claude") || !text.includes("MacBook") || !text.includes(REPLY)) fail(`${where}: the card says ${JSON.stringify(text)}`);
  if (await bubble.getByText("Context ·").count()) fail(`${where}: the forward also folds as a context row`);
  await bubble.scrollIntoViewIfNeeded();
  ok(`${where}: the sent bubble shows the forward as a card`);
  await shot(page, `${where}-4-sent`);
  return card;
}

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel });
try {
  for (const phone of [false, true]) {
    for (const theme of ["light", "dark"]) {
      const width = phone ? "phone" : "wide";
      const { ctx, page, errors } = await open(browser, theme, phone);
      const tag = `${width}-${theme}`;

      // to the blog's Agent on the Mac Studio
      await go(page, `/s/macbook/${REVIEW}?view=chat`);
      await forwardTo(page, phone, `forward-${tag}-agent`, "agent");
      // an Agent's page may settle on its terminal tab's address (/t/…/<session>)
      await page.waitForURL((u) => new URL(u).pathname.endsWith(`/${BLOG}`));
      ok(`${tag}: picking the chat opened it`);
      const card = await sendNote(page, `forward-${tag}-agent`, { route: "From MacBook · to Claude on Mac Studio", note: `Check the build (${tag} ${run})`, mid: "studio", sid: BLOG });
      await card.locator("button").first().click();
      await page.waitForURL((u) => new URL(u).pathname.endsWith(`/${REVIEW}`));
      await page.locator("main [data-search-hit]").first().waitFor({ state: "attached", timeout: 8000 }).catch(() => fail(`${tag}: the original message was not brought into view`));
      ok(`${tag}: a click on the card opened the original chat at the message`);

      // to Persona, the main chat
      await forwardTo(page, phone, `forward-${tag}-persona`, "persona");
      await page.waitForURL((u) => new URL(u).pathname === "/");
      ok(`${tag}: picking Persona opened the main chat`);
      await sendNote(page, `forward-${tag}-persona`, { route: "From MacBook · to Persona", note: `What do you think? (${tag} ${run})`, mid: assistant.machine_id, sid: assistant.main });

      // to a new Agent: New agent opens with the forward over its first message; nothing starts here
      if (theme === "light") {
        await go(page, `/s/macbook/${REVIEW}?view=chat`);
        const dialog = await openForward(page, phone, `forward-${tag}-new`);
        await dialog.locator('[data-forward-target="new"]').click();
        const form = page.locator("[data-new-agent-forward]");
        await form.waitFor({ state: "visible" }).catch(() => fail(`${tag}: New agent has no forward over its first message`));
        if (!(await form.textContent()).includes(REPLY)) fail(`${tag}: New agent's forward does not show the reply`);
        await form.scrollIntoViewIfNeeded();
        await shot(page, `forward-${tag}-new-3-form`);
        await page.keyboard.press("Escape");
        await form.waitFor({ state: "hidden" });
        ok(`${tag}: New agent opened with the forward over its first message`);
      }

      // several at once (#3838)
      await forwardSeveral(page, phone, tag);

      if (errors.length) fail(`${tag}: page errors: ${errors.join(" | ")}`);
      await ctx.close();
    }
  }
} finally {
  await browser.close();
}
console.log("PASS web forward e2e");
process.exit(0);
