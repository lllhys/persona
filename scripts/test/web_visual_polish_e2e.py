#!/usr/bin/env python3
"""End-to-end check of the web visual polish (#3475): reduced motion keeps the busy signal (a spinner and the working dot
go on at ~2.4s, the other animations are stripped), the sidebar's count columns share one right edge whatever the row
(the ⋯ overlays instead of reserving width; touch screens show it always), the context meter shows used context and amber caution from 80%, and toasts show the warning icon for errors and a close button.

Needs a running web page (the Vite dev server: the toast check imports the app's own module from it) against the fake
hub with MOCK_SPLIT_TABS=1 MOCK_SOURCES=1 MOCK_CONTEXT_DANGER=1, and Playwright for Python:

    MOCK_SPLIT_TABS=1 MOCK_SOURCES=1 MOCK_CONTEXT_DANGER=1 PORT=19060 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:19060 npx vite --port 19061 --strictPort &)
    python3 scripts/test/web_visual_polish_e2e.py http://127.0.0.1:19061 [--shots <dir>]

Runs light / dark × wide (1280) / narrow (390, the drawer) × reduced motion off / on, plus a touch screen at 390. Prints
one line per check and a verdict; exits 1 on the first failure. --shots saves screenshots of each combination. The
browser is the installed Chromium (PERSONA_E2E_CHROMIUM) or Chrome.
"""
import argparse
import os
import re

from playwright.sync_api import expect, sync_playwright

TESTS_SID = "5b1170b5-0000-4000-8000-000000000001"  # 92% used in the fake hub with MOCK_SPLIT_TABS and MOCK_CONTEXT_DANGER
PHOTOS_SID = "5b1170b5-0000-4000-8000-000000000002"  # 86% used
WARNING_PATH = "M10.29 3.86"
METER = "[data-composer-usage] [role=progressbar]"
SIZES = {"wide": (1280, 820), "narrow": (390, 844)}
ROWS = ("服务器", "app", "docs")  # a machine, two of its workspaces

# The app's own atoms module: the dev server puts a ?t= stamp on modules it has hot-updated, and an import without the stamp
# would be a second copy with its own toast list.
ATOMS = "import([...performance.getEntriesByType('resource').map((r) => r.name), '/src/ui/atoms.tsx'].find((n) => /\\/src\\/ui\\/atoms\\.tsx(\\?|$)/.test(n)))"

# The right edge of a header row's count: the last number or tally in it that is not a button.
EDGE_JS = """(row) => {
  const els = [...row.querySelectorAll('.tabular-nums, [title]')].filter((e) => e.textContent.trim() && !e.closest('button'));
  const el = els[els.length - 1];
  return el ? el.getBoundingClientRect().right : null;
}"""


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    raise AssertionError(msg)


# Spinners turn this many times per cycle of their keyframes (web/src/lib/busy-motion.ts SPIN_TURNS): the cycle is this
# many times the time one turn takes.
SPIN_TURNS = 100


def per_turn(duration):
    """Seconds per turn of a spinner whose cycle is `duration` ("90s")."""
    return float(duration.rstrip("s")) / SPIN_TURNS


def close(a, b, tol=0.5):
    return abs(a - b) <= tol


def tag(theme, size, reduce):
    return f"{theme}-{size}-{'reduced' if reduce else 'motion'}"


def anim(page, selector):
    """The computed animation of the first element that matches."""
    return page.locator(selector).first.evaluate(
        "(e) => { const c = getComputedStyle(e); return { name: c.animationName, duration: c.animationDuration, count: c.animationIterationCount }; }"
    )


def new_context(browser, theme, size, reduce, touch=False):
    w, h = SIZES[size]
    ctx = browser.new_context(
        viewport={"width": w, "height": h},
        device_scale_factor=2,
        locale="zh-CN",
        color_scheme=theme,
        reduced_motion="reduce" if reduce else "no-preference",
        has_touch=touch,
        is_mobile=touch,
    )
    # (the resource buffer holds 250 entries by default, fewer than the dev server's modules: ATOMS reads it)
    ctx.add_init_script(f"performance.setResourceTimingBufferSize(5000); localStorage.setItem('persona.theme', '{theme}'); localStorage.setItem('agent.details', '1');")
    return ctx


