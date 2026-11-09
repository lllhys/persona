#!/usr/bin/env python3
"""End-to-end check of a to-do's knowledge-base project on the web (#2581, docs/assistant/todos.md "Project"): the open
to-dos are grouped under their project's name first, the detail's project menu moves one to another project (long-term
projects first) or out of any, and each change reaches the hub. The page runs in zh-CN, as the other web e2e checks, so
the selectors use the Chinese labels.

Needs a running web page against the fake hub, and Playwright for Python:

    PORT=18899 python3 scripts/mock/mock_hub.py &
    (cd web && PERSONA_HUB_URL=http://127.0.0.1:18899 npx vite --port 5899 --strictPort &)
    python3 scripts/test/web_todo_project_e2e.py http://127.0.0.1:5899 --hub http://127.0.0.1:18899 [--shots <dir>]

Prints one line per check and a verdict; exits 1 on the first failure. Runs light and dark, at desktop and phone
widths; --shots saves each state there. --channel chrome / msedge uses an installed browser instead of Playwright's
own Chromium.
"""
import argparse
import json
import os
import sys
import time
import urllib.request

from playwright.sync_api import expect, sync_playwright

SIZES = {"wide": {"width": 1280, "height": 860}, "narrow": {"width": 390, "height": 844}}
NAS = "todo_nas"  # the mock's own to-do, put in the photo-sync project for the check
NAS_TITLE = "给 NAS 配自动备份"


def ok(msg):
    print("ok  " + msg, flush=True)


def call(hub, token, method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(hub + path, data=data, method=method,
                                 headers={"Authorization": f"Bearer {token}", "Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=10))


def todo(hub, token, tid):
    return next(t for t in call(hub, token, "GET", "/api/v1/todos")["todos"] if t["id"] == tid)


def until(check, what):
    deadline = time.time() + 10
    while time.time() < deadline:
        if check():
            return
        time.sleep(0.2)
    raise AssertionError("timed out: " + what)


def run(browser, base, hub, token, size, theme, shots):
    tag = f"{size}/{theme}"
    call(hub, token, "PATCH", f"/api/v1/todos/{NAS}", {"project": "photo-sync"})  # the fixture
    ctx = browser.new_context(viewport=SIZES[size], device_scale_factor=2, has_touch=size == "narrow", locale="zh-CN",
                              color_scheme=theme)
    page = ctx.new_page()
    page.set_default_timeout(20000)
    page.goto(base + "/overview?token=" + token)
    page.wait_for_function("localStorage.getItem('persona.token')")
    page.evaluate(f"localStorage.setItem('persona.theme', '{theme}')")

    def shot(name):
        if shots:
            page.wait_for_timeout(300)
            page.screenshot(path=os.path.join(shots, f"todo-project-{size}-{theme}-{name}.png"))

    # The list: the project's group comes first, under its name, with the NAS to-do in it; the rest follow.
    page.goto(base + "/todos")
    groups = page.locator('[data-testid="todo-group"]')
    first = groups.first
    expect(first).to_have_attribute("data-group", "todos-kb:photo-sync")
    expect(first.get_by_role("button", name="照片同步脚本")).to_be_visible()
    expect(first.locator('[role="button"]', has_text=NAS_TITLE)).to_be_visible()
    expect(groups.last.get_by_role("button", name="未归类")).to_be_visible()
    shot("list")
    ok(f"{tag}: the to-do is grouped under its project, the ones without any last")

    # The detail: the menu shows its project; long-term ones come first; moving it reaches the hub and the list.
    first.locator('[role="button"]', has_text=NAS_TITLE).click()
    picker = page.locator('[data-testid="todo-plan-project"]')
    expect(picker).to_have_value("photo-sync")
    options = picker.locator("option").all_inner_texts()
    assert options[:2] == ["不归类", "读书笔记"] and "促销邮件" not in options, options
    shot("detail")
    picker.select_option("reading")
    until(lambda: todo(hub, token, NAS).get("project") == "reading", "project reading on the hub")
    expect(picker).to_have_value("reading")
    if size == "wide":  # the list is beside the detail
        expect(page.locator('[data-group="todos-kb:reading"]').locator('[role="button"]', has_text=NAS_TITLE)).to_have_count(1)
    shot("moved")
    picker.select_option("")
    until(lambda: "project" not in todo(hub, token, NAS), "project cleared on the hub")
    if size == "wide":
        expect(page.locator('[data-group="todos-kb:reading"]')).to_have_count(0)
    ok(f"{tag}: moved to another project and out of any")
    ctx.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("base")
    ap.add_argument("--hub", required=True)
    ap.add_argument("--token", default="mock")
    ap.add_argument("--shots")
    ap.add_argument("--channel", default="")
    a = ap.parse_args()
    if a.shots:
        os.makedirs(a.shots, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(channel=a.channel or None)
        try:
            for size in SIZES:
                for theme in ("light", "dark"):
                    run(browser, a.base, a.hub, a.token, size, theme, a.shots)
        except Exception as e:  # noqa: BLE001 — one line and a verdict, as the other e2e scripts
            print(f"FAIL {e}", flush=True)
            sys.exit(1)
        finally:
            browser.close()
    print("PASS", flush=True)


if __name__ == "__main__":
    main()
