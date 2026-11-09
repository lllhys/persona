// Actual Settings editor against the existing loopback mock hub, with synthetic API fixtures.
// Usage: node web/e2e/knowledge-selection.mjs <mockWebOrigin> <artifactDir>
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";

const [address, artifacts, ...extra] = process.argv.slice(2);
assert.ok(address && artifacts && !extra.length, "Supply the mock web origin and artifact directory");
const base = new URL(address);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname), "Use a loopback mock server");
assert.ok(["http:", "https:"].includes(base.protocol) && !base.username && !base.password && base.pathname === "/" && !base.search && !base.hash, "Use an origin without credentials or a route");
assert.equal((await (await fetch(new URL("/api/v1/health", base), { signal: AbortSignal.timeout(5000) })).json()).version, "mock");
const output = resolve(artifacts);
await mkdir(output, { recursive: true });
const messages = ["mentions", "mine", "dm", "group", "bot", "official", "muted", "collapsed", "done"];
const docs = ["mentions", "comment_mentions", "authored", "viewed"];
const fixture = () => ({
  version: 1,
  messages: { order: [["mentions", "mine"], ...messages.slice(2).map((n) => [n])], rules: Object.fromEntries(messages.map((n) => [n, { on: true, days: null, attachment_days: null, ...(n === "mentions" ? { include_all: false } : n === "mine" ? { context: 0 } : {}) }])) },
  docs: { order: [["mentions", "comment_mentions"], ["authored"], ["viewed"]], rules: Object.fromEntries(docs.map((n) => [n, { on: true, days: null }])) },
  items: [{ source: "example-chat", kind: "chat", id: "chat/one", name: "Project notes", read: true, store: false, days: null }],
  history: { "example-chat": { back_days: 90 }, "example-docs": { back_days: null } },
});
const machine = { id: "example-computer", name: "Example computer", online: true };
const result = { status: "running", checks: [], screenshots: [], runs: [] };
const browser = await chromium.launch({ channel: "chrome", headless: true });
const timeout = setTimeout(() => { console.error("FAIL knowledge selection acceptance timed out"); process.exit(1); }, 240_000);

