// Run with MOCK_MANAGERS=1 MOCK_LEADERS=1 MOCK_BACKGROUND_SUPERVISOR=1 on the mock hub.
// node web/e2e/sidebar-chat-hierarchy.mjs <Vite loopback URL> <screenshot directory>
import assert from "node:assert/strict";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [base, output] = process.argv.slice(2);
assert(/^http:\/\/(127\.0\.0\.1|localhost):\d+$/.test(base ?? "") && output);
mkdirSync(output, { recursive: true });
const recentTitle = "A long recent chat title about planning a garden through every season of the year";
const archiveTitle = "Archived garden notes";
const scheduledTitle = "Scheduled garden report";

const browser = await chromium.launch({ channel: "chrome" });
try {
  const cases = [[240, "standard"], [284, "standard"], [480, "standard"], ["phone", "standard"], [284, "large"], ["phone", "large"]];
  for (const [size, textSize] of cases) {
    const suffix = textSize === "large" ? "-large" : "";
    for (const theme of ["light", "dark"]) {
      const phone = size === "phone";
      const context = await browser.newContext({
        viewport: { width: phone ? 390 : 1440, height: phone ? 844 : 1100 },
        hasTouch: phone, colorScheme: theme, reducedMotion: "reduce",
      });
      try {
        await context.addInitScript(({ size, theme, textSize }) => {
          localStorage.setItem("persona.language", "en");
          localStorage.setItem("persona.theme", theme);
          if (textSize === "large") localStorage.setItem("persona.text-size", textSize);
          if (size !== "phone" && size !== 284) localStorage.setItem("persona.sidebarWidth", String(size));
        }, { size, theme, textSize });
        let data;
        await context.route((url) => url.pathname === "/api/v1/assistant" && !url.searchParams.has("space"), async (route) => {
          const headers = await route.request().allHeaders();
          // This route builds a fresh synthetic response, regardless of the mock's cached real body.
          delete headers["if-none-match"];
          const response = await route.fetch({ headers });
          const body = await response.json();
          assert(body.main && body.leaders?.length && body.background_supervisor);
          const original = body.conversations.find((chat) => chat.session_id !== body.main && !body.leaders.includes(chat.session_id) && chat.session_id !== body.background_supervisor && !chat.archived);
          assert(original);
          original.title = recentTitle;
          const scheduled = { ...original, session_id: "scheduled-garden", title: scheduledTitle, origin: "task", origin_ref: "garden" };
          const archived = { ...original, session_id: "archived-garden", title: archiveTitle, archived: true };
          body.conversations.push(scheduled);
          if (new URL(route.request().url()).searchParams.has("archived")) body.conversations.push(archived);
          // The live session stream still has this chat while both assistant summaries are behind.
          const liveOnly = body.conversations.find((chat) => chat !== original && chat.session_id !== body.main
            && !body.leaders.includes(chat.session_id) && chat.session_id !== body.background_supervisor
            && chat !== scheduled && !chat.archived);
          assert(liveOnly);
          data = { main: body.main, recent: original.session_id, lead: body.leaders[0], liveOnly: liveOnly.session_id,
            background: body.background_supervisor, total: body.conversations.filter((chat) => !chat.archived).length };
          body.conversations = body.conversations.filter((chat) => chat !== liveOnly);
          await route.fulfill({ response, json: body });
        });

        const page = await context.newPage();
        page.setDefaultTimeout(12000);
        const errors = [];
        page.on("pageerror", (error) => errors.push(String(error)));
        await page.goto(`${base}/overview?token=mock`);
        const sidebar = page.locator("aside");
        const showSidebar = () => page.getByRole("button", { name: "Sidebar", exact: true }).click();
        if (phone) await showSidebar();
        const persona = sidebar.getByRole("button", { name: "Persona chats", exact: true });
        const recent = sidebar.getByRole("button", { name: "Recent", exact: true });
        const help = sidebar.getByRole("button", { name: /About.*Recent/ });
        const history = sidebar.getByRole("link", { name: /^All Persona chats/ });
        await history.waitFor();
        assert(data);
        const chat = (id) => sidebar.locator(`a[href*="/persona/${id}"]`);
        const leaf = chat(data.recent);
        const pinned = sidebar.getByRole("heading", { name: "Pinned", exact: true });
        const agents = sidebar.locator("[data-sidebar-agents]").getByText("Agents", { exact: true }).first();
        assert.equal(await history.getAttribute("href"), "/history");
        await history.locator(`[title="${data.total} unarchived Persona chats"]`).waitFor();
        assert.equal(await chat(data.main).count(), 0);
        assert.equal(await chat(data.background).count(), 0);
        assert.equal(await chat("scheduled-garden").count(), 0);
        assert.equal(await chat("archived-garden").count(), 0);
        await chat(data.lead).waitFor();

        const x = async (locator) => (await locator.boundingBox()).x;
        const unit = await sidebar.evaluate((el) => parseFloat(getComputedStyle(el).getPropertyValue("--sidebar-indent")));
        assert.equal(await x(agents), await x(persona), "peer headings align");
        assert.equal(await x(recent) - await x(persona), unit, "Recent is one level in");
        assert.equal(await x(pinned), await x(recent), "Pinned and Recent are peers");
        assert.equal(await x(history) + 8, await x(recent), "history's icon aligns with subgroup headings");
        assert.equal(await x(leaf) + 8 - await x(recent), unit, "Recent leaves are another level in");
        const title = leaf.getByText(recentTitle, { exact: true });
        await title.waitFor();
        const fit = await title.evaluate((el) => ({
          truncated: el.scrollWidth > el.clientWidth,
          ellipsis: getComputedStyle(el).textOverflow === "ellipsis",
          rowFits: el.closest("a").getBoundingClientRect().right <= el.closest("aside").getBoundingClientRect().right,
        }));
        assert(fit.ellipsis && fit.rowFits && (size === 480 || fit.truncated), JSON.stringify(fit));

        await recent.scrollIntoViewIfNeeded();
        const width = Math.round((await sidebar.boundingBox()).width);
        if (!phone) assert.equal(width + 1, size, "the column includes its 1px divider");
        await sidebar.screenshot({ path: join(output, `sidebar-${size}-${theme}${suffix}.png`), animations: "disabled" });

        if (phone) {
          assert((await recent.boundingBox()).height >= 44);
          assert((await help.boundingBox()).width >= 44);
          await help.tap();
          await page.getByText(/^Chats you used most recently/).waitFor();
          await recent.tap();
        } else {
          await recent.focus();
          await page.keyboard.press("Space");
        }
        assert.equal(await recent.getAttribute("aria-expanded"), "false");
        await history.waitFor();
        assert.equal(await leaf.count() === 0 || await leaf.evaluate((el) => !!el.closest("[inert]")), true);
        await recent.click();
        await leaf.waitFor();
        await persona.click();
        assert.equal(await persona.getAttribute("aria-expanded"), "false");
        await history.waitFor({ state: "hidden" });
        await persona.click();
        await history.waitFor();
        if (phone) await history.tap();
        else {
          await history.focus();
          await page.keyboard.press("Enter");
        }
        await page.waitForURL("**/history");
        const heading = page.getByRole("heading", { name: "All Persona chats", exact: true });
        await heading.waitFor();
        const total = page.getByText(`${data.total} unarchived Persona chats`, { exact: true }).last();
        assert.equal(await total.textContent(), `${data.total} unarchived Persona chats`);
        const archived = page.getByRole("button", { name: "Archived (1)", exact: true });
        await archived.waitFor();
        assert.equal(await archived.getAttribute("aria-expanded"), "false");
        await page.getByText(scheduledTitle, { exact: false }).first().waitFor();
        assert.equal(await page.getByRole("main").locator(`a[href*="/persona/${data.main}"]`).count(), 1);
        assert.equal(await page.getByRole("main").locator(`a[href*="/persona/${data.lead}"]`).count(), 1);
        assert.equal(await page.getByRole("main").locator(`a[href*="/persona/${data.recent}"]`).count(), 1);
        assert.equal(await page.getByRole("main").locator(`a[href*="/persona/${data.liveOnly}"]`).count(), 1);

        await archived.click();
        await page.getByText(archiveTitle, { exact: true }).waitFor();
        const search = page.getByRole("searchbox").or(page.getByRole("textbox", { name: "Search chats", exact: true }));
        await search.fill("Scheduled garden report");
        await archived.waitFor({ state: "hidden" });
        assert.equal(await total.textContent(), `${data.total} unarchived Persona chats`);
        await search.clear();
        await archived.waitFor();
        const historyHelp = page.getByRole("button", { name: /About.*All Persona chats/ });
        await historyHelp.click();
        await page.getByText(/^This page lists chats with Persona/).waitFor();
        if (size === 284 || phone) {
          await page.screenshot({ path: join(output, `history-${size}-${theme}${suffix}.png`), animations: "disabled" });
        }
        if (phone) {
          await historyHelp.click();
          await showSidebar();
        }
        assert.equal(await history.getAttribute("aria-current"), "page");
        assert.deepEqual(errors, []);
        console.log(`PASS ${size} sidebar ${width}px ${theme} ${textSize}: hierarchy, fold, navigation, membership and counts`);
      } finally {
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
}