def open_drawer(page, size):
    if size == "narrow":
        page.locator('button[aria-label="侧边栏"]').first.click()
        page.wait_for_timeout(500)


def row_of(page, text):
    return page.locator("aside .group\\/sec", has_text=text).first


def edge(page, text):
    return row_of(page, text).evaluate(EDGE_JS)


def sign_in(page, base, token):
    page.goto(f"{base}/overview?token={token}")
    page.wait_for_selector("aside")
    page.wait_for_timeout(1200)


# ── 1. reduced motion ──────────────────────────────────────────────────────────

def busy_signal(browser, base, token, theme, size, reduce, shots):
    ctx = new_context(browser, theme, size, reduce)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    # Hold the sign-in so the "joining" spinner stays up long enough to read.
    held = []
    page.route("**/auth/tokens", lambda route: held.append(route))
    page.goto(f"{base}/overview?token={token}")
    page.wait_for_selector("[role=status] .busy-motion")
    spinner = anim(page, "[role=status] .busy-motion")
    if reduce:
        assert spinner["name"] == "spin-long" and close(per_turn(spinner["duration"]), 2.4, 1e-6) and spinner["count"] == "infinite", spinner
    else:
        assert spinner["name"] == "spin-long" and close(per_turn(spinner["duration"]), 0.7, 1e-6) and spinner["count"] == "infinite", spinner
    ok(f"[{tag(theme, size, reduce)}] the sign-in spinner turns once in {per_turn(spinner['duration']):g}s x {spinner['count']}")
    if shots:
        page.screenshot(path=os.path.join(shots, f"spinner-{tag(theme, size, reduce)}.png"))
    for r in held:
        r.continue_()
    page.unroute("**/auth/tokens")
    page.wait_for_selector("aside")
    page.wait_for_timeout(1200)

    # The working dot breathes (slowly under reduced motion, and without shrinking).
    dots = page.evaluate(
        """() => document.getAnimations().filter((a) => a.animationName === 'breathe').map((a) => ({
            duration: getComputedStyle(a.effect.target).animationDuration,
            keys: [...new Set(a.effect.getKeyframes().flatMap((k) => Object.keys(k)))].filter((k) => !['offset', 'easing', 'composite', 'computedOffset'].includes(k)),
          }))"""
    )
    assert dots, "no breathing dot in the sidebar"
    for d in dots:
        if reduce:
            assert d["duration"] == "2.4s", d
            assert "transform" not in d["keys"], d
        else:
            assert d["duration"] in ("1.8s", "1.4s", "1.2s"), d
            assert "transform" in d["keys"], d
    ok(f"[{tag(theme, size, reduce)}] {len(dots)} breathing dot(s): {dots[0]['duration']}, {'opacity only' if reduce else 'opacity + scale'}")

    # Movement is still stripped: a toast slides up in 220ms, or not at all.
    page.evaluate(ATOMS + ".then((m) => m.toast('提示', 'info'))")
    page.wait_for_selector("[aria-live] > div")
    slide = anim(page, "[aria-live] > div")
    assert slide["name"] == "slide-up", slide
    assert slide["duration"] == ("0.001s" if reduce else "0.22s"), slide
    ok(f"[{tag(theme, size, reduce)}] the toast's slide-up runs {slide['duration']}")
    ctx.close()


# ── 2. toasts ──────────────────────────────────────────────────────────────────

