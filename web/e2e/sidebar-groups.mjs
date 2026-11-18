// The sidebar's groups (#9071) in a real browser against the mock hub, in Simplified Chinese: extensions' nav_pages name
// 工作台 / 资产 / 家里 and their pages come in under those headings; Settings › General › Sidebar pages makes 生活 (first
// named "Life", then renamed), moves built-in and extension pages between groups (the user's choice over the
// manifest's), moves Pages to the end, and removes a group with Undo, no page lost; a second browser (another device)
// draws the same arrangement from the hub. `--old-hub`: a hub without the route keeps the arrangement in the browser.
// Light and dark screenshots of the sidebar and of the settings editor at 1440 × 900 and 390 × 844.
//
// Start the mock with MOCK_EXTENSIONS=1 MOCK_EXTENSION_COLUMNS=1 MOCK_SIDEBAR_GROUPS=1 (and MOCK_NAVIGATION=off for
// --old-hub), and Vite (or `vite preview`) pointing at it (PERSONA_HUB_URL). The run sets the mock's arrangement.
// node web/e2e/sidebar-groups.mjs <loopback web URL> <artifact directory> [--old-hub] [Chromium executable]
import { mkdirSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const args = process.argv.slice(2);
const oldHub = args.includes("--old-hub");
const [base, shots, executable] = args.filter((a) => a !== "--old-hub");
if (!base || !shots || !/^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?\/?$/.test(base)) {
  console.error("usage: node web/e2e/sidebar-groups.mjs <loopback web URL> <artifact directory> [--old-hub] [Chromium executable]");
  process.exit(2);
}
mkdirSync(shots, { recursive: true });
const watchdog = setTimeout(() => {
  console.error("RESULT: sidebar-groups timed out (300s)");
  process.exit(124);
}, 300_000);
const root = base.replace(/\/$/, "");
const results = [];
const check = (name, ok, detail) => {
  results.push({ name, ok: Boolean(ok), detail });
  console.log(`${ok ? "ok  " : "FAIL"} ${name}${detail === undefined ? "" : ` ${JSON.stringify(detail)}`}`);
};

const browser = await chromium.launch(executable ? { executablePath: executable } : { channel: "chrome" });

async function device({ width, height, theme = "light" }) {
  const context = await browser.newContext({ viewport: { width, height }, colorScheme: theme, deviceScaleFactor: 2, serviceWorkers: "block", hasTouch: width < 640 });
  await context.addInitScript((theme) => {
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("persona.language", "zh-Hans");
  }, theme);
  await context.route("**/*", (route) => (new URL(route.request().url()).origin === new URL(root).origin ? route.continue() : route.abort()));
  const p = await context.newPage();
  await p.goto(`${root}/?token=mock`);
  await p.waitForFunction(() => localStorage.getItem("persona.token"));
  await p.waitForLoadState("networkidle");
  return { context, p };
}

const navigation = (p, method, body) =>
  p.evaluate(async ([method, body]) => {
    const r = await fetch("/api/v1/navigation", { method, headers: { Authorization: `Bearer ${localStorage.getItem("persona.token")}`, "Content-Type": "application/json" }, ...(body ? { body: JSON.stringify(body) } : {}) });
    return r.status === 200 ? r.json() : { status: r.status };
  }, [method, body]);

/** The wide sidebar's (or the drawer's) groups as drawn: heading, then the pages' names under it. */
const sidebarGroups = (p) =>
  p.evaluate(() =>
    [...document.querySelectorAll("[data-sidebar-column] [data-sidebar-group-heading]")]
      .map((h) => [h.textContent.trim(), [...(h.parentElement?.querySelectorAll("a") ?? [])].map((a) => a.textContent.trim().replace(/[\d/]+$/, ""))])
      .filter((_, i, all) => i < all.findIndex(([title]) => title.startsWith("Persona"))),
  );

async function editor(p, width) {
  await p.goto(root + (width < 640 ? "/settings?tab=general" : "/settings"));
  const group = p.locator('[data-settings-group="sidebar-pages"]');
  await group.waitFor({ timeout: 20_000 });
  return group;
}
const moveTo = (group, page, to) => group.locator(`li[data-page="${page}"] select`).selectOption({ label: to });
async function newGroup(p, group, name) {
  await group.getByRole("button", { name: "新建分组" }).click();
  await group.getByLabel("分组名称").fill(name);
  await group.getByRole("button", { name: "保存" }).click();
  await p.waitForFunction((n) => [...document.querySelectorAll('[data-settings-group="sidebar-pages"] h3')].some((h) => h.textContent === n), name);
}

let failed = false;
try {
  if (oldHub) {
    const { context, p } = await device({ width: 1440, height: 900 });
    check("the hub has no navigation route", (await navigation(p, "GET")).status === 404);
    const group = await editor(p, 1440);
    await newGroup(p, group, "生活");
    await moveTo(group, "health", "生活");
    check("the arrangement stays in this browser", JSON.parse(await p.evaluate(() => localStorage.getItem("sidebar.pages"))).assign?.health?.startsWith("u:"));
    check("the setting says so", (await group.textContent()).includes("只存在这个浏览器里"));
    await p.goto(`${root}/`);
    await p.waitForLoadState("networkidle");
    const drawn = await sidebarGroups(p);
    check("the sidebar draws it after a reload", drawn.some(([t, pages]) => t === "生活" && pages.includes("健康")), drawn);
    await context.close();
  } else {
    const laptop = await device({ width: 1440, height: 900 });
    const { p } = laptop;
    await navigation(p, "PUT", { pages: { order: [], shown: [], hidden: [] } });
    await p.reload();
    await p.waitForLoadState("networkidle");
    let drawn = await sidebarGroups(p);
    check("an extension's nav_page comes in under the group it names", drawn.some(([t, pages]) => t === "工作台" && pages.includes("Today")), drawn);

    const group = await editor(p, 1440);
    // the extensions' pages past the first three wait under More: put them in the list
    // (pages switched on by hand count toward the three, so every extension page is switched on, the ones in the list too)
    for (let pass = 0; pass < 2; pass++) {
      for (const on of await group.locator('li[data-page^="p:"] [role="switch"]').all()) {
        if ((await on.getAttribute("aria-checked")) !== "true") await on.click();
      }
    }
    await newGroup(p, group, "Life");
    await group.getByRole("button", { name: "重命名Life" }).click();
    await group.getByLabel("分组名称").fill("生活");
    await group.getByRole("button", { name: "保存" }).click();
    for (const [page, to] of [["home", "工作台"], ["inbox", "工作台"], ["todos", "工作台"], ["health", "生活"], ["routines", "生活"], ["p:example-console/calendar", "生活"], ["kb", "资产"], ["machines", "家里"]]) {
      await moveTo(group, page, to);
    }
    // Today first in 工作台
    const up = group.locator('li[data-page="p:example-today/today"]').getByRole("button", { name: "把「Today」上移" });
    while (await up.isEnabled()) await up.click();
    // 生活 right after 工作台, and Pages to the end
    for (let i = 0; i < 2; i++) await group.locator(`[data-page-group^="u:"]`).first().getByRole("button", { name: "把「生活」上移" }).click();
    for (let i = 0; i < 4; i++) await group.locator('[data-page-group=""]').getByRole("button", { name: "把「页面」下移" }).click();
    // a group made and removed: its page goes back, and Undo brings the group back
    await newGroup(p, group, "临时");
    await moveTo(group, "terminal", "临时");
    await group.getByRole("button", { name: "移除临时" }).click();
    await p.getByRole("button", { name: "撤销" }).waitFor();
    const afterRemove = await p.evaluate(() => JSON.parse(localStorage.getItem("sidebar.pages")));
    check("removing a group gives its page back", !Object.values(afterRemove.assign ?? {}).some((g) => afterRemove.groups.every((x) => x.id !== g)) && !afterRemove.groups.some((g) => g.title === "临时"));
    await p.getByRole("button", { name: "撤销" }).click();
    await p.waitForFunction(() => JSON.parse(localStorage.getItem("sidebar.pages")).groups.some((g) => g.title === "临时"));
    check("Undo brings the removed group back", true);
    await group.getByRole("button", { name: "移除临时" }).click();
    await p.waitForFunction(() => !JSON.parse(localStorage.getItem("sidebar.pages")).groups.some((g) => g.title === "临时"));

    await p.goto(`${root}/`);
    await p.waitForLoadState("networkidle");
    drawn = await sidebarGroups(p);
    check("the groups in the user's order: 工作台, 生活, 资产, 家里, then Pages", JSON.stringify(drawn.map(([t]) => t)) === JSON.stringify(["工作台", "生活", "资产", "家里", "页面"]), drawn);
    const under = Object.fromEntries(drawn);
    check("built-in and extension pages mixed in the groups, Today first", under["工作台"]?.[0] === "Today" && under["工作台"]?.includes("总览") && under["工作台"]?.includes("Today") && under["生活"]?.includes("健康"), under);
    check("the user's choice over the manifest's: Calendar in 生活", under["生活"]?.includes("Calendar") && !under["工作台"]?.includes("Calendar"), under);
    check("no page lost", ["总览", "收件箱", "健康", "Today", "Lark", "Calendar", "Assets", "Home devices", "设置"].every((n) => drawn.some(([, pages]) => pages.includes(n))), drawn);
    const hub = await navigation(p, "GET");
    check("the hub keeps the arrangement", hub.pages?.groups?.some((g) => g.title === "生活"), hub.pages);

    // another device draws it from its hello
    const phone = await device({ width: 1440, height: 900 });
    await phone.p.waitForFunction(() => (JSON.parse(localStorage.getItem("sidebar.pages") ?? "{}").groups ?? []).some((g) => g.title === "生活"), null, { timeout: 20_000 });
    const there = await sidebarGroups(phone.p);
    check("another device draws the same groups", JSON.stringify(there) === JSON.stringify(drawn), there);
    await phone.context.close();

    for (const theme of ["light", "dark"]) {
      for (const [width, height] of [[1440, 900], [390, 844]]) {
        const d = await device({ width, height, theme });
        await d.p.waitForFunction(() => (JSON.parse(localStorage.getItem("sidebar.pages") ?? "{}").groups ?? []).some((g) => g.title === "生活"), null, { timeout: 20_000 });
        if (width < 640) {
          await d.p.getByRole("button", { name: /侧边栏/ }).first().click();
          await d.p.waitForTimeout(500); // the drawer slides in
          const groups = await sidebarGroups(d.p);
          check(`${theme} ${width}: the drawer has the groups below its grid`, groups.some(([t]) => t === "工作台"), groups.map(([t]) => t));
        }
        await d.p.screenshot({ path: join(shots, `sidebar-${theme}-${width}.png`) });
        const s = await editor(d.p, width);
        await s.evaluate((e) => e.scrollIntoView({ block: "start" }));
        await d.p.screenshot({ path: join(shots, `editor-${theme}-${width}.png`) });
        await d.context.close();
      }
    }
    await laptop.context.close();
  }
} catch (e) {
  failed = true;
  console.error(e);
}
clearTimeout(watchdog);
await browser.close();
writeFileSync(join(shots, oldHub ? "results-old-hub.json" : "results.json"), JSON.stringify(results, null, 2));
const ok = !failed && results.length > 0 && results.every((r) => r.ok);
console.log(`RESULT: sidebar-groups ${ok ? "passed" : "failed"} (${results.filter((r) => r.ok).length}/${results.length})`);
process.exit(ok ? 0 : 1);
