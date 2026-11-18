// A task its supervisor parked (#3804): the real web app with synthetic HTTP/WS only; it never connects to a user's hub
// or conversation. Parked and ready multi-PR waits share one peer group; active work and owner decisions keep theirs.
// Also checks the To-dos list and All board, all three languages, both themes and the largest app text size.
// node web/e2e/parked-task.mjs <web URL> --shots <directory> [--chromium <executable>]
import assert from "node:assert/strict";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const flag = (name) => { const i = args.indexOf(name); return i < 0 ? undefined : args[i + 1]; };
const base = args[0]?.replace(/\/$/, "");
const shots = flag("--shots");
if (!base || !shots) {
  console.error("usage: node web/e2e/parked-task.mjs <web URL> --shots <directory> [--chromium <executable>]");
  process.exit(2);
}
const origin = new URL(base);
assert(["127.0.0.1", "localhost", "[::1]"].includes(origin.hostname) && origin.protocol === "http:", "only an owned loopback HTTP web server is allowed");
assert(!origin.username && !origin.password && !origin.search && !origin.hash, "the web URL must not contain credentials");
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => { console.error("TIMEOUT parked-task browser verification (180s)"); process.exit(124); }, 180_000);
const sid = "parked-task-demo";
const REASON = "The design team has to pick the icon set first";
const words = { en: /^Parked until .+ · /, "zh-Hans": /^搁置到 .+ · /, ja: /^.+ まで保留 · / };
const chip = { en: "Parked", "zh-Hans": "已搁置", ja: "保留中" };
const group = { en: "Parked", "zh-Hans": "搁置", ja: "保留中" };
const all = { en: "All", "zh-Hans": "全部", ja: "すべて" };
const readings = [];

function todos() {
  // Parked a week ago, so an unparked task this still would be shelved. An elapsed fallback does not hide this hold.
  const at = new Date(Date.now() - 7 * 24 * 3600 * 1000).toISOString();
  const until = new Date(Date.now() - 3600 * 1000).toISOString();
  const pr = (number) => ({ url: `https://github.com/example/demo/pull/${number}`, state: "open", draft: false, review: "required", checks: "pending", conflicts: false, checked_at: new Date().toISOString() });
  const task = (id, title, supervision, more = {}) => ({ id, title, origin: sid, created_at: at, updated_at: at,
    agents: [{ machine_id: "demo", session_id: `${id}-worker`, linked_at: at }], supervision: { supervisor: sid, done_when: ["The icons are merged"], ...supervision }, ...more });
  return [
    task("demo-icons", "Example toolbar icons", { state: "parked", parked: { reason: REASON, until, waiting_on: "someone_else", count: 1 },
      reviews: [{ at, verdict: "parked", note: REASON }] }),
    task("demo-rollout", "Example rollout", { state: "needs_user" },
      { ask: { outcome: "decide", asked_at: at, verdict: "Needs your decision: roll out to everyone now, or to 10% first", recommend: "10% first" } }),
    task("demo-reviews", "Example schedule reviews", {},
      { agents: [], pr_url: pr(42).url, pr_status: pr(42), other_prs: [pr(41)] }),
    task("demo-writing", "Example draft in progress", {}),
  ];
}

