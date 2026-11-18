#!/usr/bin/env python3
"""End-to-end check of side sessions in their parent's chat on the web (#7944). On `MOCK_SIDE_SESSIONS=1` the hub folds
three Claude desktop side sessions of the PR review into it (`sessions_side_fold`): a /btw question that waits on a
question card, a side chat, and a stopped side session of that side chat. No side is a row of its own; the review's row
needs you (its waiting side). The review's header has a "Side sessions" button with the count, 3, and the attention
dot; it never opens the panel by itself. The panel lists the sides flat, needs you first and the stopped one last, each
pill named by its title and state, and shows the chosen side's own conversation. The waiting side's card has one home on
the page: in the side's conversation while the panel shows that side, else in the review's dock, "From side session ·
…". A link to a side (`/s/<machine>/<side>`) opens the review with the panel on that side, at any width. Light / dark ×
1280 / 390, English.

    MOCK_SIDE_SESSIONS=1 MOCK_MANAGERS=0 PORT=18797 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18797 npx vite --port 5207 --strictPort &)
    python3 scripts/test/web_side_sessions_e2e.py http://127.0.0.1:5207 [--shots <dir>]

Prints one line per check and a verdict (also when a wait fails or times out); exits 1 on the first failure.
"""
import argparse
import os
import sys

from playwright.sync_api import Error as PlaywrightError, expect, sync_playwright

MID = "macbook"
REVIEW = "a1f3c2d4-0000-4000-8000-000000000006"
BTW = "a1f3c2d4-0000-4000-8000-0000000059a1"  # waits on a question card
CHAT = "a1f3c2d4-0000-4000-8000-0000000059a2"  # idle
STOPPED = "a1f3c2d4-0000-4000-8000-0000000059a3"  # stopped, a side of the side chat
SIDES = (BTW, CHAT, STOPPED)
SIZES = {"wide": {"width": 1280, "height": 860}, "narrow": {"width": 390, "height": 844}}
OVERFLOW = "() => document.documentElement.scrollWidth - document.documentElement.clientWidth"
PANEL = 'aside[aria-label="Side sessions"]'


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def settle(page, ms=1500):
    page.wait_for_load_state("load")
    page.wait_for_timeout(ms)


def run(browser, base, size, theme, shots):
    tag = f"{size}/{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2 if size == "narrow" else 1, has_touch=size == "narrow",
                              color_scheme=theme, locale="en-US")
    page = ctx.new_page()
    page.set_default_timeout(15000)
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.goto(base + "/overview?token=mock")
    settle(page)
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    prefix = f"{size}-{theme}"

    def shot(name):
        if shots:
            page.screenshot(path=os.path.join(shots, f"{prefix}-{name}.png"))

    panel = page.locator(PANEL)
    pills = panel.get_by_role("tab")
    button = page.locator("main header [data-side-sessions]")
    main_dock = page.locator("main [data-card]")

    # the list: on the phone the Agents page, on a computer the sidebar beside the review's page
    page.goto(base + ("/agents" if size == "narrow" else f"/s/{MID}/{REVIEW}"))
    settle(page, 2500)
    review_row = page.locator(f'a[href$="/{REVIEW}"]').first
    expect(review_row).to_be_visible()
    for sid in SIDES:
        if page.locator(f'a[href$="/{sid}"]').count():
            fail(f"{tag}: the side session {sid} is a row of its own")
    expect(review_row).to_contain_text("Has a question")
    ok(f"{tag}: no side session is a row; the review's row needs you for its waiting side")
    shot("list")

    if size == "narrow":
        review_row.click()
        page.wait_for_url(f"**/{REVIEW}")
        settle(page)
    expect(button).to_have_attribute("data-side-sessions", "3")
    expect(button).to_have_attribute("aria-label", "Side sessions (3) · A side session needs you")
    expect(page.locator("main header [data-side-sessions-needs]")).to_be_visible()
    if panel.count():
        fail(f"{tag}: the Side sessions panel opened by itself")
    expect(main_dock.first).to_contain_text("From side session · btw: 这个 PR 现在能合吗")
    ok(f"{tag}: the header's Side sessions button says 3 with the dot; the panel stays shut; the side's card is in the review's dock, from that side")
    shot("header")

    # the panel: by hand, the sides flat in order, the waiting one shown with its card, not also in the review's dock
    button.click()
    expect(panel).to_be_visible()
    expect(pills).to_have_count(3)
    expect(pills.nth(0)).to_have_attribute("aria-label", "btw: 这个 PR 现在能合吗 · Needs you")
    expect(pills.nth(1)).to_contain_text("Side chat")
    expect(pills.nth(2)).to_contain_text("查一下 CI 为什么慢")
    expect(pills.nth(0)).to_have_attribute("aria-selected", "true")
    expect(panel.locator("[data-card]")).to_have_count(1)
    page.wait_for_timeout(300)
    if size == "wide" and main_dock.count():
        fail(f"{tag}: the shown side's card is in the review's dock too")
    ok(f"{tag}: the panel lists the sides needs-you first, stopped last, and the waiting side's card is only in its conversation")
    shot("panel")

    # another side: the waiting one's card goes back to the review's dock
    pills.nth(1).click()
    expect(pills.nth(1)).to_have_attribute("aria-selected", "true")
    expect(panel.locator("[data-card]")).to_have_count(0)
    ok(f"{tag}: picking another side takes the card out of the panel")
    if size == "wide":
        expect(main_dock.first).to_contain_text("From side session · btw: 这个 PR 现在能合吗")
        ok(f"{tag}: the card is back in the review's dock, from that side")
        page.wait_for_timeout(800)  # the card's way in
        shot("request")
    panel.get_by_role("button", name="Close panel").click()
    expect(panel).to_have_count(0)

    # a deep link to a side: the review's page with the panel on it
    page.goto(base + f"/s/{MID}/{CHAT}")
    page.wait_for_url(f"**/{REVIEW}")
    settle(page)
    expect(panel).to_be_visible()
    expect(pills.nth(1)).to_have_attribute("aria-selected", "true")
    expect(panel.locator("header")).to_contain_text("Side chat")
    ok(f"{tag}: a link to a side opens the review with the panel on that side")
    shot("deeplink")
    if size == "narrow":
        # the request's single home on a phone: the panel covers the page, so it is the review's dock once closed
        panel.get_by_role("button", name="Close panel").click()
        expect(panel).to_have_count(0)
        expect(main_dock.first).to_contain_text("From side session · btw: 这个 PR 现在能合吗")
        ok(f"{tag}: with the panel closed the card is in the review's dock, from that side")
        page.wait_for_timeout(800)
        shot("request")

    overflow = page.evaluate(OVERFLOW)
    if overflow > 1:
        fail(f"{tag}: the page scrolls sideways by {overflow}px")
    if errors:
        fail(f"{tag}: page errors: {errors[:3]}")
    ok(f"{tag}: no sideways scroll, no page errors")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--shots")
    ap.add_argument("--channel", default="chrome")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(channel=a.channel or None)
            for size in SIZES:
                for theme in ("light", "dark"):
                    run(browser, a.base.rstrip("/"), size, theme, a.shots)
            browser.close()
    except (PlaywrightError, AssertionError) as e:
        fail(f"a wait failed or timed out: {str(e).splitlines()[0] if str(e) else type(e).__name__}")
    print("PASS side sessions e2e", flush=True)


if __name__ == "__main__":
    main()
