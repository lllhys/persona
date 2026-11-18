#!/usr/bin/env python3
"""Verify a plan waiting for Start in the main conversation (#8193) in a real browser, with synthetic HTTP/WS data only.

Build web first. Run with a loopback production preview URL and --shots <directory>:

    python3 web/e2e/plan-card-chat.py http://127.0.0.1:<port> --shots <directory> [--chromium <executable>]

The plan the assistant made is its card in the chat, where it was made (as iOS's FeedLine draws it): its task, the
computer, folder and model chips, then Start, Edit and Don't start. The Pending tray still has it as a row. Edit opens
the dialog that changes the computer and the model; Don't start goes to the hub and the card becomes its one-line
outcome. Light and dark, wide and phone width; English.
"""
import argparse
import json
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

AT = "2026-11-10T08:00:00Z"
MAIN = "demo-main"
MACHINES = [{"id": "demo", "name": "Computer A", "online": True, "os": "linux"},
            {"id": "demo-b", "name": "Computer B", "online": True, "os": "darwin"}]
PERSONA = {"machine_id": "demo", "session_id": MAIN, "title": "Persona", "state": "idle", "status": "idle", "cwd": "/demo", "updated_at": AT, "started_by_persona": True, "capabilities": {"send": True, "interrupt": True}}
PLAN_ID = "demo-plan"


def plan(state="open"):
    return {"id": PLAN_ID, "conversation": MAIN, "created_at": AT, "updated_at": AT, "state": state,
            "tasks": [{"title": "Tidy up the release checklist", "prompt": "Synthetic task", "machine": "demo", "cwd": "/demo/project",
                       "permission": "acceptEdits", "why": "Computer A has the project checked out."}]}


def run(args):
    shots = Path(args.shots)
    shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=args.chromium, args=["--no-sandbox"])
        try:
            for name, (width, height), theme in ((n, size, th) for n, size in (("wide", (1280, 820)), ("phone", (390, 844))) for th in ("light", "dark")):
                state = {"plan": plan(), "posts": []}
                items = [{"id": "demo-user", "seq": 1, "role": "user", "kind": "text", "text": "The release checklist is a mess.", "ts": AT},
                         {"id": "demo-answer", "seq": 2, "role": "assistant", "kind": "text", "text": "I drafted a task to tidy it up; start it when you are ready.", "ts": AT}]
                feed = [{"id": "demo-plan-event", "conversation": MAIN, "at": AT, "kind": "plan", "ref": PLAN_ID, "body": {"plan": plan()}}]
                context = browser.new_context(viewport={"width": width, "height": height}, locale="en-US", color_scheme=theme, reduced_motion="reduce")
                context.add_init_script(f"localStorage.setItem('persona.token', 'demo'); localStorage.setItem('persona.language', 'en'); localStorage.setItem('persona.theme', '{theme}');")
                context.add_init_script("const RealDate = Date; globalThis.Date = class extends RealDate { constructor(...args) { super(...(args.length ? args : ['2026-11-10T08:10:00Z'])); } static now() { return new RealDate('2026-11-10T08:10:00Z').getTime(); } };")

                def api(route):
                    path = urlparse(route.request.url).path.removeprefix("/api/v1")
                    data = {}
                    if route.request.method == "POST" and path == f"/task-plans/{PLAN_ID}/dismiss":
                        state["posts"].append(path)
                        state["plan"] = plan("dismissed")
                        data = {"plan": state["plan"]}
                    elif path == "/machines":
                        data = {"machines": MACHINES}
                    elif path == "/sessions":
                        data = {"sessions": [PERSONA]}
                    elif path == "/interactions":
                        data = {"interactions": []}
                    elif path == "/assistant":
                        data = {"machine_id": "demo", "main": MAIN, "conversations": [PERSONA], "tasks": [], "memory": []}
                    elif path == f"/assistant/conversations/{MAIN}/feed":
                        data = {"items": feed, "plans": {PLAN_ID: state["plan"]}}
                    elif path == "/todos":
                        data = {"todos": []}
                    elif path == f"/sessions/demo/{MAIN}/items":
                        data = {"items": items, "has_more": False}
                    elif path == f"/sessions/demo/{MAIN}":
                        data = {"session": PERSONA}
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

                def capture(step):
                    expect(page.locator("vite-error-overlay")).to_have_count(0)
                    assert not errors, errors
                    page.evaluate("() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))")
                    page.screenshot(path=str(shots / f"{name}-{theme}-{step}.png"))

                # the card in the chat itself, outside the dock's trays, with its three actions on one line each
                tray = page.locator("[data-assistant-actionbar]")
                in_chat = page.locator("[data-plan-actions]:not([data-assistant-actionbar] [data-plan-actions])")
                expect(in_chat).to_have_count(1)
                expect(in_chat).to_be_visible()
                actions = in_chat.locator("button")
                expect(actions).to_have_count(3)
                for label in ("Start", "Edit", "Don't start"):
                    expect(in_chat.get_by_role("button", name=label, exact=True)).to_be_visible()
                for i in range(3):
                    box = actions.nth(i).bounding_box()
                    assert box["height"] < 48, box  # one line: the text never wraps
                expect(page.get_by_text("Tidy up the release checklist").first).to_be_visible()
                capture("chat")

                # still a row of the Pending tray, counted there
                pending = tray.locator('[data-tray-entry="needs"]')
                expect(pending).to_contain_text("1")
                pending.click()
                expect(tray.locator("section")).to_be_visible()
                expect(tray.locator("section")).to_contain_text("Tidy up the release checklist")
                capture("tray")
                pending.click()
                expect(tray.locator("section")).to_have_count(0)

                # Edit: the computer and the model can change before it starts
                in_chat.get_by_role("button", name="Edit", exact=True).click()
                dialog = page.get_by_role("dialog")
                expect(dialog).to_be_visible()
                expect(dialog).to_contain_text("Computer")
                capture("edit")
                page.keyboard.press("Escape")
                expect(dialog).to_have_count(0)

                # Don't start: the hub dismisses it and the card becomes its outcome, with no actions left
                in_chat.get_by_role("button", name="Don't start", exact=True).click()
                expect(page.locator("[data-plan-actions]")).to_have_count(0)
                assert state["posts"] == [f"/task-plans/{PLAN_ID}/dismiss"], state["posts"]
                capture("dismissed")
                print(f"  {name} {theme}: ok", flush=True)
                context.close()
        finally:
            browser.close()
    print("PASS plan card in the chat browser verification", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("url")
    parser.add_argument("--shots", required=True)
    parser.add_argument("--chromium", default="/usr/bin/google-chrome")
    run(parser.parse_args())
