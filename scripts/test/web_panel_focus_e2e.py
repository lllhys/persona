#!/usr/bin/env python3
"""End-to-end check that opening a task in the side panel does not push the page up (#1282, #5424).

The bug: in a task's side panel, a plan waiting for approval with a question and a permission under it stood taller
than the panel; the composer's focus then scrolled the page's overflow-hidden root up to show it (about 80-150 px),
and the panel's header went off the top. The panel's dock now folds its cards and keeps to DOCK_MAX as a narrow
screen's does, and the composer's focus does not scroll its ancestors.

Needs a running web page against the fake hub and Playwright for Python:

    MOCK_TASKS=1 MOCK_TASKS_DOCK=many PORT=18793 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18793 npx vite --port 5293 --strictPort &)
    python3 scripts/test/web_panel_focus_e2e.py http://127.0.0.1:5293 [--size 1280x800] [--shots <dir>]

Opens every row of the main conversation's Needs you and In progress trays in turn (the side panel is the default of
Settings › How Agents open), light and dark, and checks that no overflow-hidden element got scrolled and the panel's
header is in the window. Prints one line per row and a verdict; exits 1 on the first failure.
"""
import argparse
import re
import sys

from playwright.sync_api import sync_playwright

# The largest scrollTop of an element that does not scroll on its own: the page pushed up when it is above 0.
PUSHED = """() => {
  let worst = 0, who = '';
  for (const el of document.querySelectorAll('*')) {
    const s = getComputedStyle(el);
    if ((s.overflowY === 'hidden' || s.overflowY === 'clip') && el.scrollTop > worst) {
      worst = el.scrollTop; who = el.tagName + '.' + String(el.className).slice(0, 60);
    }
  }
  return { worst, who, doc: document.scrollingElement.scrollTop };
}"""


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--size", default="1280x800")
    ap.add_argument("--shots")
    ap.add_argument("--executable", help="a Chromium to drive instead of Playwright's own")
    a = ap.parse_args()
    w, h = (int(x) for x in a.size.split("x"))
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=a.executable) if a.executable else p.chromium.launch()
        for theme in ("light", "dark"):
            ctx = browser.new_context(viewport={"width": w, "height": h}, color_scheme=theme)
            ctx.add_init_script(f"localStorage.setItem('persona.theme', '{theme}'); localStorage.setItem('persona.language', 'en')")
            page = ctx.new_page()
            page.goto(a.base + "/?token=" + a.token)
            page.wait_for_selector("[data-assistant-actionbar] [data-tray-entry]", timeout=20000)
            page.wait_for_timeout(800)
            opened = 0
            for tray, rows in (("needs", "button[data-needs-row]"), ("running", "[data-task-slot], [data-running-agent]")):
                entry = page.locator(f'[data-assistant-actionbar] [data-tray-entry="{tray}"]')
                for i in range(30):
                    if entry.get_attribute("aria-expanded") != "true":
                        entry.click()
                        page.wait_for_timeout(300)
                    row = page.locator(f"[data-assistant-actionbar] :is({rows})")
                    if row.count() <= i:
                        break
                    label = row.nth(i).inner_text().replace("\n", " ")[:30]
                    # a folded Needs you row opens its card in place (#5569): the panel opens from the card's Open button
                    # (or its title), and a card with neither is only checked for pushing the page
                    folded = row.nth(i).get_attribute("aria-expanded") is not None
                    row.nth(i).click()
                    aside = page.locator("aside[aria-label]")
                    if folded:
                        page.wait_for_timeout(300)
                        card = page.locator("[data-assistant-actionbar] [data-needs-open]").first
                        go = card.get_by_role("button", name=re.compile(r"^Open( |$)")).first
                        if go.count() == 0:
                            r = page.evaluate(PUSHED)
                            ok = r["worst"] == 0 and r["doc"] == 0
                            print(f"{'ok  ' if ok else 'FAIL'} {theme} {w}x{h} {tray} [{label}]: opened in place, pushed {r['worst']} px {r['who']}")
                            if not ok:
                                print("FAIL: opening a card in place pushed the page up")
                                return 1
                            card.locator("button[aria-expanded=true]").first.click()
                            page.wait_for_timeout(200)
                            continue
                        go.click()
                    aside.wait_for(timeout=10000)
                    page.wait_for_timeout(900)
                    r = page.evaluate(PUSHED)
                    top = aside.locator("header").first.bounding_box()
                    if a.shots:
                        page.screenshot(path=f"{a.shots}/panel-{theme}-{w}-{tray}-{i}.png")
                    ok = r["worst"] == 0 and r["doc"] == 0 and top is not None and top["y"] >= 0
                    print(f"{'ok  ' if ok else 'FAIL'} {theme} {w}x{h} {tray} [{label}]: pushed {r['worst']} px {r['who']}, header at {top and top['y']}")
                    if not ok:
                        print("FAIL: opening a task in the side panel pushed the page up")
                        return 1
                    opened += 1
                    aside.get_by_role("button", name="Close panel").first.click()
                    page.wait_for_timeout(300)
            if not opened:
                print("FAIL: no row opened a side panel")
                return 1
            ctx.close()
        browser.close()
    print("PASS: no task's panel pushes the page up")
    return 0


if __name__ == "__main__":
    sys.exit(main())
