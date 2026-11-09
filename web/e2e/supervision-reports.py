#!/usr/bin/env python3
"""Check report cards in the actual web app, using the mock hub's exchange fixture.

Start MOCK_TASKS=1 MOCK_EXCHANGES=1 scripts/mock/mock_hub.py and a web preview.
Run: python3 web/e2e/supervision-reports.py <web URL> --shots <directory>
Only synthetic feed data is used. --cdp retains the shared browser and test pages.
"""
import argparse
import json
import sys
from pathlib import Path

from playwright.sync_api import TimeoutError as BrowserTimeout, expect, sync_playwright

BODY_LINK = "https://github.com/example/weather/pull/45"
EVIDENCE = [
    "https://example.com/evidence/pull/45",
    "https://example.com/evidence/commit/abc123",
    "https://example.com/evidence/issue-comment/456",
    "https://example.com/evidence/actions/runs/1001",
    "/tmp/example-results.xcresult",
]
SIZES = {"wide": {"width": 1280, "height": 1000}, "narrow": {"width": 390, "height": 844}}


def case(browser, base, size, theme, shots):
    context = browser.new_context(viewport=SIZES[size], device_scale_factor=1, locale="zh-CN")
    body = []

    def feed(route):
        response = route.fetch(timeout=20000)
        data = response.json()
        for item in data.get("items", []):
            if item.get("id") == "feed_x2":
                body.append(item["body"]["text"])
                item["body"]["evidence"] = EVIDENCE
        route.fulfill(response=response, body=json.dumps(data), content_type="application/json")

    context.route("**/api/v1/assistant/conversations/*/feed*", feed)
    page = context.new_page()
    page.set_default_timeout(20000)
    page.goto(base + "/overview?token=mock")
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate("theme => {localStorage.setItem('persona.theme', theme); localStorage.setItem('persona.lang', 'zh-Hans');}", theme)
    page.goto(base + "/")
    card = page.locator('[data-testid="report-line"]').filter(has_text=BODY_LINK)
    expect(card).to_have_count(1)
    expect(card).to_be_visible()
    assert body, "The actual feed response did not include the synthetic report"
    shown = card.locator(".whitespace-pre-wrap").text_content()
    assert shown.endswith(body[-1]), "The report body was altered"
    assert BODY_LINK in shown, "The authored link was removed from the body"
    expect(card.locator("ul, li")).to_have_count(0)
    for entry in EVIDENCE:
        assert entry not in card.inner_text(), "A separate evidence row is still visible"
    card.scroll_into_view_if_needed()
    page.evaluate("document.fonts.ready")
    if shots:
        page.screenshot(path=str(shots / f"web-report-{size}-{theme}.png"))
        card.screenshot(path=str(shots / f"web-report-card-{size}-{theme}.png"))
    print(f"PASS {size}/{theme}: report body unchanged; authored link retained; 5 evidence entries hidden", flush=True)
    return context


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base")
    parser.add_argument("--shots", type=Path)
    parser.add_argument("--cdp")
    args = parser.parse_args()
    if args.shots:
        args.shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as playwright:
        browser = playwright.chromium.connect_over_cdp(args.cdp) if args.cdp else playwright.chromium.launch(channel="chrome")
        contexts = []
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    contexts.append(case(browser, args.base.rstrip("/"), size, theme, args.shots))
        finally:
            if not args.cdp:
                for context in contexts:
                    context.close()
                browser.close()
    print("PASS supervision report browser verification", flush=True)


if __name__ == "__main__":
    try:
        main()
    except BrowserTimeout:
        print("TIMEOUT supervision report browser verification", file=sys.stderr, flush=True)
        sys.exit(1)
    except Exception:
        print("FAIL supervision report browser verification", file=sys.stderr, flush=True)
        raise
