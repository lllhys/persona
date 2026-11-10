#!/usr/bin/env python3
"""Verify the main conversation's dispatched line (#3700) in a real browser, with synthetic HTTP/WS data only.

Build web first. Run with a loopback production preview URL and --shots <directory>:

    python3 web/e2e/dispatched-line.py http://127.0.0.1:<port> --shots <directory>

One dispatched task reads "Dispatched: <title> · Open" and opens that task in the side panel, its Agent's chat; several
list their titles, each opening its own task. Neither opens the In progress tray. Light and dark, wide; English.
"""
import argparse
import json
import re
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

AT = "2026-11-10T08:00:00Z"
MAIN = "demo-main"
MACHINE = {"id": "demo", "name": "Computer A", "online": True, "os": "linux"}
PERSONA = {"machine_id": "demo", "session_id": MAIN, "title": "Persona", "state": "idle", "status": "idle", "cwd": "/demo", "updated_at": AT, "started_by_persona": True, "capabilities": {"send": True, "interrupt": True}}


def worker(i, title):
    return {"machine_id": "demo", "session_id": f"worker-{i}", "title": title, "state": "working", "status": "working", "cwd": "/demo/project", "updated_at": AT, "agent": "claude", "todo": {"id": f"demo-task-{i}", "title": title}}


def todo(i, title, agent=True):
    return {"id": f"demo-task-{i}", "title": title, "origin": MAIN, "created_at": AT, "updated_at": AT,
            "agents": [{"machine_id": "demo", "session_id": f"worker-{i}", "linked_at": AT}] if agent else []}


# An unrelated running task of the same conversation: it is in In progress, never in the dispatched line.
OTHER = {"id": "demo-other", "title": "Earlier unrelated task", "origin": MAIN, "created_at": AT, "updated_at": AT,
         "agents": [{"machine_id": "demo", "session_id": "worker-9", "linked_at": AT}]}
OTHER_SESSION = worker(9, "Earlier unrelated task") | {"todo": {"id": "demo-other", "title": "Earlier unrelated task"}}
WORKER_ITEMS = [{"id": "w-1", "seq": 1, "role": "user", "kind": "text", "text": "Fix the crash on the sign-in page.", "ts": AT},
                {"id": "w-2", "seq": 2, "role": "assistant", "kind": "text", "text": "Reproduced it; working on the fix now.", "ts": AT}]


def scenario(name):
    """The feed of one reply that dispatched work: `one` a single task, `many` a plan of three."""
    if name == "one":
        title = "Fix the sign-in crash"
        todos = [todo(1, title), OTHER]
        sessions = [PERSONA, worker(1, title), OTHER_SESSION]
        feed = [{"id": "demo-task-event", "conversation": MAIN, "at": AT, "kind": "task", "ref": "demo-task-1"}]
        return todos, sessions, feed, {}
    titles = ["Fix the sign-in crash", "Update the release notes", "Check the Android build"]
    plan = {"id": "demo-plan", "conversation": MAIN, "created_at": AT, "state": "started",
            "tasks": [{"title": t, "prompt": "Synthetic task", "machine": "demo", "cwd": "/demo/project", "todo_id": f"demo-task-{i}"} for i, t in enumerate(titles)]}
    # the third has no Agent yet: it opens its task card
    todos = [todo(i, t, agent=i < 2) | {"plan_id": plan["id"]} for i, t in enumerate(titles)] + [OTHER]
    sessions = [PERSONA, worker(0, titles[0]), worker(1, titles[1]), OTHER_SESSION]
    feed = [{"id": "demo-plan-event", "conversation": MAIN, "at": AT, "kind": "plan", "ref": plan["id"]}] + \
           [{"id": f"demo-task-event-{i}", "conversation": MAIN, "at": AT, "kind": "task", "ref": f"demo-task-{i}"} for i in range(len(titles))]
    return todos, sessions, feed, {plan["id"]: plan}


