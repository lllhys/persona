#!/usr/bin/env python3
"""Exercise the real Assistant tray against shared synthetic activity fixtures, never a live hub."""
import argparse
import copy
import json
import re
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import expect, sync_playwright

ROOT = Path(__file__).resolve().parents[2]
AT = "2026-11-10T08:00:00Z"
MAIN = "main-chat"
DEVICE = "fixture-device"


def fixtures():
    cases = json.loads((ROOT / "spec/fixtures/entries/cases.json").read_text())["cases"]
    names = ["background_commands_and_monitor_in_progress_use_working_mark", "workflow_summary_in_progress_keeps_phase_and_progress",
             "subagent_detail_counts_once_in_its_task", "external_wait_without_background_keeps_clock"]
    titles = ["Background checks", "Workflow review", "Child agent checks", "Wait for deployment"]
    todos, sessions = [], []
    for i, (name, title) in enumerate(zip(names, titles)):
        case = copy.deepcopy(next(c for c in cases if c["name"] == name))
        todo, session = case["input"]["todos"][0], case["input"]["sessions"][0]
        todo.update(id=f"background-{i}", title=title)
        todo["agents"][0]["session_id"] = f"worker-{i}"
        session.update(session_id=f"worker-{i}", title=title, rev=1)
        # The production tray gets summaries, including for a session outside the open chat window.
        session.update(waiting=True, background_tasks_count=2 if i == 0 else 0, running_subagents=2 if i == 2 else 0)
        for field in ("background", "subagents", "workflows"):
            session.pop(field, None)
        todos.append(todo); sessions.append(session)
    manual = copy.deepcopy(next(c for c in cases if c["name"] == "manual_agent_workflow_is_one_in_progress_row")["input"]["sessions"][0])
    manual.update(title="Manual workflow", rev=1)
    sessions.append(manual)
    main = dict(machine_id=DEVICE, session_id=MAIN, title="Persona", cwd="/demo/project", state="idle", status="idle", assistant=True,
                started_by_persona=True, updated_at=AT, waiting=False, background_tasks_count=0, rev=1)
    return todos, [main] + sessions


