// Real app, synthetic HTTP/WS only. A shared CDP run leaves Chrome and evidence tabs open.
// node web/e2e/subagent-reports.mjs <loopback web URL> <screenshot directory> [CDP URL | Chrome executable] [standard | large]
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";
const [base, shots, cdp = "http://127.0.0.1:9222", textSize = "standard"] = process.argv.slice(2);
const shared = /^https?:/.test(cdp);
if (!base || !shots || !["standard", "large"].includes(textSize) || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/subagent-reports.mjs <loopback web URL> <screenshot directory> [CDP URL]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => { console.error("TIMEOUT subagent report verification (180s)"); process.exit(124); }, 180_000);
const AT = "2026-11-01T10:00:00Z";
const item = (seq, kind, extra = {}) => ({ id: `report-example-${seq}#0`, seq, ts: new Date(Date.parse(AT) + seq * 1000).toISOString(), role: kind === "tool_use" || kind === "text" && seq !== 1 ? "assistant" : "user", kind, ...extra });
const task = (status) => ({ kind: "agent", task_id: "example-agent", tool_use_id: "example-launch", status, title: "Check the example tests" });
const report = (seq, status, text) => item(seq, "tool_result", { tool_use_id: "example-launch", text, task: { ...task(status), detail: text.slice(0, 4096) } });
const initial = [
  item(1, "text", { text: "Check the example project and report your findings." }),
  item(2, "tool_use", { tool_name: "Agent", tool_use_id: "example-launch", tool_input: { description: "Check the example tests", prompt: "Inspect the fictional test suite and share progress before the final report.", subagent_type: "general-purpose" } }),
  item(3, "tool_result", { tool_use_id: "example-launch", text: "Async agent launched successfully.\nagentId: example-agent\nThe agent is working in the background.", task: task("running") }),
];
const progress = report(4, "running", "## First findings\nThe fictional checks are still running.\n\n- The example files are loaded.\n- The remaining test cases are being checked.");
const terminal = report(6, "completed", "## Final findings\nEvery fictional check passed.\n\nThe synthetic test suite and build are complete. Earlier progress remains available above.");
let browser;
async function verify(theme, width) {
  let items = initial;
  let agentRunning = true;
  let live = true;
  let socket;
  const sid = `example-reports-${theme}-${width}`;
  const subagent = () => ({ id: "example-agent", session_id: `${sid}@example-agent`, type: "general-purpose", tool_use_id: "example-launch", running: agentRunning, started_at: AT, updated_at: atNow(), steps: agentRunning ? 3 : 5, last_tool: { id: "example-check", name: "Bash", summary: "Checking fictional tests" } });
  const session = () => ({ machine_id: "example", session_id: sid, title: "Example Agent reports", agent: "claude", cwd: "/tmp/example-project", state: live ? "working" : "idle", status: live ? "working" : "idle", running: true, updated_at: AT, state_since: AT, subagents: [subagent()], capabilities: { read: true, send: true, interrupt: true } });
  const context = shared ? browser.contexts()[0] : await browser.newContext();
  const page = await context.newPage();
  await page.setViewportSize({ width, height: 1000 });
  await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
  await page.addInitScript(({ theme, at, textSize }) => {
    localStorage.setItem("persona.text-size", textSize);
    localStorage.setItem("persona.token", "synthetic-example-token");
    localStorage.setItem("persona.language", "en");
    localStorage.setItem("persona.theme", theme);
    const NativeDate = Date;
    globalThis.Date = class extends NativeDate {
      constructor(...args) { super(...(args.length ? args : [at])); }
      static now() { return at; }
    };
  }, { theme, at: Date.parse(AT) + 10000, textSize });
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace(/^\/api\/v1/, "");
    assert(["GET", "HEAD"].includes(route.request().method()) || /\/(seen|read|today)$/.test(path), `no conversation writes: ${path}`);
    let body = {};
    if (path === "/machines") body = { machines: [{ id: "example", name: "Example computer", online: true, os: "linux" }] };
    else if (path === "/sessions") body = { sessions: [session()] };
    else if (path === "/interactions") body = { interactions: [] };
    else if (path === "/assistant") body = { machine_id: "example", main: "example-main", conversations: [], tasks: [], memory: [] };
    else if (path === "/todos") body = { todos: [] };
    else if (path === `/sessions/example/${sid}`) body = { session: session() };
    else if (path.endsWith("/items")) body = { items: path.includes("%40") || path.includes("@") ? [] : url.searchParams.has("after_seq") ? items.filter((i) => i.seq > Number(url.searchParams.get("after_seq"))) : items, has_more: false };
    else if (path.endsWith("/feed")) body = { items: [] };
    else if (path === "/extensions/ui") body = { slots: [] };
    else if (path === "/extensions") body = { extensions: [] };
    else if (path === "/managers") body = { managers: [] };
    await route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.routeWebSocket("**/api/v1/ws**", (ws) => {
    socket = ws;
    ws.send(JSON.stringify({ type: "hello", server_version: "synthetic", hub_name: "Example hub", features: [] }));
    ws.onMessage((raw) => { if (JSON.parse(String(raw)).type === "app_ping") ws.send(JSON.stringify({ type: "pong" })); });
  });
  const errors = [];
  const reportChunks = [];
  page.on("pageerror", (error) => errors.push(String(error)));
  page.on("request", (request) => { if (/\/SubagentCard-[^/]+\.js(?:\?|$)/.test(request.url())) reportChunks.push(request.url()); });
  page.setDefaultTimeout(15000);
  await page.goto(base.replace(/\/$/, "") + `/s/example/${sid}`);
  const chat = page.locator("[data-chat-scroll]");
  const summary = chat.locator("[data-work-group] > button").first();
  const card = chat.locator("[data-subagent-card]");
  const shot = async (state) => {
    await page.waitForFunction(() => [...document.querySelectorAll("[data-work-group] .grid[style]")].every((element) => !element.style.willChange));
    assert.deepEqual(errors, [], "no page exceptions");
    assert.equal(await page.locator("vite-error-overlay").count(), 0);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, "no horizontal overflow");
    await page.screenshot({ path: join(shots, `web-${width}-${theme}-${state}.png`) });
  };
  await summary.waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.dataset.textSize ?? "standard"), textSize, "configured text size is applied");
  assert.equal(await summary.getAttribute("aria-expanded"), "false");
  assert.equal(await card.count(), 0);
  assert.equal(reportChunks.length, 0, "collapsed work does not fetch the subagent report card");
  items = [...initial, progress];
  socket.send(JSON.stringify({ type: "items", machine_id: "example", session_id: sid, items: [progress] }));
  await shot("running-collapsed");
  await summary.click();
  await card.waitFor();
  assert.equal(await card.getAttribute("data-subagent-card"), "running");
  assert.equal(await card.locator("button").first().getAttribute("aria-expanded"), "false");
  await shot("running-tool-collapsed");
  await card.locator("button").first().click();
  await card.locator("[data-report-line]").waitFor();
  assert.equal(await card.locator("[data-report]").getAttribute("data-report"), "folded");
  await card.locator("[data-report-line]").click();
  await card.getByText("The fictional checks are still running.", { exact: true }).waitFor();
  assert(await page.getByText("1 subagent working", { exact: false }).isVisible(), "interim report keeps the working bar");
  await shot("running-report-expanded");
  const pointer = item(7, "tool_result", { tool_use_id: "example-launch", task: task("completed") });
  items = [...items, terminal, pointer];
  socket.send(JSON.stringify({ type: "items", machine_id: "example", session_id: sid, items: [terminal, pointer] }));
  await page.waitForFunction(() => document.querySelector("[data-subagent-card]")?.getAttribute("data-subagent-card") === "completed");
  assert.equal(await card.locator("[data-report]").count(), 2, "pointer notification adds no duplicate report");
  assert.equal(await card.locator('[data-items="report-example-4#0"] [data-report]').getAttribute("data-report"), "open");
  await card.locator("button").first().click();
  assert.equal(await card.locator("button").first().getAttribute("aria-expanded"), "false");
  await shot("completed-tool-collapsed");
  await card.locator("button").first().click();
  await card.locator('[data-items="report-example-6#0"] [data-report-line]').click();
  await card.getByText("Every fictional check passed.", { exact: true }).waitFor();
  assert.equal(await chat.locator("[data-subagent-card]").count(), 1);
  assert.equal(await chat.getByText("Async agent launched", { exact: false }).count(), 0);
  assert.equal(await page.getByText("1 subagent working", { exact: false }).count(), 0);
  await shot("completed-reports-expanded");
  await summary.click();
  await chat.locator("[data-work-detail]").waitFor({ state: "detached" });
  agentRunning = false;
  live = false;
  const answer = item(8, "text", { text: "Final result: the example checks passed." });
  items = [...items, answer];
  socket.send(JSON.stringify({ type: "items", machine_id: "example", session_id: sid, items: [answer] }));
  socket.send(JSON.stringify({ type: "sessions", machine_id: "example", sessions: [session()] }));
  await chat.getByText(answer.text, { exact: true }).waitFor();
  assert.equal(await summary.getAttribute("aria-expanded"), "false");
  await page.locator("[data-running-status]").waitFor({ state: "detached" });
  await shot("completed-collapsed");
  console.log(`PASS ${width} ${theme}: interim stays running, two reports under one collapsed tool, manual expansion, status-only pointer deduplicated`);
  if (!shared) await context.close();
}
function atNow() { return new Date(Date.parse(AT) + 10000).toISOString(); }
try {
  browser = shared ? await chromium.connectOverCDP(cdp) : await chromium.launch({ executablePath: cdp, args: ["--no-sandbox"] });
  for (const width of [1280, 390]) for (const theme of ["light", "dark"]) await verify(theme, width);
  console.log(`PASS subagent report browser verification: 4 real-app scenarios, 24 screenshots, synthetic data only${shared ? "; tabs retained" : ""}`);
} catch (error) {
  console.error("FAIL subagent report browser verification", error);
  process.exitCode = 1;
} finally {
  // For a CDP connection, close disconnects the driver; it does not close the shared Chrome.
  await browser?.close();
  clearTimeout(watchdog);
}