async function open(lang, theme, mode = "normal", phone = false) {
  const strings = JSON.parse(await readFile(new URL(`../src/i18n/locales/${lang}/settings-knowledge.json`, import.meta.url), "utf8"));
  const nav = JSON.parse(await readFile(new URL(`../src/i18n/locales/${lang}/settings-nav.json`, import.meta.url), "utf8"));
  const run = { lang, theme, mode, phone, reads: [], writes: [], errors: [], blocked: [], mockHellos: 0 };
  result.runs.push(run);
  const context = await browser.newContext({ viewport: { width: phone ? 390 : 1280, height: 900 }, colorScheme: theme, deviceScaleFactor: 1 });
  await context.addInitScript(({ lang, theme }) => {
    localStorage.setItem("persona.token", "fixture-token");
    localStorage.setItem("persona.language", lang);
    localStorage.setItem("persona.theme", theme);
  }, { lang, theme });
  let document = fixture(), failRead = mode === "read-error", releaseRead;
  let socket;
  await context.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url());
    if (url.origin !== base.origin) { run.blocked.push(url.pathname); return route.abort(); }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    const path = url.pathname.replace(/^\/api\/v1/, "");
    const manager = request.headers()["x-persona-manager"];
    if (path === "/kb/selection") {
      assert.ok(["assistant", "second"].includes(manager), "Every selection read/write names its configuration manager");
      if (request.method() === "PUT") {
        const body = request.postDataJSON();
        run.writes.push({ path, manager, body });
        if (mode === "write-error") return route.fulfill({ status: 403, json: { error: "Read-only device" } });
        document = body;
        socket?.send(JSON.stringify({ type: "kb_selection", manager }));
        return route.fulfill({ json: document });
      }
      run.reads.push({ path, manager });
      if (failRead) return route.fulfill({ status: 503, json: { error: "Selection unavailable" } });
      if (mode === "late-manager" && manager === "assistant") await new Promise((done) => { releaseRead = done; });
      const data = structuredClone(document);
      if (manager === "second") data.messages.rules.dm.days = 42;
      return route.fulfill({ json: data });
    }
    if (path.startsWith("/kb/") || path === "/connectors") {
      assert.ok(manager, "Every item picker request names its manager");
      run.reads.push({ path, manager });
      if (path === "/connectors") return route.fulfill({ json: { connectors: [
        { id: "example-chat", name: "Example chats", manager, capabilities: ["history"] },
        { id: "wrong-manager", name: "Other manager", manager: "absent", capabilities: ["history"] },
      ] } });
      if (path === "/kb/selection/candidates") return route.fulfill({ json: { cached: true, source: "example-chat", chats: [{ id: "chat/one", name: "Project notes", source: "example-chat" }, { id: "chat/two", name: "Project notes", source: "example-chat" }] } });
      if (path === "/kb/docs") return route.fulfill({ json: { items: [{ kind: "docs", id: "sanitized-filename", name: "Project notes" }] } });
      if (path === "/kb/docs/sanitized-filename") return route.fulfill({ json: { detail: { id: "sanitized-filename", meta: { id: "original/token:exact", source: "example-docs" } } } });
    }
    assert.ok(["GET", "HEAD"].includes(request.method()), `Unexpected write ${path}`);
    const data = {
      "/sessions": { sessions: [], total: 0, has_more: false, revision: "fixture" },
      "/machines": { machines: [machine] }, "/interactions": { interactions: [] },
      "/assistant": { machine_id: machine.id, conversations: [] }, "/todos": { todos: [] },
      "/mutes": { spaces: [], sessions: [], allowed: [] }, "/pins": { spaces: [], sessions: [], sections: [] },
      "/agents/sessions": { sessions: [] }, "/agents/prefs": {},
      "/managers": { managers: [{ id: "assistant", name: "Primary", builtin: true, online: true }, { id: "second", name: "Second", online: true }], default: "assistant" },
      "/personas": { personas: [] }, "/assistant/personas": { personas: [] }, "/workspaces": { workspaces: [] }, "/extensions": { extensions: [] },
    };
    return route.fulfill({ json: data[path] ?? {} });
  });
  await context.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/, (ws) => {
    socket = ws;
    const upstream = ws.connectToServer();
    upstream.onMessage((message) => {
      const event = JSON.parse(String(message));
      if (event.type !== "hello") return;
      assert.equal(event.server_version, "mock"); run.mockHellos++;
      ws.send(JSON.stringify({ type: "hello", server_version: "mock", protocol: 1, features: ["managers"], hub_name: "Example hub" }));
      ws.send(JSON.stringify({ type: "machines", machines: [machine] }));
    });
    ws.onMessage((message) => { const event = JSON.parse(String(message)); if (event.type === "app_ping") ws.send(JSON.stringify({ type: "pong", id: event.id })); });
  });
  const page = await context.newPage(); page.setDefaultTimeout(10_000);
  page.on("pageerror", (e) => run.errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error" && !(mode.endsWith("error") && /Failed to load resource.*(?:403|503)/.test(m.text()))) run.errors.push(m.text()); });
  await page.goto(`${base.origin}/settings?tab=knowledge`);
  const form = page.getByRole("form", { name: strings.title });
  await form.waitFor();
  const dm = () => page.locator('[data-selection-rule="messages/dm"] input').first();
  const save = () => page.getByRole("button", { name: strings.save, exact: true });
  const shot = async (state) => {
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(350); // finite presentation transitions
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "No horizontal overflow");
    assert.equal(await page.locator("vite-error-overlay").count(), 0);
    const file = `${lang}-${theme}${phone ? "-phone" : ""}-${state}.png`;
    await page.screenshot({ path: join(output, file) }); result.screenshots.push(file);
  };
  return { context, page, form, run, strings, nav, dm, save, shot, setRead: (fails) => { failRead = fails; }, release: () => releaseRead?.(), emit: (event) => socket.send(JSON.stringify(event)) };
}