def run(args):
    shots = Path(args.shots)
    shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=args.chromium, args=["--no-sandbox"])
        try:
            for name in ("one", "many"):
                for theme in ("light", "dark"):
                    todos, sessions, feed, plans = scenario(name)
                    items = [{"id": "demo-user", "seq": 1, "role": "user", "kind": "text", "text": "Please get this fixed today.", "ts": AT},
                             {"id": "demo-answer", "seq": 2, "role": "assistant", "kind": "text", "text": "Handed it to an Agent; it is on it now.", "ts": AT}]
                    context = browser.new_context(viewport={"width": 1280, "height": 760}, locale="en-US", color_scheme=theme, reduced_motion="reduce")
                    context.add_init_script(f"localStorage.setItem('persona.token', 'demo'); localStorage.setItem('persona.language', 'en'); localStorage.setItem('persona.theme', '{theme}');")
                    context.add_init_script("const RealDate = Date; globalThis.Date = class extends RealDate { constructor(...args) { super(...(args.length ? args : ['2026-11-10T08:10:00Z'])); } static now() { return new RealDate('2026-11-10T08:10:00Z').getTime(); } };")

                    def api(route):
                        path = urlparse(route.request.url).path.removeprefix("/api/v1")
                        data = {}
                        if path == "/machines":
                            data = {"machines": [MACHINE]}
                        elif path == "/sessions":
                            data = {"sessions": sessions}
                        elif path == "/interactions":
                            data = {"interactions": []}
                        elif path == "/assistant":
                            data = {"machine_id": "demo", "main": MAIN, "conversations": [PERSONA], "tasks": [], "memory": []}
                        elif path == f"/assistant/conversations/{MAIN}/feed":
                            data = {"items": feed, "plans": plans}
                        elif path == "/todos":
                            data = {"todos": todos}
                        elif path == f"/sessions/demo/{MAIN}/items":
                            data = {"items": items, "has_more": False}
                        elif path.endswith("/items"):
                            data = {"items": WORKER_ITEMS, "has_more": False}
                        elif re.fullmatch(r"/sessions/demo/[^/]+", path):
                            sid = path.rsplit("/", 1)[-1]
                            data = {"session": next((s for s in sessions if s["session_id"] == sid), PERSONA)}
                        elif path in ("/extensions", "/managers"):
                            data = {"extensions": [], "managers": []}
                        route.fulfill(status=200, content_type="application/json", body=json.dumps(data))

                    def socket(ws):
                        ws.send(json.dumps({"type": "hello", "server_version": "demo", "hub_name": "Demo hub", "features": []}))
                        ws.on_message(lambda message: ws.send(json.dumps({"type": "pong"})) if "app_ping" in message else None)

                    context.route("**/api/v1/**", api)
                    context.route_web_socket("**/api/v1/ws**", socket)
                    page = context.new_page()
                    errors = []
                    page.on("pageerror", lambda error: errors.append(str(error)))
                    page.set_default_timeout(15000)
                    page.goto(args.url.rstrip("/") + "/")
                    running = page.locator('[data-assistant-actionbar] [data-tray-entry="running"]')
                    expect(running).to_be_visible()
                    hint = page.locator("[data-dispatch-hint]")
                    expect(hint).to_have_count(1)
                    expect(hint).not_to_contain_text("In progress")
                    expect(hint).not_to_contain_text("Earlier unrelated task")

                    def capture(step):
                        expect(page.locator("vite-error-overlay")).to_have_count(0)
                        assert not errors, errors
                        page.evaluate("() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))")
                        page.screenshot(path=str(shots / f"{name}-{theme}-{step}.png"))

                    def opened(title, agent):
                        aside = page.get_by_role("complementary", name="Task")
                        expect(aside).to_be_visible()
                        expect(aside.locator("header")).to_contain_text(title)
                        # the Agent's chat (its transcript) when it has one, the task card before it starts
                        expect(aside.get_by_text("Reproduced it; working on the fix now.")).to_have_count(1 if agent else 0)
                        expect(running).to_have_attribute("aria-expanded", "false")
                        expect(page.locator("[data-assistant-actionbar] section")).to_have_count(0)

                    if name == "one":
                        expect(hint).to_have_text("Dispatched: Fix the sign-in crash· Open")
                        capture("line")
                        hint.click()
                        opened("Fix the sign-in crash", True)
                        capture("opened")
                    else:
                        links = hint.locator("[data-dispatched-task]")
                        expect(hint).to_contain_text("Dispatched 3")
                        expect(links).to_have_text(["Fix the sign-in crash", "Update the release notes", "Check the Android build"])
                        capture("line")
                        links.nth(1).click()
                        opened("Update the release notes", True)
                        capture("opened")
                        links.nth(2).click()
                        opened("Check the Android build", False)
                    print(f"  {name} {theme}: ok", flush=True)
                    context.close()
        finally:
            browser.close()
    print("PASS dispatched line browser verification", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("url")
    parser.add_argument("--shots", required=True)
    parser.add_argument("--chromium", default="/usr/bin/google-chrome")
    run(parser.parse_args())
