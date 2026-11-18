// Real Agents UI with synthetic HTTP/WebSocket traffic: no user's hub or Paseo is contacted.
// node web/e2e/paseo-workspace-pins.mjs <loopback web origin> <artifacts> [Chromium executable]
import assert from "node:assert/strict";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [web, output, executable] = process.argv.slice(2);
assert(/^http:\/\/(?:127\.0\.0\.1|localhost):\d+$/.test(web ?? "") && output,
  "Provide an owned loopback web origin and an artifact directory");
mkdirSync(output, { recursive: true });
const watchdog = setTimeout(() => { console.error("TIMEOUT workspace pin browser verification (180s)"); process.exit(124); }, 180_000);
const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });
const results = [];
try {
  for (const width of [1440, 390]) for (const theme of ["light", "dark"]) for (const textSize of ["standard", "large"]) {
    const tag = `${width}-${theme}-${textSize}`;
    const context = await browser.newContext({ viewport: { width, height: 900 }, hasTouch: width === 390,
      colorScheme: theme, reducedMotion: "reduce", serviceWorkers: "block" });
    await context.addInitScript(({ theme, textSize }) => {
      localStorage.setItem("persona.token", "synthetic-pins-token");
      localStorage.setItem("persona.language", "en");
      localStorage.setItem("persona.theme", theme);
      localStorage.setItem("persona.text-size", textSize);
      localStorage.setItem("persona.agentGrouping", "repo");
    }, { theme, textSize });
    const machine = { id: "example-computer", name: "Example computer", online: true };
    const workspace = (id, title, extra = {}) => ({ id, machine_id: machine.id, source: "paseo", source_id: id,
      title, cwd: "/projects/example", project_id: "zeta", project_name: "Zeta project", available: true, ...extra });
    let inventory = [workspace("main", "Main workspace", { is_main: true }),
      workspace("pinned-empty", "Pinned empty workspace", { pinned_at: "2026-11-16T10:00:00Z" }),
      workspace("pinned-saved", "Saved workspace", { pinned_at: "2026-11-15T10:00:00Z" }),
      workspace("ordinary", "Ordinary workspace", { project_id: "alpha", project_name: "Alpha project" })];
    const session = { machine_id: machine.id, session_id: "local-pin", title: "Keep this Agent", cwd: "/projects/example",
      space_kind: "paseo", host: "paseo", workspace_id: "main", space_id: "zeta", source_kind: "runtime", state: "idle", status: "idle" };
    const saved = { ...session, session_id: "saved", title: "Continue the saved conversation", workspace_id: "pinned-saved", state: "offline", status: "offline", updated_at: "2020-01-01T00:00:00Z" };
    const writes = [], errors = [];
    await context.route("**/*", async (route) => {
      const request = route.request(), url = new URL(request.url());
      if (url.origin !== web) return route.abort();
      if (!url.pathname.startsWith("/api/")) return route.continue();
      if (request.method() !== "GET") writes.push({ method: request.method(), path: url.pathname });
      const data = {
        "/machines": { machines: [machine] }, "/sessions": { sessions: [session, saved] },
        "/workspaces": { workspaces: inventory }, "/pins": { spaces: [], sessions: [`${machine.id}/${session.session_id}`] },
        "/mutes": { spaces: [], sessions: [] }, "/interactions": { interactions: [] },
        "/assistant": { machine_id: machine.id, conversations: [] }, "/todos": { todos: [] },
        "/agents/sessions": { sessions: [] }, "/managers": { managers: [], default_id: "" },
        "/extensions": { extensions: [] }, "/extensions/ui": { slots: [] },
      };
      return route.fulfill({ json: data[url.pathname.replace(/^\/api\/v1/, "")] ?? {} });
    });
    await context.routeWebSocket("**/api/v1/ws**", (socket) => {
      socket.send(JSON.stringify({ type: "hello", server_version: "synthetic", protocol: 1, features: [], hub_name: "Example hub" }));
      socket.onMessage((raw) => {
        const message = JSON.parse(String(raw));
        if (message.type === "app_ping") socket.send(JSON.stringify({ type: "pong", id: message.id }));
        assert(!/send|resume/.test(String(message.type)), "No Agent action is sent");
      });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(12_000);
    page.on("pageerror", (error) => errors.push(String(error)));
    await page.goto(`${web}/agents`);
    const tree = width === 390 ? page.getByRole("main") : page.locator("aside:visible");
    const pinned = tree.getByRole("button", { name: "Pinned empty workspace", exact: true });
    await pinned.waitFor();
    const order = () => tree.locator("[data-workspace-key]").evaluateAll((elements) => elements.map((el) => JSON.parse(el.dataset.workspaceKey)[4]));
    assert.deepEqual(await order(), ["pinned-empty", "pinned-saved", "main", "ordinary"]);
    assert.equal(await tree.locator('[data-workspace-key] [title="Pinned"]').count(), 2);
    assert.equal(await tree.locator("[data-workspace-key]").getByRole("link", { name: /Continue the saved conversation/ }).count(), 1);
    assert.equal(await tree.getByRole("link", { name: /Keep this Agent/ }).count(), 1, "Persona session pin appears once");
    await pinned.click();
    assert.equal(await pinned.getAttribute("aria-pressed"), "true");
    assert.equal(await pinned.getAttribute("aria-expanded"), "false");
    await page.evaluate(() => document.fonts.ready);
    await page.screenshot({ path: join(output, `${tag}-pinned.png`) });
    inventory = inventory.map((w) => ({ ...w, pinned_at: undefined }));
    await page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
    await page.waitForFunction(() => document.querySelectorAll('[data-workspace-key] [title="Pinned"]').length === 0);
    assert.deepEqual(await order(), ["ordinary", "main", "pinned-empty", "pinned-saved"]);
    assert.equal(await tree.locator("[data-workspace-key]").getByRole("link", { name: /Continue the saved conversation/ }).count(), 0);
    assert.equal(await pinned.getAttribute("aria-pressed"), "true", "Selection survives unpin and reorder");
    assert.equal(await pinned.getAttribute("aria-expanded"), "false", "The workspace keeps its disclosure");
    assert.equal(await tree.getByRole("link", { name: /Keep this Agent/ }).count(), 1);
    assert.deepEqual(writes, [], "Synchronization never writes Persona preferences or to Paseo");
    assert.deepEqual(errors, []);
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false, "No page overflow");
    await page.screenshot({ path: join(output, `${tag}-unpinned.png`) });
    results.push({ tag, passed: true, emptyWorkspace: true, stableSelection: true, writes: writes.length });
    await context.close();
    console.log(`PASS workspace pins ${tag}`);
  }
  writeFileSync(join(output, "results.json"), JSON.stringify(results, null, 2));
  console.log("PASS workspace pin browser verification");
} finally {
  await browser.close();
  clearTimeout(watchdog);
}
