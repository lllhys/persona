#!/usr/bin/env python3
"""Verify the Assistant trays' room with many things waiting (#4656), using only synthetic HTTP/WS data.

Build web first. Run with a loopback production preview URL and --shots <directory>.
Wide and narrow, light and dark: with two plans waiting for Start (five tasks, briefs), an Agent's permission card, a
hand-opened Agent's question and a draft, the open Needs you tray takes most of the visible height instead of a fixed
short box; its header stays while only its list scrolls; the first plan's Start is in reach; the composer stays on
screen. Expanded, the tray fills the chat column (the conversation steps aside) and Restore gives the room back. In
progress with many running Agents scrolls the same way, and All opens the task board from its own chunk.
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


def brief(bid, goal, done_when, minutes, request=None, out=None):
    b = {"id": bid, "created_at": AT, "started_by": "owner", "goal": goal, "done_when": done_when, "criteria": done_when, "expect_minutes": minutes, "expect_minutes_now": minutes, "log": [], "clock_at": AT, "due_at": "2026-11-12T08:00:00Z"}
    if request:
        b["request"] = {"text": request, "at": AT, "conversation": MAIN, "message_id": "demo-message"}
    if out:
        b["out_of_bounds"] = out
    return b


def task(title, cwd, why, b=None, **more):
    return {"title": title, "prompt": "Synthetic task", "machine": "demo", "cwd": cwd, "why": why, "permission": "bypassPermissions", "workspace": "none", **({"brief": b} if b else {}), **more}


PLAN_A = {"id": "demo-plan-a", "conversation": MAIN, "created_at": AT, "state": "open", "tasks": [
    task("Fix the web first-screen budget", "/demo/project", "The web build and its deploy run on this computer.",
         brief("brief-a1", "The web build passes its first-screen budget again", ["The build passes", "The first screen is not larger than before"], 60,
               "Why does the deploy keep failing? Find out and fix it, and keep the first screen within its budget.", ["Do not raise the budget", "Do not turn the check off", "No unrelated refactors"])),
    task("Explain the failing deploy notice", "/demo/project", "The deploy logs are on this computer.",
         brief("brief-a2", "The owner knows why the deploy notice kept appearing", ["A short note says what failed and why"], 20)),
]}
PLAN_B = {"id": "demo-plan-b", "conversation": MAIN, "created_at": AT, "state": "open", "tasks": [
    task("Update the example release notes", "/demo/notes", "The notes live in this folder."),
    task("Check the example widget in landscape", "/demo/widget", "The simulator runs on this computer.", readonly=True),
    task("Clean up the example build cache", "/demo/project", "The cache is on this computer.", model="demo-model"),
]}
SESSIONS = [{"machine_id": "demo", "session_id": MAIN, "title": "Persona", "state": "idle", "status": "idle", "cwd": "/demo", "updated_at": AT, "started_by_persona": True, "capabilities": {"send": True, "interrupt": True}}]
# a permission card from one hand-opened Agent, a question from another, and many working ones for In progress
SESSIONS += [{"machine_id": "demo", "session_id": "ask-permission", "title": "Run the example checks", "state": "needs_you", "status": "blocked", "pending_interactions": 1, "cwd": "/demo/project", "updated_at": AT, "agent": "claude"},
             {"machine_id": "demo", "session_id": "ask-question", "title": "Tidy the example logs", "state": "needs_you", "status": "blocked", "pending_interactions": 1, "cwd": "/demo/project", "updated_at": AT, "agent": "claude"}]
WORKING = [f"Example job {i + 1}" for i in range(14)]
SESSIONS += [{"machine_id": "demo", "session_id": f"work-{i}", "title": title, "state": "working", "status": "working", "cwd": "/demo/project", "updated_at": AT, "agent": "claude", "activity": "Running the synthetic checks"} for i, title in enumerate(WORKING)]
INTERACTIONS = [
    {"id": "demo-permission", "machine_id": "demo", "session_id": "ask-permission", "kind": "permission", "agent": "claude", "tool_name": "Bash", "tool_input": {"command": "npm test", "description": "Run the synthetic checks"}, "created_at": AT},
    {"id": "demo-question", "machine_id": "demo", "session_id": "ask-question", "kind": "question", "agent": "claude", "created_at": AT, "questions": [{"question": "Remove the old example logs?", "options": [{"label": "Remove them"}, {"label": "Keep them"}]}]},
]
ITEMS = [{"id": "demo-user", "seq": 1, "role": "user", "kind": "text", "text": "Split the work and start it.", "ts": AT}, {"id": "demo-answer", "seq": 2, "role": "assistant", "kind": "text", "text": "Two plans wait for Start in Needs you.", "ts": AT}]
FEED = [{"id": "demo-plan-a-event", "conversation": MAIN, "at": AT, "kind": "plan", "ref": PLAN_A["id"]},
        {"id": "demo-plan-b-event", "conversation": MAIN, "at": AT, "kind": "plan", "ref": PLAN_B["id"]},
        {"id": "demo-draft", "conversation": MAIN, "at": AT, "kind": "message", "ref": "demo-draft-ref", "body": {"sender": "Demo sender", "chat": "Demo group", "text": "When are the checks ready?", "draft": "The demo checks are ready for review."}}]
FRAME = "() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))"
BOX = "e => { const b = e.getBoundingClientRect(); return { top: b.top, bottom: b.bottom, height: b.height }; }"


def run(args):
    shots = Path(args.shots)
    shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=args.chromium, args=["--no-sandbox"])
        try:
            for width, height in ((1280, 900), (390, 844)):
                for theme in ("light", "dark"):
                    context = browser.new_context(viewport={"width": width, "height": height}, locale="en-US", color_scheme=theme, reduced_motion="reduce")
                    context.add_init_script(f"localStorage.setItem('persona.token', 'demo'); localStorage.setItem('persona.language', 'en'); localStorage.setItem('persona.theme', '{theme}');")
                    context.add_init_script("const RealDate = Date; globalThis.Date = class extends RealDate { constructor(...args) { super(...(args.length ? args : ['2026-11-10T08:10:00Z'])); } static now() { return new RealDate('2026-11-10T08:10:00Z').getTime(); } };")
                    writes = []

                    def api(route):
                        path = urlparse(route.request.url).path.removeprefix("/api/v1")
                        data = {}
                        preview = re.fullmatch(r"/task-plans/([^/]+)/workspace-preview", path)
                        # nothing a card does (Start, Don't start, an answer, a send) runs from looking through the trays
                        if route.request.method != "GET" and re.search(r"/(start|dismiss|accept|message|messages)$|^/interactions/", path):
                            writes.append(path)
                        if preview:
                            # the card's workspace line: with "Not needed" the task runs in its own folder
                            plan = PLAN_A if preview.group(1) == PLAN_A["id"] else PLAN_B
                            tasks = json.loads(route.request.post_data or "{}").get("tasks", [])
                            data = {"plan": {**plan, "tasks": [{**t, "workspace_resolved": {"machine": t["machine"], "path": t["cwd"]}} for t in plan["tasks"]][:max(len(tasks), 1)]}}
                        elif path == "/machines":
                            data = {"machines": [MACHINE]}
                        elif path == "/sessions":
                            data = {"sessions": SESSIONS}
                        elif path == "/interactions":
                            data = {"interactions": INTERACTIONS}
                        elif path == "/assistant":
                            data = {"machine_id": "demo", "main": MAIN, "conversations": [SESSIONS[0]], "tasks": [], "memory": []}
                        elif path == f"/assistant/conversations/{MAIN}/feed":
                            data = {"items": FEED, "plans": {PLAN_A["id"]: PLAN_A, PLAN_B["id"]: PLAN_B}, "task_cards": []}
                        elif path == "/todos":
                            data = {"todos": []}
                        elif path.endswith("/items"):
                            data = {"items": ITEMS, "has_more": False}
                        elif re.fullmatch(r"/sessions/demo/[^/]+", path):
                            data = {"session": next(s for s in SESSIONS if s["session_id"] == path.rsplit("/", 1)[-1])}
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
                    bar = page.locator("[data-assistant-actionbar]")
                    expect(bar).to_be_visible()
                    composer = page.get_by_role("textbox", name="Message", exact=True)
                    expect(composer).to_be_visible()
                    tag = f"{width}-{theme}"

                    def capture(name):
                        expect(page.locator("vite-error-overlay")).to_have_count(0)
                        assert not errors, errors
                        assert page.evaluate("() => document.documentElement.scrollWidth <= innerWidth"), "nothing overflows sideways"
                        c = composer.evaluate(BOX)
                        assert c["bottom"] <= height and c["height"] > 0, f"the composer stays on screen: {c}"
                        page.evaluate(FRAME)
                        page.screenshot(path=str(shots / f"{tag}-{name}.png"))

                    needs = bar.locator('[data-tray-entry="needs"]')
                    expect(needs).to_have_text(re.compile(r"Needs you\s*5"))
                    needs.click()
                    tray = bar.locator("section.assistant-action-tray")
                    head = tray.locator("> div").first
                    lst = tray.locator("[data-tray-list]")
                    expect(tray.get_by_text("Fix the web first-screen budget")).to_be_visible()
                    expect(tray.get_by_text("Clean up the example build cache")).to_be_attached()
                    expect(tray.locator("[data-workspace-path] [role=alert]")).to_have_count(0)
                    expect(tray.locator("[data-workspace-path]").first).to_have_text("/demo/project")
                    expect(tray.locator("[data-plan-brief]")).to_have_count(2)
                    expect(tray.get_by_role("button", name="Allow", exact=True)).to_be_attached()
                    expect(tray.locator('[data-testid="message-card"]')).to_have_count(1)
                    page.evaluate(FRAME)
                    t = tray.evaluate(BOX)
                    entry = needs.evaluate(BOX)
                    # #4656: the tray follows the viewport, not a 20rem / 40dvh box: most of what is visible
                    assert t["height"] >= 0.45 * height, f"the open tray takes most of the visible height: {t} of {height}"
                    assert t["bottom"] <= entry["top"], f"the tray sits over its entries: {t} {entry}"
                    h = head.evaluate(BOX)
                    l = lst.evaluate(BOX)
                    print(f"  {tag}: tray {round(t['height'])} px of {height} (was at most 320)", flush=True)
                    capture("needs")
                    # the first plan (under the two Agents asking): scrolled to its top, its Start is in reach, under the
                    # header and inside the list, sticking to the list's bottom when the card is taller
                    # a plan is folded to a line until opened (#5569)
                    tray.locator('button[data-needs-row^="plan:"][aria-expanded="false"]').first.click()
                    plan = tray.locator("[data-plan-actions]").first.locator("xpath=..")
                    plan.evaluate("e => { const list = e.closest('[data-tray-list]'); list.scrollTop += e.getBoundingClientRect().top - list.getBoundingClientRect().top - 8; }")
                    page.evaluate(FRAME)
                    start = tray.get_by_role("button", name="Start 2 tasks", exact=True)
                    s = start.evaluate(BOX)
                    assert s["top"] >= h["bottom"] and s["bottom"] <= l["bottom"] + 0.5, f"Start is in reach: {s} head {h} list {l}"
                    assert abs(head.evaluate(BOX)["top"] - h["top"]) < 0.5, "the header stays while the list scrolls"
                    capture("needs-plan")
                    # only the list scrolls; the header (All, Expand, Close) stays
                    assert lst.evaluate("e => e.scrollHeight > e.clientHeight"), "many things: the list scrolls"
                    lst.evaluate("e => e.scrollTo(0, e.scrollHeight)")
                    page.evaluate(FRAME)
                    assert lst.evaluate("e => e.scrollTop") > 0
                    assert abs(head.evaluate(BOX)["top"] - h["top"]) < 0.5, "the header stays while the list scrolls"
                    expect(tray.get_by_role("button", name="Start 3 tasks", exact=True)).to_be_in_viewport()
                    capture("needs-scrolled")
                    lst.evaluate("e => e.scrollTo(0, 0)")
                    # expanded: the tray fills the chat column, the conversation steps aside, the composer stays
                    tray.get_by_role("button", name="Expand", exact=True).click()
                    expect(tray).to_have_attribute("data-expanded", "true")
                    page.evaluate(FRAME)
                    big = tray.evaluate(BOX)
                    chat = page.locator("[data-chat-scroll]").first.evaluate("e => e.clientHeight")
                    assert big["height"] > t["height"] + 40 or t["height"] >= 0.7 * height, f"expanding gives the tray more room: {t} -> {big}"
                    assert chat <= 2, f"the conversation steps aside while the tray is expanded: {chat}"
                    assert big["bottom"] <= needs.evaluate(BOX)["top"], "still over its entries"
                    print(f"  {tag}: expanded {round(big['height'])} px", flush=True)
                    capture("needs-expanded")
                    tray.get_by_role("button", name="Restore size", exact=True).click()
                    expect(tray).not_to_have_attribute("data-expanded", "true")
                    page.evaluate(FRAME)
                    assert abs(tray.evaluate(BOX)["height"] - t["height"]) < 1, "Restore gives the room back"
                    assert page.locator("[data-chat-scroll]").first.evaluate("e => e.clientHeight") > 40, "the conversation is back"
                    # In progress: many running Agents, the same fixed header and scrolling list
                    running = bar.locator('[data-tray-entry="running"]')
                    running.click()
                    expect(running).to_have_attribute("aria-expanded", "true")
                    expect(tray.locator("[data-running-agent]")).to_have_count(len(WORKING))
                    expect(tray).not_to_have_attribute("data-expanded", "true")
                    page.evaluate(FRAME)
                    assert tray.evaluate(BOX)["height"] >= 0.45 * height, "In progress gets the same room"
                    capture("running")
                    # All: the task board, now a chunk loaded when it opens
                    tray.get_by_role("button", name="All", exact=True).click()
                    dialog = page.get_by_role("dialog")
                    expect(dialog).to_be_visible()
                    expect(dialog.locator('[data-testid="task-board"]')).to_be_visible()
                    capture("board")
                    page.keyboard.press("Escape")
                    expect(dialog).to_have_count(0)
                    assert not writes, f"nothing was sent: {writes}"
                    print(f"PASS {tag}: the tray follows the viewport, its header stays, Start in reach, expand and restore, board loads", flush=True)
                    context.close()
        finally:
            browser.close()
    print("PASS Assistant tray room browser verification", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("url")
    parser.add_argument("--shots", required=True)
    parser.add_argument("--chromium", default="/usr/bin/google-chrome")
    run(parser.parse_args())