def run(args):
    shots = Path(args.shots); shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=args.chromium, args=["--no-sandbox"])
        try:
            for width in (1280, 390):
                for theme in ("light", "dark"):
                    for text_size in ("standard", "large"):
                        todos, sessions = fixtures()
                        writes, sockets, errors = [], [], []
                        revision = [1]
                        context = browser.new_context(viewport={"width": width, "height": 1000}, locale="zh-CN", color_scheme=theme, reduced_motion="reduce")
                        context.add_init_script(f"localStorage.setItem('persona.token','fixture'); localStorage.setItem('persona.language','zh-Hans'); localStorage.setItem('persona.theme','{theme}'); localStorage.setItem('persona.text-size','{text_size}');")
                        context.add_init_script("const RealDate = Date; globalThis.Date = class extends RealDate { constructor(...args) { super(...(args.length ? args : ['2026-11-10T09:00:00Z'])); } static now() { return new RealDate('2026-11-10T09:00:00Z').getTime(); } };")

                        def api(route):
                            parsed = urlparse(route.request.url)
                            path = parsed.path.removeprefix("/api/v1")
                            # The existing automatic briefing uses POST even on a read-only UI visit. It is synthetic here.
                            if route.request.method == "POST" and path == f"/assistant/conversations/{MAIN}/today":
                                route.fulfill(status=200, content_type="application/json", body=json.dumps(dict(id="fixture-today", conversation=MAIN, at=AT, kind="today", body={})))
                                return
                            if route.request.method != "GET":
                                writes.append(path)
                                route.fulfill(status=405, body="Read-only fixture"); return
                            data = {}
                            if path == "/machines":
                                data = {"machines": [dict(id=DEVICE, name="Fixture computer", online=True, os="linux")]}
                            elif path == "/sessions":
                                query = parse_qs(parsed.query)
                                rows = sessions
                                if query.get("session_id"):
                                    rows = [s for s in rows if s["session_id"] == query["session_id"][0]]
                                data = dict(sessions=rows, revision=f"r{revision[0]}", total=len(rows), has_more=False, remove=[])
                            elif path == "/interactions":
                                data = {"interactions": []}
                            elif path == "/assistant":
                                data = dict(machine_id=DEVICE, main=MAIN, conversations=[sessions[0]], tasks=[], memory=[])
                            elif path == f"/assistant/conversations/{MAIN}/feed":
                                data = dict(items=[], plans={}, task_cards=[])
                            elif path == "/todos":
                                data = {"todos": todos}
                            elif path.endswith("/items"):
                                data = dict(items=[dict(id="input", seq=1, role="user", kind="text", text="Show the ongoing work.", ts=AT),
                                                   dict(id="reply", seq=2, role="assistant", kind="text", text="The task tray shows what is still running.", ts=AT)], has_more=False)
                            elif re.fullmatch(r"/sessions/[^/]+/[^/]+", path):
                                data = {"session": next(s for s in sessions if s["session_id"] == path.rsplit("/", 1)[-1])}
                            elif path in ("/extensions", "/managers"):
                                data = dict(extensions=[], managers=[])
                            route.fulfill(status=200, content_type="application/json", body=json.dumps(data))

                        def socket(ws):
                            sockets.append(ws)
                            ws.send(json.dumps(dict(type="hello", server_version="fixture", hub_name="Fixture hub", features=["sessions_list", "sessions_lookup"])))
                            ws.on_message(lambda message: ws.send('{"type":"pong"}') if "app_ping" in message else None)

                        context.route("**/api/v1/**", api)
                        context.route_web_socket("**/api/v1/ws**", socket)
                        page = context.new_page(); page.set_default_timeout(20000)
                        page.on("pageerror", lambda error: errors.append(str(error)))
                        page.goto(args.url.rstrip("/") + "/")
                        bar = page.locator("[data-assistant-actionbar]")
                        expect(bar).to_be_visible()
                        bar.locator('[data-tray-entry="running"]').click()
                        expect(bar.locator('[data-tray-entry="running"]')).to_have_text("进行中5")
                        bg = bar.locator('[data-task-slot="task:background-0"]')
                        wf = bar.locator('[data-task-slot="task:background-1"]')
                        child = bar.locator('[data-task-slot="task:background-2"]')
                        wait = bar.locator('[data-task-slot="task:background-3"]')
                        expect(bg).to_contain_text("后台任务 2 个在跑")
                        expect(wf).to_contain_text("Adversarial review · 5/6+")
                        expect(child).to_contain_text("子 Agent 2 个在跑")
                        for row in (bg, wf, child):
                            expect(row.locator('[data-task-mark="working"]')).to_have_count(1)
                            expect(row.locator('[data-task-mark="waiting"]')).to_have_count(0)
                        expect(wait.locator('[data-task-mark="waiting"]')).to_have_count(1)
                        expect(bar.locator('[data-running-agent="fixture-device/manual-worker"]')).to_contain_text("Adversarial review · 5/6+")
                        # The footer's true count must be visible even on the narrow layout.
                        for row in (bg, wf):
                            row.scroll_into_view_if_needed()
                            assert row.evaluate("e => { const r=e.getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth; }")
                        bg.scroll_into_view_if_needed()
                        page.evaluate("() => new Promise(r => requestAnimationFrame(() => requestAnimationFrame(r)))")
                        page.screenshot(path=str(shots / f"{width}-{theme}-{text_size}.png"))
                        wf.evaluate("e => globalThis.activityRow = e")
                        sessions[2]["workflow_progress"].update(phase="Final check", agents_done=6, agents_started=8)
                        sessions[2]["rev"] = 2; revision[0] += 1
                        for ws in sockets:
                            ws.send(json.dumps(dict(type="sessions_list", revision=f"r{revision[0]}")))
                        expect(wf).to_contain_text("Final check · 6/8+")
                        assert wf.evaluate("e => e === globalThis.activityRow")
                        sessions[2].update(state="idle", status="idle", background_working=False, running_workflows=0,
                                           work=dict(phase="blocked", on="external", line="Wait for deployment"), rev=3)
                        sessions[2].pop("workflow_progress", None); revision[0] += 1
                        for ws in sockets:
                            ws.send(json.dumps(dict(type="sessions_list", revision=f"r{revision[0]}")))
                        expect(wf.locator('[data-task-mark="waiting"]')).to_have_count(1)
                        expect(wf).not_to_contain_text("Final check")
                        # No Agent send, approval or task mutation; the existing auto-briefing POST is stubbed above.
                        assert not writes, writes
                        assert not errors, errors
                        print(f"PASS: {width}px {theme} {text_size}; background/workflow/subagent spinner, real-wait clock, phase update and completion, zero writes", flush=True)
                        context.close()
        finally:
            browser.close()
    print("PASS: Assistant background work browser verification", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True)
    parser.add_argument("--shots", required=True)
    parser.add_argument("--chromium", required=True)
    run(parser.parse_args())
