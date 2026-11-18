#!/usr/bin/env python3
"""End-to-end check of "Quote" in the web chat (#2427): quoting a reply of the main conversation puts a bar above the input
(who and when, a plain two-line summary, × to drop it) and leaves the input empty and focused; sending carries the quote
as the same `> ` block in front of the message; the sent bubble (and the same message after a reload, from the
history: MOCK_QUOTE's earlier message) shows it as a folded quote card at its top, not `>` lines, opening to the whole passage.

    MOCK_QUOTE=1 PORT=18841 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18841 npx vite --port 5341 --strictPort &)
    python3 scripts/test/web_quote_e2e.py http://127.0.0.1:5341 [--shots <dir>]

`--before` only takes the same screenshots (for the build before #2427, which typed `> ` lines into the input).
Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each.
"""
import argparse
import os
import re
import sys

from playwright.sync_api import expect, sync_playwright

SIZES = {"wide": {"width": 1280, "height": 900}, "narrow": {"width": 390, "height": 844}}
SEND = re.compile(r"^(发送|排队发送)$")  # the round button queues while the agent works (#5324)


def ok(msg):
    print("ok  " + msg, flush=True)


def fail(msg):
    print("FAIL " + msg, flush=True)
    sys.exit(1)


def shot(page, shots, name):
    if shots:
        page.screenshot(path=os.path.join(shots, name + ".png"))


def quote_reply(page):
    """Opens the long reply's menu (right-click works at both widths) and picks "Quote"."""
    reply = page.locator(".prose-j").filter(has_text="件事需要你").first
    reply.scroll_into_view_if_needed()
    reply.click(button="right")
    page.get_by_role("menuitem", name="引用").click()


def case(browser, base, token, size, theme, shots, before):
    tag = f"{size}/{theme}"
    name = f"quote-{'before' if before else 'after'}-{size}-{theme}"
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2)
    page = ctx.new_page()
    page.set_default_timeout(15000)
    page.goto(base + "/overview?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")
    page.goto(base + "/")
    expect(page.locator(".prose-j").last).to_be_visible()
    box = page.locator("textarea").last
    quote_reply(page)
    page.wait_for_timeout(400)
    shot(page, shots, name + "-composer")
    if before:
        box.fill(box.input_value() + "这段再展开讲讲")
        page.wait_for_timeout(400)
    else:
        bar = page.get_by_label("引用的消息")
        expect(bar).to_be_visible()
        if box.input_value() != "":
            fail(f"{tag}: the input has text after 引用: {box.input_value()!r}")
        if not box.evaluate("el => el === document.activeElement"):
            fail(f"{tag}: the input is not focused after 引用")
        if ">" in bar.inner_text():
            fail(f"{tag}: the bar shows > marks")
        ok(f"{tag}: the quote waits in a bar, the input is empty and focused")
        page.get_by_role("button", name="取消引用").click()
        expect(bar).to_have_count(0)
        ok(f"{tag}: × drops the quote")
        quote_reply(page)
        expect(bar).to_be_visible()
        box.fill("这段再展开讲讲")
        page.wait_for_timeout(400)
        shot(page, shots, name + "-typed")
    posts = []
    page.on("request", lambda r: posts.append(r.post_data or "") if r.method == "POST" else None)
    page.get_by_role("button", name=SEND).click()
    page.wait_for_timeout(1200)
    bubble = page.locator("[data-user-bubble]").last
    bubble.scroll_into_view_if_needed()
    shot(page, shots, name + "-sent")
    if not before:
        body = next((p for p in posts if "这段再展开讲讲" in p), "")
        if '"text":"> ' not in body or "这段再展开讲讲" not in body:
            fail(f"{tag}: the message did not go out as a > block in front of the words: {body[:200]!r}")
        ok(f"{tag}: sent as the > block in front of the words")
        card = bubble.locator("[data-quote]")
        expect(card).to_have_count(1)
        if ">" in bubble.inner_text():
            fail(f"{tag}: the bubble shows > marks")
        ok(f"{tag}: the bubble draws the quote as a card")
        page.reload()
        bubble = page.locator("[data-user-bubble]").filter(has_text="第二件先放一放").last
        bubble.scroll_into_view_if_needed()
        expect(bubble.locator("[data-quote]")).to_have_count(1)
        if ">" in bubble.inner_text() or "**" in bubble.inner_text():
            fail(f"{tag}: the earlier message shows > or ** marks")
        ok(f"{tag}: an earlier message from the history has its quote card")
        page.wait_for_timeout(300)
        shot(page, shots, name + "-history")
        bubble.get_by_role("button", name="展开引用").click()
        expect(bubble.get_by_role("button", name="收起引用")).to_be_visible()
        expect(bubble).to_contain_text("她下午要和客户确认")
        bubble.scroll_into_view_if_needed()
        page.wait_for_timeout(300)
        shot(page, shots, name + "-open")
        ok(f"{tag}: a long quote opens to the whole passage")
        overflow = page.evaluate("document.documentElement.scrollWidth - document.documentElement.clientWidth")
        if overflow > 0:
            fail(f"{tag}: the page scrolls sideways by {overflow}px")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    ap.add_argument("--before", action="store_true")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as pw:
        browser = pw.chromium.launch(channel="chrome")
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    case(browser, a.base.rstrip("/"), a.token, size, theme, a.shots, a.before)
        finally:
            browser.close()
    print("PASS")


if __name__ == "__main__":
    main()
