// End-to-end check of the assistant's alerts on an open page (#8145, docs/web/web.md "Attention signals"): with the
// alert sound and system notifications on, the page asks the hub for its `notify` events (client_hello `ws_notify`),
// and a new ask_user card in the main chat or a leader's chat, and the assistant's notify_user, post a system
// notification and play the "Needs you" sound while the page is not in front; the hub's event for an Agent's card adds
// nothing to the page's own; with both switches off the page does not ask.
//
// Against the fake hub with leaders, and the app (Vite's dev server is enough):
//
//   MOCK_LEADERS=1 PORT=18799 python3 scripts/mock/mock_hub.py &
//   (cd web && PERSONA_HUB_URL=http://127.0.0.1:18799 npx vite --host 127.0.0.1 --port 5799 --strictPort &)
//   node web/e2e/assistant-alerts.mjs http://127.0.0.1:5799 --hub http://127.0.0.1:18799 [--shots <dir>] [--executable <chromium>]
//
// The browser's notifications are recorded, not shown (a headless browser has no notification centre), and its sound
// is counted at the oscillators the page starts; "not in front" is the window losing the focus. Prints one line per
// check and a verdict; exits 1 on a failure and after three minutes. The page reaches only the two loopback servers.
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
if (!base || !hub) {
  console.error("usage: node web/e2e/assistant-alerts.mjs <web URL> --hub <mock hub URL> [--shots <dir>] [--executable <chromium>]");
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
  console.log("FAIL timed out after 3 minutes");
  process.exit(1);
}, 180_000).unref();

const ok = (msg) => console.log(`ok  ${msg}`);
let cleanup = () => {};
function fail(msg) {
  console.log(`FAIL ${msg}`);
  cleanup();
  process.exit(1);
}

const hubGet = async (path) => {
  const r = await fetch(`${hub}${path}`, { headers: { Authorization: "Bearer mock" }, signal: AbortSignal.timeout(5000) });
  return r.json();
};
const assistant = await hubGet("/api/v1/assistant");
const main = assistant.main;
const leader = (assistant.leaders ?? [])[0];
if (!main || !leader) fail("the fake hub has no main chat or no leader: start it with MOCK_LEADERS=1");
const sessions = (await hubGet("/api/v1/sessions")).sessions ?? [];
const agentKeys = new Set(sessions.filter((s) => !s.assistant).map((s) => `${s.machine_id}/${s.session_id}`));
const agentCard = ((await hubGet("/api/v1/interactions")).interactions ?? []).find((ix) => ix.session_id && !ix.folded_under && agentKeys.has(`${ix.machine_id}/${ix.session_id}`));
if (!agentCard) fail("the fake hub has no Agent card to push");

const browser = await chromium.launch(executable ? { executablePath: executable } : {});
cleanup = () => void browser.close().catch(() => {});
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2, locale: "en-US" });
await ctx.grantPermissions(["notifications"], { origin: base });
await ctx.addInitScript((on) => {
  localStorage.setItem("persona.language", "en");
  if (on) {
    localStorage.setItem("persona.systemNotify", "1");
    localStorage.setItem("persona.attentionSound", "1");
  }
  // what the page posts, instead of the screen; whether its window is the one in front (the test moves the focus away)
  window.__posted = [];
  window.__tones = 0;
  window.__away = false;
  const Real = window.Notification;
  class Recorded {
    static get permission() { return Real.permission; }
    static requestPermission() { return Real.requestPermission(); }
    constructor(title, opts = {}) {
      this.title = title;
      this.tag = opts.tag;
      window.__posted.push({ title, body: opts.body ?? "", tag: opts.tag ?? "", silent: Boolean(opts.silent) });
    }
    close() {}
    addEventListener() {}
  }
  window.Notification = Recorded;
  const make = AudioContext.prototype.createOscillator;
  AudioContext.prototype.createOscillator = function (...a) {
    window.__tones++;
    return make.apply(this, a);
  };
  Document.prototype.hasFocus = function () { return !window.__away; };
}, true);
const page = await ctx.newPage();
page.on("pageerror", (e) => console.log(`page error: ${e.message}`));

const posted = () => page.evaluate(() => window.__posted);
const tones = () => page.evaluate(() => window.__tones);
async function waitFor(what, check, ms = 5000) {
  const until = Date.now() + ms;
  while (Date.now() < until) {
    if (await check()) return;
    await page.waitForTimeout(100);
  }
  fail(what);
}