def toasts(browser, base, token, theme, size, shots):
    ctx = new_context(browser, theme, size, False)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    sign_in(page, base, token)
    show = ATOMS + ".then((m) => { m.toast('没有发出去', 'error'); m.toast('已拷贝', 'ok'); m.toast('提示一条', 'info'); })"
    page.evaluate(show)
    rows = page.locator("[aria-live] > div")
    expect(rows).to_have_count(3)
    err = rows.nth(0)
    assert WARNING_PATH in err.inner_html(), "the error toast has no warning icon"
    assert err.locator(".rounded-full.bg-red").count() == 0, "the red dot is still there"
    for i in range(3):
        expect(rows.nth(i).locator('button[aria-label="关闭"]')).to_have_count(1)
    ok(f"[{theme}-{size}] an error toast has the warning icon, all three have a close button")
    if shots:
        page.wait_for_timeout(400)  # past the slide-up
        page.screenshot(path=os.path.join(shots, f"toasts-{theme}-{size}.png"))
    # click closes just that one
    rows.nth(1).locator('button[aria-label="关闭"]').click()
    expect(rows).to_have_count(2)
    assert "已拷贝" not in page.locator("[aria-live]").inner_text()
    # keyboard: focus the close button, Enter drops the toast
    page.keyboard.press("Shift")
    close_btn = rows.nth(0).locator('button[aria-label="关闭"]')
    close_btn.focus()
    assert close_btn.evaluate("(b) => document.activeElement === b"), "the close button cannot take focus"
    page.keyboard.press("Enter")
    expect(rows).to_have_count(1)
    ok(f"[{theme}-{size}] the close button closes by click and by keyboard")
    # the same line again is folded
    page.evaluate(ATOMS + ".then((m) => { m.toast('重复一条', 'info'); m.toast('重复一条', 'info'); })")
    expect(page.locator("[aria-live] > div", has_text="重复一条")).to_have_count(1)
    ok(f"[{theme}-{size}] the same toast twice is still one")
    ctx.close()


# ── 3. the sidebar's count column ───────────────────────────────────────────────

def counts(browser, base, token, theme, size, reduce, shots):
    ctx = new_context(browser, theme, size, reduce)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    sign_in(page, base, token)
    open_drawer(page, size)
    edges = {name: edge(page, name) for name in ROWS}
    assert all(v is not None for v in edges.values()), edges
    base_edge = edges[ROWS[0]]
    for name, v in edges.items():
        assert close(v, base_edge), f"{name}'s count ends at {v}, the machine's at {base_edge}"
    ok(f"[{tag(theme, size, reduce)}] the machine's and workspaces' counts end at the same x ({base_edge:.1f})")

    # hover: the ⋯ shows over the count's place, the count steps aside, and the menu opens
    row = row_of(page, "app")
    row.hover()
    page.wait_for_timeout(350)
    btn = row.locator("[data-row-menu]")
    assert btn.evaluate("(b) => getComputedStyle(b).opacity") == "1", "the ⋯ does not show on hover"
    box, rbox = btn.bounding_box(), row.bounding_box()
    assert close(box["x"] + box["width"], base_edge, 0.6), (box, base_edge)
    assert box["x"] + box["width"] <= rbox["x"] + rbox["width"] + 0.5
    if shots:
        page.screenshot(path=os.path.join(shots, f"sidebar-hover-{tag(theme, size, reduce)}.png"))
    btn.click()
    expect(page.locator("[role=menu]")).to_be_visible()
    page.keyboard.press("Escape")
    ok(f"[{tag(theme, size, reduce)}] hovering a workspace shows the ⋯ over its count; it opens the menu")

    # keyboard focus shows it as well
    page.mouse.move(SIZES[size][0] - 2, SIZES[size][1] - 2)
    page.keyboard.press("Shift")
    row_of(page, "docs").locator("[data-row-menu]").focus()
    page.wait_for_timeout(350)
    opacity = row_of(page, "docs").locator("[data-row-menu]").evaluate("(b) => getComputedStyle(b).opacity")
    assert opacity == "1", f"the focused ⋯ has opacity {opacity}"
    ok(f"[{tag(theme, size, reduce)}] keyboard focus shows the ⋯")
    if shots:
        page.screenshot(path=os.path.join(shots, f"sidebar-{tag(theme, size, reduce)}.png"))
    ctx.close()