try {
  for (const lang of ["en", "zh-Hans", "ja"]) for (const theme of ["light", "dark"]) {
    const x = await open(lang, theme);
    const { page, strings: s, dm, save, run } = x;
    await dm().waitFor();
    await page.getByRole("button", { name: s.add_chat, exact: true }).click();
    const source = page.getByLabel(s.source, { exact: true });
    await source.locator('option[value="example-chat"]').waitFor({ state: "attached" });
    assert.deepEqual(await source.locator("option").evaluateAll((els) => els.map((e) => e.value)), ["", "example-chat"]);
    await source.selectOption("example-chat");
    await page.getByRole("button", { name: s.added, exact: true }).waitFor();
    await page.getByRole("button", { name: s.add, exact: true }).click();
    await page.getByRole("button", { name: s.add_doc, exact: true }).click();
    await page.getByRole("button", { name: s.add, exact: true }).click();
    await page.getByText("original/token:exact", { exact: false }).waitFor();
    await page.locator('[data-selection-item]').first().getByRole("switch", { name: s.store, exact: true }).click();
    await page.locator('[data-selection-item]').last().getByRole("switch", { name: s.store, exact: true }).click();
    await page.getByText(s.store_all, { exact: true }).waitFor();
    await page.getByText(s.store_all_doc, { exact: true }).waitFor();
    await dm().fill("30");
    await page.locator('[data-selection-rule="messages/mentions"]').getByRole("button", { name: s.split, exact: true }).click();
    await page.locator('[data-selection-rule="messages/mentions"]').getByRole("button", { name: s.join, exact: true }).click();
    await save().click();
    await page.getByRole("status").filter({ hasText: s.saved }).waitFor();
    await page.getByRole("alert").filter({ hasText: s.external_change }).waitFor({ state: "hidden" });
    const expected = fixture(); expected.messages.rules.dm.days = 30; expected.messages.order[0] = ["mine", "mentions"];
    expected.items.push({ source: "example-chat", kind: "chat", id: "chat/two", name: "Project notes", read: true, store: false, days: null });
    expected.items.push({ source: "example-docs", kind: "doc", id: "original/token:exact", name: "Project notes", read: true, store: false, days: null });
    expected.items[0].store = true;
    expected.items.at(-1).store = true;
    assert.deepEqual(run.writes.map((w) => w.body), [expected]);
    await page.locator('[data-settings-group="knowledge-items"]').scrollIntoViewIfNeeded(); await x.shot("items");
    await page.locator('[data-settings-group="knowledge-docs"]').scrollIntoViewIfNeeded(); await x.shot("document-rules");
    await page.getByRole("button", { name: s.reload, exact: true }).click(); await dm().waitFor();
    assert.equal(await dm().inputValue(), "30");
    await dm().fill("31"); x.emit({ type: "kb_selection", manager: "assistant" });
    await page.getByRole("alert").filter({ hasText: s.external_change }).waitFor(); assert.equal(await save().isDisabled(), true);
    assert.ok(run.mockHellos > 0); assert.deepEqual(run.blocked, []); assert.deepEqual(run.errors, []);
    result.checks.push(`${lang}/${theme}: real Settings, named pickers, original identity, whole-document round-trip, priorities, observable stale edit`);
    await x.context.close();
  }
  for (const mode of ["read-error", "write-error", "late-manager"]) {
    const x = await open("en", "light", mode), { page, strings: s } = x;
    if (mode === "read-error") {
      await page.getByText(/Selection unavailable/).waitFor(); assert.equal(await x.save().count(), 0);
      x.setRead(false); await page.getByRole("button", { name: s.reload, exact: true }).click(); await x.dm().waitFor();
      assert.equal(await x.save().isDisabled(), true); assert.equal(x.run.writes.length, 0);
    } else if (mode === "write-error") {
      await x.dm().fill("7"); await x.save().click(); await page.getByText(/Read-only device/).waitFor();
      assert.equal(await x.dm().inputValue(), "7"); assert.equal(await x.save().isDisabled(), false);
      assert.equal(await page.getByRole("status").filter({ hasText: s.saved }).count(), 0);
    } else {
      await page.locator("select").first().selectOption("second"); await x.dm().waitFor(); assert.equal(await x.dm().inputValue(), "42");
      x.release(); await page.waitForTimeout(100); assert.equal(await x.dm().inputValue(), "42");
    }
    result.checks.push(`${mode}: negative control reached`); assert.deepEqual(x.run.errors, []); await x.context.close();
  }
  const phone = await open("ja", "dark", "normal", true); await phone.dm().waitFor(); await phone.shot("editor"); await phone.context.close();
  result.status = "passed";
} catch (e) {
  result.status = "failed"; result.failure = e.stack; process.exitCode = 1;
  const page = browser.contexts().at(-1)?.pages().at(-1);
  if (page) {
    await page.screenshot({ path: join(output, "failure.png") }).catch(() => {});
    await writeFile(join(output, "failure.txt"), await page.locator("body").innerText().catch(() => "Page unavailable"));
  }
}
finally { clearTimeout(timeout); await browser.close(); await writeFile(join(output, "results.json"), `${JSON.stringify(result, null, 2)}\n`); }
console.log(`${result.status === "passed" ? "PASS" : "FAIL"} knowledge selection: ${result.checks.length} checks, ${result.screenshots.length} screenshots`);
