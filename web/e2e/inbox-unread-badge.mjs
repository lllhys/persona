// Real app, synthetic hub only. No user profile or live hub writes.
import assert from "node:assert/strict";
import { parseArgs } from "node:util";
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";
const { values } = parseArgs({ options: { origin: { type: "string" }, cdp: { type: "string" }, out: { type: "string" }, help: { type: "boolean", short: "h" } } });
if (values.help || !values.origin || !values.out || !values.cdp) {
  console.log("Usage: node web/e2e/inbox-unread-badge.mjs --origin <owned-web-origin> --cdp <browser-endpoint> --out <evidence-directory>");
  process.exit(values.help ? 0 : 2);
}
const origin = new URL(values.origin);
assert(origin.protocol === "http:" && !origin.username && !origin.password, "owned HTTP test server without credentials required");
mkdirSync(values.out, { recursive: true });
const watchdog = setTimeout(() => { console.error("TIMEOUT inbox badge verification"); process.exit(124); }, 180_000);
const browser = await chromium.connectOverCDP(values.cdp);
const results = [];
try {
  for (const theme of ["light", "dark"]) {
    const context = await browser.newContext({ viewport: { width: 1280, height: 900 }, colorScheme: theme });
    try {
      await context.addInitScript((theme) => {
        localStorage.setItem("persona.token", "synthetic-demo-token");
        localStorage.setItem("persona.language", "en");
        localStorage.setItem("persona.theme", theme);
      }, theme);
      const make = (id, read, done = false) => ({ id, source: "mail", account: "example", account_name: "Example mail", kind: "mail", title: id,
        preview: "Synthetic message", tier: "action", read, count: 1, updated_at: "2026-11-10T08:00:00Z",
        ...(done ? { done_at: "2026-11-10T09:00:00Z" } : {}), mail: { account_id: "example", uids: [1] } });
      let items = [make("Unread pending example", false), make("Read pending example", true), make("Read completed example", true, true)];
      let socket;
      const writes = [];
      await context.route("**/api/v1/**", async (route) => {
        const request = route.request();
        const path = new URL(request.url()).pathname.replace(/^\/api\/v1/, "");
        let body = {};
        if (request.method() !== "GET") {
          const data = request.postDataJSON();
          writes.push({ path, method: request.method(), data });
          assert.equal(path, "/mail/messages/example/1/flags", "read must never file or hide an item");
          assert.equal(data.seen, true);
          items = items.map((it) => ({ ...it, read: true }));
          body = { ok: true };
        } else if (path === "/inbox") body = { items };
        else if (path.startsWith("/mail/messages/")) body = { message: { uid: 1, subject: "Unread pending example", text: "Synthetic message body", seen: false, from: { email: "sender@example.com" } } };
        else if (path === "/mail/accounts") body = { accounts: [] };
        else if (path === "/machines") body = { machines: [] };
        else if (path === "/sessions") body = { sessions: [] };
        else if (path === "/interactions") body = { interactions: [] };
        else if (path === "/managers") body = { default: "assistant", managers: [] };
        else if (path === "/extensions/ui") body = { slots: [] };
        else if (path === "/extensions") body = { extensions: [] };
        else if (path === "/todos") body = { todos: [] };
        await route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
        if (request.method() !== "GET") socket?.send(JSON.stringify({ type: "inbox" }));
      });
      await context.routeWebSocket("**/api/v1/ws**", (ws) => {
        socket = ws;
        ws.send(JSON.stringify({ type: "hello", server_version: "synthetic", hub_name: "Example hub", features: [] }));
        ws.onMessage((raw) => { const m = JSON.parse(String(raw)); if (m.type === "app_ping") ws.send(JSON.stringify({ type: "pong" })); });
      });
      const page = await context.newPage();
      page.setDefaultTimeout(15_000);
      await page.goto(new URL("/inbox", origin).href);
      const nav = page.locator('a[href$="/inbox"]').first();
      await nav.getByText("1", { exact: true }).waitFor();
      await page.getByText("Read pending example", { exact: true }).first().waitFor();
      await page.screenshot({ path: join(values.out, `${theme}-mixed.png`) });
      const readResponse = page.waitForResponse((r) => r.url().endsWith("/flags") && r.request().method() === "POST");
      await page.getByText("Unread pending example", { exact: true }).first().click();
      await page.waitForFunction(() => location.pathname.includes("/inbox/"));
      await readResponse;
      await page.waitForFunction(() => [...document.querySelectorAll('a[href$="/inbox"]')].some((el) => el.textContent.trim() === "Inbox"));
      assert.equal(await nav.innerText(), "Inbox");
      assert.equal(items.filter((it) => !it.done_at).length, 2);
      assert.equal(writes.length, 1);
      await page.screenshot({ path: join(values.out, `${theme}-read-zero.png`) });
      await page.reload();
      await page.getByText("Read pending example", { exact: true }).first().waitFor();
      assert.equal(await nav.innerText(), "Inbox");
      assert.equal(writes.length, 1, "reload must not file anything");
      await page.screenshot({ path: join(values.out, `${theme}-reload.png`) });
      results.push({ theme, badgeBefore: 1, badgeAfterRead: 0, badgeAfterReload: 0, pendingItems: 2, completionWrites: 0, writes });
    } finally { await context.close(); }
  }
  writeFileSync(join(values.out, "results.json"), JSON.stringify(results, null, 2) + "\n");
  console.log(JSON.stringify({ verdict: "PASS", results }));
} finally { clearTimeout(watchdog); await browser.close(); }