def counts_touch(browser, base, token, theme, shots):
    ctx = new_context(browser, theme, "narrow", False, touch=True)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    sign_in(page, base, token)
    open_drawer(page, "narrow")
    edges = {name: edge(page, name) for name in ROWS}
    for name, v in edges.items():
        assert close(v, edges[ROWS[0]]), f"touch: {name}'s count ends at {v}, the machine's at {edges[ROWS[0]]}"
    for name in ROWS[1:]:
        row = row_of(page, name)
        btn = row.locator("[data-row-menu]")
        assert btn.evaluate("(b) => getComputedStyle(b).opacity") == "1", f"touch: {name}'s ⋯ is hidden"
        box, rbox = btn.bounding_box(), row.bounding_box()
        assert box["x"] >= edges[name] - 0.5, f"touch: {name}'s ⋯ covers its count ({box['x']} < {edges[name]})"
        assert box["x"] + box["width"] <= rbox["x"] + rbox["width"] + 0.5
        assert box["width"] >= 32 and box["height"] >= 32, box
    if shots:
        page.screenshot(path=os.path.join(shots, f"sidebar-touch-{theme}-narrow.png"))
    row_of(page, "app").locator("[data-row-menu]").tap()
    expect(page.locator("[role=menu]")).to_be_visible()
    ok(f"[{theme}-narrow-touch] counts share an edge ({edges[ROWS[0]]:.1f}), the ⋯ is always there beside them and opens the menu on a tap")
    ctx.close()


# ── 4. the context tiers ──────────────────────────────────────────────────────

def context_tiers(browser, base, token, theme, size, shots):
    ctx = new_context(browser, theme, size, False)
    page = ctx.new_page()
    page.set_default_timeout(10000)
    sign_in(page, base, token)
    page.goto(f"{base}/s/server/{TESTS_SID}")
    meter = page.locator(METER)
    expect(meter).to_have_attribute("aria-valuenow", "92")
    expect(meter.locator(".bg-orange")).to_have_attribute("style", re.compile(r"width:\s*92%"))
    expect(page.locator(f"{METER} + span")).to_have_text("上下文 92%")
    ok(f"[{theme}-{size}] the context bar shows 92% used in amber")
    if shots:
        page.screenshot(path=os.path.join(shots, f"context-caution-{theme}-{size}.png"))
    page.goto(f"{base}/s/server/{PHOTOS_SID}")
    expect(meter).to_have_attribute("aria-valuenow", "86")
    expect(meter.locator(".bg-orange")).to_be_visible()
    expect(page.locator(f"{METER} + span")).to_have_text("上下文 86%")
    ok(f"[{theme}-{size}] the context bar shows 86% used in amber")
    open_drawer(page, size)
    rows = page.locator("aside a", has_text="上下文 92%")
    if rows.count():
        expect(rows.first.locator("span.text-orange", has_text="上下文 92%")).to_be_visible()
        ok(f"[{theme}-{size}] the sidebar also shows 92% used in amber")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--token", default="mock")
    a = ap.parse_args()
    base = a.base.rstrip("/")
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        exe = os.environ.get("PERSONA_E2E_CHROMIUM")
        browser = p.chromium.launch(executable_path=exe) if exe else p.chromium.launch(channel="chrome")
        try:
            for theme in ("light", "dark"):
                for size in SIZES:
                    for reduce in (False, True):
                        busy_signal(browser, base, a.token, theme, size, reduce, a.shots)
                        counts(browser, base, a.token, theme, size, reduce, a.shots)
                    toasts(browser, base, a.token, theme, size, a.shots)
                    context_tiers(browser, base, a.token, theme, size, a.shots)
                counts_touch(browser, base, a.token, theme, a.shots)
        finally:
            browser.close()
    print("PASS web visual polish e2e")


if __name__ == "__main__":
    main()