await page.goto(`${base}/?token=mock`);
await page.waitForFunction(() => localStorage.getItem("persona.token"), null, { timeout: 15000 }).catch(() => fail("the page did not sign in to the fake hub"));
await page.locator("body").click({ position: { x: 5, y: 5 } }); // the gesture that lets the page play sound
await waitFor("the page's client_hello did not declare ws_notify", async () => ((await hubGet("/mock/hello")).features ?? []).includes("ws_notify"));
ok("with the sound and system notifications on, client_hello declares ws_notify");
// away from the window, past the moment after the hello whose events are taken as kept ones
await page.evaluate(() => { window.__away = true; window.dispatchEvent(new Event("blur")); });
await page.waitForTimeout(3500);

const before = await tones();
const asked = await hubGet(`/mock/assistant_ask?session=${main}&question=${encodeURIComponent("Move tomorrow's meeting to the afternoon?")}`);
if (!asked.sent) fail("the fake hub sent the card's notify to no connection");
const mainTag = `${assistant.machine_id}/${main}#needs`;
await waitFor(`no system notification for the main chat's ask_user card (${mainTag})`, async () => (await posted()).some((n) => n.tag === mainTag));
ok(`the main chat's new ask_user card posts a notification: ${JSON.stringify((await posted()).find((n) => n.tag === mainTag))}`);
await waitFor("no Needs you sound for the main chat's card", async () => (await tones()) >= before + 2);
ok("and plays the Needs you sound");

await page.waitForTimeout(3100); // the sound's rate limit
const leaderTones = await tones();
await hubGet(`/mock/assistant_ask?session=${leader}&question=${encodeURIComponent("Ship the direction's plan as it is?")}`);
const leaderTag = `${assistant.machine_id}/${leader}#needs`;
await waitFor(`no system notification for the leader's ask_user card (${leaderTag})`, async () => (await posted()).some((n) => n.tag === leaderTag));
await waitFor("no Needs you sound for the leader's card", async () => (await tones()) >= leaderTones + 2);
ok("a leader's new ask_user card posts one and sounds too");

await page.waitForTimeout(3100);
const toldTones = await tones();
await hubGet(`/mock/notify_user?session=${main}&title=Persona&body=${encodeURIComponent("The quarterly report is ready for your signature")}`);
await waitFor("no system notification for notify_user", async () => (await posted()).some((n) => n.body.includes("quarterly report")));
await waitFor("no sound for notify_user", async () => (await tones()) >= toldTones + 2);
ok(`the assistant's notify_user posts one and sounds: ${JSON.stringify((await posted()).find((n) => n.body.includes("quarterly report")))}`);

const count = (await posted()).length;
await hubGet(`/mock/notify?interaction=${agentCard.id}`);
await page.waitForTimeout(1500);
if ((await posted()).length !== count) fail(`the hub's notify for an Agent's card posted again: ${JSON.stringify((await posted()).slice(count))}`);
ok("the hub's notify for an Agent's card adds nothing to the page's own");

if (shots) {
  // what was posted, drawn over the page that posted it
  await page.evaluate(() => {
    const box = document.createElement("div");
    box.style.cssText = "position:fixed;top:16px;right:16px;z-index:99999;width:420px;font:13px system-ui;display:grid;gap:8px";
    for (const n of window.__posted) {
      const card = document.createElement("div");
      card.style.cssText = "background:#fff;color:#111;border-radius:12px;padding:10px 12px;box-shadow:0 4px 18px rgba(0,0,0,.25);white-space:pre-line";
      card.textContent = `${n.title}\n${n.body}\n[${n.tag}]`;
      box.append(card);
    }
    document.body.append(box);
  });
  await page.screenshot({ path: join(shots, "assistant-alerts-posted.png") });
}

// both switches off: the page does not ask, so the hub counts it as no device for these
await page.evaluate(() => {
  localStorage.removeItem("persona.systemNotify");
  localStorage.removeItem("persona.attentionSound");
  window.dispatchEvent(new StorageEvent("storage", { key: "persona.systemNotify" }));
});
await waitFor("client_hello still declares ws_notify with both switches off", async () => !((await hubGet("/mock/hello")).features ?? []).includes("ws_notify"));
ok("with both switches off, the page says client_hello again without ws_notify");

console.log("PASS assistant alerts on an open page");
cleanup();
process.exit(0);
