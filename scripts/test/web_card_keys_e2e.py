#!/usr/bin/env python3
"""End-to-end check of answering a waiting card from the keyboard (#1720): the shortcut focuses the first card, digits
pick options or submit a single-choice question, Enter runs the card's main action, Esc goes back to the composer
without answering, and the key hints show only while the card holds keyboard focus. Also saves screenshots of a focused
card, light / dark, wide and phone.

Needs a running web page against the fake hub and Playwright for Python (it drives the installed Chrome):

    MOCK_SETTINGS_INPUT=1 PORT=18798 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18798 npx vite --port 5298 --strictPort &)
    python3 scripts/test/web_card_keys_e2e.py http://127.0.0.1:5298 [--shots <dir>]

The checks answer the mock hub's cards (a question, a plan, a permission), so each run needs its own hub; the
screenshot passes only focus, which is local. Runs on Linux / Windows (Mod is Ctrl). Prints one line per check and a
verdict; exits 1 on the first failure.
"""
import argparse
import os
import sys

from playwright.sync_api import expect, sync_playwright

TESTS = "/s/macbook/a1f3c2d4-0000-4000-8000-000000000002"  # a question card ("ix_tests")
PHOTOS = "/s/studio/a1f3c2d4-0000-4000-8000-000000000005"  # a plan card ("ix_photos")
REVIEW = "/s/macbook/a1f3c2d4-0000-4000-8000-000000000006"  # a permission card (MOCK_SETTINGS_INPUT)
CARD = "[data-card-keys]"
HINT = f"{CARD} [data-card-actions] kbd"  # the Enter badge; the digit badges are [data-key-hint] spans


def ok(msg):
    print("ok  " + msg, flush=True)


def run(browser, base, token, shots):
    def fresh(path, width, height, dark, touch=False):
        """A new browser with a card's session open, focused nowhere (its own localStorage, so its own theme)."""
        ctx = browser.new_context(viewport={"width": width, "height": height}, device_scale_factor=2, locale="zh-CN",
                                  is_mobile=touch, has_touch=touch)
        ctx.add_init_script(f"localStorage.setItem('persona.theme', '{'dark' if dark else 'light'}')")
        page = ctx.new_page()
        page.set_default_timeout(10000)
        page.goto(f"{base}{path}?token={token}")
        page.wait_for_selector(CARD)
        return ctx, page

    def shortcut(page):
        """Presses the focus-card chord, once more if the page was still mounting (a dev server's first load)."""
        page.keyboard.press("Control+Alt+Enter")
        if page.evaluate(f"document.activeElement?.closest('{CARD}')") is None:
            page.wait_for_timeout(800)
            page.keyboard.press("Control+Alt+Enter")
        assert page.evaluate(f"document.activeElement?.closest('{CARD}') !== null"), "the shortcut should focus the card"

    # Screenshots first (focusing only; a digit submits now, and the checks below answer the hub's cards away).
    for name, path, size, dark, touch in (
        ("question-light", TESTS, (1280, 900), False, False),
        ("question-dark", TESTS, (1280, 900), True, False),
        ("question-phone-light", TESTS, (390, 844), False, True),
        ("question-phone-dark", TESTS, (390, 844), True, True),
    ):
        ctx, page = fresh(path, *size, dark, touch)
        expect(page.locator(HINT)).to_have_count(0)  # at rest: no hints
        shortcut(page)
        expect(page.locator(f"{CARD} [data-key-hint]")).not_to_have_count(0)
        if shots:
            page.screenshot(path=os.path.join(shots, f"card-keys-{name}.png"))
        ctx.close()
    ok("the shortcut focuses the card at every size, hints show only then (light and dark)")

    # A question card: Esc hands focus back to the composer without answering; a digit answers on its own.
    ctx, page = fresh(TESTS, 1280, 900, dark=False)
    page.locator("main textarea").first.click()
    assert page.evaluate(f"document.activeElement?.closest('{CARD}')") is None, "nothing should have the card focused yet"
    expect(page.locator(f"{CARD} [data-key-hint]")).to_have_count(0)
    shortcut(page)
    page.keyboard.press("Escape")
    assert page.evaluate("document.activeElement?.tagName") == "TEXTAREA", "Esc should return to the composer"
    expect(page.locator(f"{CARD} button[aria-pressed='true']")).to_have_count(0)
    expect(page.locator("[data-answered]")).to_have_count(0)
    shortcut(page)
    expect(page.locator(HINT)).to_have_count(1)
    page.keyboard.press("1")
    page.wait_for_selector("[data-answered]")
    ok("Esc returns to the composer and answers nothing; a digit submits the answer")

    # A plan card: Enter approves it.
    ctx, page = fresh(PHOTOS, 1280, 900, dark=False)
    shortcut(page)
    page.keyboard.press("Enter")
    page.wait_for_selector("[data-answered]")
    ok("Enter approves a plan card")

    # A permission card (MOCK_SETTINGS_INPUT): Enter allows, never denies — the Deny button is the only deny.
    ctx, page = fresh(REVIEW, 1280, 900, dark=False)
    shortcut(page)
    if shots:
        page.emulate_media(color_scheme="dark")
        page.screenshot(path=os.path.join(shots, "card-keys-permission-dark.png"))
        page.emulate_media(color_scheme="light")
    page.keyboard.press("Enter")
    page.wait_for_selector("[data-answered]")
    assert "拒绝" not in page.locator("[data-answered]").inner_text(), "Enter must never deny"
    ok("Enter allows a permission card; denying stays an explicit press")


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
        browser = p.chromium.launch(channel="chrome")
        try:
            run(browser, base, a.token, a.shots)
        finally:
            browser.close()
    print("PASS web card keys e2e")


if __name__ == "__main__":
    try:
        main()
    except AssertionError as e:
        print("FAIL " + str(e), flush=True)
        sys.exit(1)