async function run(browser, size, theme, language, textSize = "standard") {
  const context = await browser.newContext({ viewport: size === "wide" ? { width: 1280, height: 900 } : { width: 390, height: 844 }, colorScheme: theme, reducedMotion: "reduce" });
  await context.addInitScript(({ theme, language, textSize }) => {
    localStorage.setItem("persona.token", "synthetic-demo-token");
    localStorage.setItem("persona.language", language);
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.text-size", textSize);
    localStorage.setItem("persona.todos.view", "list");
    localStorage.setItem("persona.todos.grouping", "status");
  }, { theme, language, textSize });
  const at = new Date(Date.now() - 7 * 24 * 3600 * 1000).toISOString();
  const session = { machine_id: "demo", session_id: sid, title: "Example conversation", assistant: true, state: "idle", status: "idle", updated_at: at };
  const list = todos();
  const agents = list.flatMap((t) => t.agents.map((a) => ({ ...a, title: `${t.title} Agent`,
    state: t.id === "demo-writing" ? "working" : t.id === "demo-icons" ? "unread" : "idle",
    status: t.id === "demo-writing" ? "working" : t.id === "demo-icons" ? "unread" : "idle", updated_at: at, state_since: at })));
  const apiReads = [];
  await context.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname.replace(/^\/api\/v1/, "");
    if (request.method() !== "GET") { // the app's own bookkeeping (read marks); a parked task has nothing to press
      assert(!path.startsWith("/todos"), `nothing is written to a to-do: ${request.method()} ${path}`);
      await route.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true }) });
      return;
    }
    apiReads.push(path);
    let body = {};
    if (path === "/machines") body = { machines: [{ id: "demo", name: "Example computer", online: true, os: "linux" }] };
    else if (path === "/sessions") body = { sessions: [session, ...agents] };
    else if (path === "/interactions") body = { interactions: [] };
    else if (path === "/assistant") body = { machine_id: "demo", main: sid, conversations: [session], tasks: [], memory: [] };
    else if (path === "/todos") body = { todos: list };
    else if (path === "/assistant/repositories") body = { manager: request.headers()["x-persona-manager"] || "assistant", catalog_revision: 0, repositories: [] };
    else if (/^\/todos\/[^/]+\/repositories$/.test(path)) body = { schema_version: 1, manager: request.headers()["x-persona-manager"] || "assistant", todo_id: decodeURIComponent(path.split("/")[2]), revision: 0, repositories: [] };
    else if (path === `/sessions/demo/${sid}`) body = { session };
    else if (path.endsWith("/feed")) body = { items: [] };
    else if (path.endsWith("/items")) body = { items: [], has_more: false };
    else if (path === "/extensions/ui") body = { slots: [] };
    else if (path === "/extensions") body = { extensions: [] };
    else if (path === "/managers") body = { managers: [] };
    await route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  });
  await context.routeWebSocket("**/api/v1/ws**", (ws) => {
    ws.send(JSON.stringify({ type: "hello", server_version: "synthetic", hub_name: "Example hub", features: [] }));
    ws.onMessage((raw) => {
      const message = JSON.parse(String(raw));
      if (message.type === "app_ping") ws.send(JSON.stringify({ type: "pong" }));
    });
  });
  const page = await context.newPage();
  const errors = [];
  page.on("pageerror", (error) => errors.push(String(error)));
  page.setDefaultTimeout(12_000);
  const name = (step) => join(shots, `parked-${size}-${theme}-${language}-${textSize}-${step}.png`);
  const noOverflow = async () => assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > document.documentElement.clientWidth), false, "no horizontal page overflow");
  try {
    await page.goto(`${base}/`);
    // Needs you: the decision only
    await page.locator('[data-tray-entry="needs"]').click();
    await page.locator('[data-needs-row="task:demo-rollout"]').waitFor();
    assert.equal(await page.locator("[data-needs-row]").count(), 1, "Needs you lists the decision and not the parked task");
    assert.equal(await page.locator('[data-needs-row="task:demo-icons"]').count(), 0);
    // Active work does not contain either kind of external wait.
    await page.locator('[data-tray-entry="running"]').click();
    await page.locator("[data-task-slot]").waitFor();
    assert.equal(await page.locator("[data-task-slot]").count(), 1, "In progress lists only the active task");
    assert.equal(await page.locator("[data-parked-task-row]").count(), 0);
    assert.equal(await page.locator('[data-tray-entry="pr"]').count(), 0, "no competing Waiting for merge group");
    const beforeReads = apiReads.length;
    const feedback = await page.evaluate(async () => {
      const start = performance.now();
      document.querySelector('[data-tray-entry="parked"]').click();
      await new Promise(requestAnimationFrame);
      return { start, ms: performance.now() - start };
    });
    const rows = page.locator("[data-parked-task-row]");
    await rows.first().waitFor();
    assert.equal(await rows.count(), 2, "one parked task and one multi-PR task");
    assert.equal(await page.locator('[data-tray-entry="parked"]').textContent(), `${group[language]}2`);
    assert(await page.locator('[data-tray-entry="parked"]').evaluate((entry) => {
      const row = entry.getBoundingClientRect(), scroller = entry.parentElement.getBoundingClientRect();
      return row.left >= scroller.left - 1 && row.right <= scroller.right + 1;
    }), "the selected wait group and its count stay fully visible, including at large text size");
    const readyMs = await page.evaluate((start) => performance.now() - start, feedback.start);
    const groupApiReads = apiReads.length - beforeReads;
    assert.equal(groupApiReads, 0, "opening the group uses local state, not a new API read");
    assert(feedback.ms < 1000, "the selected group is painted within one second");
    assert(readyMs < 1000, "the wait rows are ready within one second on isolated loopback");
    assert.equal(await page.locator("[data-progress-shelved]").count(), 0, "a parked task is not folded under shelved");
    assert.equal(await rows.locator(".text-green").count(), 0, "an unread Agent turn does not give a parked task a green completion mark");
    assert.equal(await page.locator('[data-parked-task-row][data-counts-open="true"]').count(), 2);
    const text = (await page.locator('[data-parked-task-row="demo-icons"]').innerText()).split("\n").map((line) => line.trim()).filter(Boolean);
    assert(text.includes(chip[language]), `the row's state word is ${chip[language]}: ${JSON.stringify(text)}`);
    const line = text.find((part) => part.includes(REASON));
    assert(line && words[language].test(line), `the row says until when and why: ${JSON.stringify(text)}`);
    assert.deepEqual(await page.locator('[data-pr-waiting-row="demo-reviews"] a').evaluateAll((links) => links.map((a) => a.href)), [
      "https://github.com/example/demo/pull/42", "https://github.com/example/demo/pull/41",
    ]);
    await noOverflow();
    await page.screenshot({ path: name("tray") });
    await page.locator(".assistant-action-tray").getByRole("button", { name: all[language], exact: true }).click();
    const board = page.locator('[data-testid="task-board"] [data-board-section="parked"]');
    await board.waitFor();
    assert.equal(await board.locator("h3").innerText(), `${group[language]} · 2`);
    assert((await board.innerText()).includes("Example toolbar icons"));
    assert((await board.innerText()).includes("Example schedule reviews"));
    assert.equal(await board.locator(".text-green").count(), 0);
    await noOverflow();
    await page.screenshot({ path: name("board") });
    await page.goto(`${base}/todos`);
    const section = page.locator('[data-testid="todo-section"][data-section="parked"]');
    await section.waitFor();
    assert.equal(await section.getAttribute("data-todos"), "2");
    assert.equal(await section.locator("[data-todo]").count(), 2);
    assert.equal(await page.locator('[data-section="pr"]').count(), 0);
    assert.equal(await section.locator('a[href="https://github.com/example/demo/pull/42"]').count(), 1);
    if (size === "wide") {
      const empty = JSON.parse(readFileSync(new URL(`../src/i18n/locales/${language}/task-repositories.json`, import.meta.url), "utf8")).no_links;
      await page.locator("[data-todo-repositories]").getByText(empty, { exact: true }).waitFor();
      assert.equal(await page.locator("[data-todo-repositories] .text-red").count(), 0, "the isolated fixture has a valid empty repository envelope");
    }
    await noOverflow();
    await page.screenshot({ path: name("list") });
    assert.deepEqual(errors, []);
    readings.push({ size, theme, language, textSize, needsYou: 1, inProgress: 1, parked: 2, boardParked: 2, listParked: 2, feedbackMs: feedback.ms, readyMs, groupApiReads });
    console.log(`PASS ${size} ${theme} ${language} ${textSize}: needs_you=1, in_progress=1, parked=2, board=2, list=2, feedback=${feedback.ms.toFixed(1)}ms, ready=${readyMs.toFixed(1)}ms, group_api_reads=0`);
  } catch (error) {
    await page.screenshot({ path: name("failed") }).catch(() => {});
    throw error;
  } finally {
    await context.close();
  }
}

let browser;
try {
  browser = await chromium.launch({ executablePath: flag("--chromium") || "/usr/bin/google-chrome", headless: true });
  for (const theme of ["light", "dark"]) {
    for (const textSize of ["standard", "large"]) {
      await run(browser, "wide", theme, "en", textSize);
      await run(browser, "narrow", theme, "en", textSize);
    }
  }
  for (const language of ["zh-Hans", "ja"]) await run(browser, "narrow", "light", language);
  writeFileSync(join(shots, "readings.json"), JSON.stringify(readings, null, 2) + "\n");
  console.log("PASS parked-task browser verification");
} catch (error) {
  console.error("FAIL parked-task browser verification", error);
  process.exitCode = 1;
} finally {
  clearTimeout(watchdog);
  await browser?.close();
}
