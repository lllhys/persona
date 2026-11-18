#!/usr/bin/env python3
"""Capture the real web app against synthetic API/WS fixtures; no hub required.

python3 web/e2e/visual_noise.py http://127.0.0.1:51783 --shots <directory> --phase before|after
Use the same baseline worktree, viewport and fixtures for both phases.
"""
import argparse
import json
import re
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

AT = "2026-11-07T00:00:00Z"


def contrast_check(locator, property_name="color"):
    """Measure resolved browser colors on the actual sent-message fill."""
    ratio = locator.evaluate("""(e, property) => {
        const rgb = value => value.match(/[\\d.]+/g).slice(0, 3).map(Number);
        const luminance = value => rgb(value).map(c => c / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((n, c, i) => n + c * [.2126, .7152, .0722][i], 0);
        const bg = luminance(getComputedStyle(e.closest('.bg-user-bubble')).backgroundColor);
        const fg = luminance(getComputedStyle(e)[property]);
        return (Math.max(bg, fg) + .05) / (Math.min(bg, fg) + .05);
    }""", property_name)
    assert ratio >= 4.5, f"{property_name} contrast {ratio:.2f}:1 is below 4.5:1"
    return ratio


MACHINE = {"id": "demo", "name": "Demo computer", "online": True, "os": "linux", "host": "herdr"}
SESSIONS = [
    {"machine_id": "demo", "session_id": "review", "title": "Review the demo change", "name": "Review the demo change", "state": "needs_you", "status": "needs_you", "agent": "claude", "online": True, "cwd": "/demo/project", "updated_at": AT, "pending_interactions": 2, "capabilities": {"approve": True, "send": True}},
    {"machine_id": "demo", "session_id": "attention", "title": "Choose the next step", "name": "Choose the next step", "state": "needs_you", "status": "needs_you", "agent": "claude", "online": True, "cwd": "/demo/project", "updated_at": AT, "pending_interactions": 0, "attention_reason": "The draft is ready. Confirm the next step when convenient."},
    {"machine_id": "demo", "session_id": "working", "title": "Run the demo checks", "name": "Run the demo checks", "state": "working", "status": "working", "agent": "claude", "online": True, "cwd": "/demo/project", "updated_at": AT, "state_since": AT, "pending_interactions": 0},
    {"machine_id": "demo", "session_id": "done", "title": "Read the test summary", "name": "Read the test summary", "state": "unread", "status": "unread", "agent": "claude", "online": True, "cwd": "/demo/project", "updated_at": AT, "pending_interactions": 0},
    {"machine_id": "demo", "session_id": "status", "title": "Demo workflow statuses", "state": "idle", "status": "idle", "agent": "claude", "online": True, "cwd": "/demo/project", "updated_at": AT, "pending_interactions": 0, "workflows": [
        {"task_id": "demo-" + status, "tool_use_id": "call-" + status, "name": name, "summary": "Synthetic workflow status preview", "status": status, "running": status == "running", "agents_started": 1, "agents_done": int(status == "completed"), "agents_running": int(status == "running"), "started_at": AT}
        for status, name in (("completed", "Demo checks passed"), ("stalled", "Demo workflow paused"), ("running", "Demo AI work"))
    ]},
]
SESSIONS[0].update(model="demo-model", context_tokens=95000, context_window=100000)
ASSISTANT_SESSION = {**SESSIONS[-1], "session_id": "receipt", "title": "Demo completion receipt", "workflows": [], "started_by_persona": True}
INTERACTIONS = [
    {"id": "demo-permission", "machine_id": "demo", "session_id": "review", "session_title": "Review the demo change", "kind": "permission", "agent": "claude", "tool_name": "Bash", "tool_input": {"command": "npm test", "description": "Run the demo unit tests"}, "summary": "Run the demo unit tests", "created_at": AT},
    {"id": "demo-plan", "machine_id": "demo", "session_id": "review", "session_title": "Review the demo change", "kind": "plan", "agent": "claude", "plan": "## Demo change\n1. Read the existing components.\n2. Use neutral surfaces and small status details.\n3. Run tests and check both appearances.", "created_at": AT},
]


def run(args):
    shots = Path(args.shots)
    shots.mkdir(parents=True, exist_ok=True)
    with sync_playwright() as p:
        browser = p.chromium.launch(executable_path=args.chromium, args=["--no-sandbox"])
        for theme in ("light", "dark"):
            context = browser.new_context(viewport={"width": 1280, "height": 1100}, device_scale_factor=1, locale="en-US", color_scheme=theme, reduced_motion="reduce")
            context.add_init_script("localStorage.setItem('persona.token', 'demo'); localStorage.setItem('persona.language', 'en'); localStorage.setItem('persona.theme', '" + theme + "'); localStorage.setItem('agent.group', 'status');")
            context.add_init_script("const RealDate = Date; const fixed = 1794009840000; globalThis.Date = class extends RealDate { constructor(...args) { super(...(args.length ? args : [fixed])); } static now() { return fixed; } };")
            remaining = list(INTERACTIONS)
            replies = []

            def api(route):
                path = urlparse(route.request.url).path.removeprefix("/api/v1")
                data = {}
                status = 200
                if path == "/machines":
                    data = {"machines": [MACHINE]}
                elif path == "/sessions":
                    data = {"sessions": SESSIONS}
                elif path == "/interactions":
                    data = {"interactions": remaining}
                elif path == "/assistant":
                    data = {"machine_id": "demo", "main": "receipt", "conversations": [ASSISTANT_SESSION], "tasks": [], "memory": []}
                elif path == "/assistant/conversations/receipt/feed":
                    data = {"items": [{"id": "demo-completion", "conversation": "receipt", "at": AT, "kind": "receipt", "body": {"state": "done", "title": "Demo checks passed", "summary": "All synthetic demo checks completed successfully."}}]}
                elif path == "/todos":
                    data = {"todos": []}
                elif path == "/inbox":
                    data = {"items": [{"id": "demo-thread", "source": "demo-chat", "kind": "group", "account": "demo", "account_name": "Demo chat", "title": "Demo group", "preview": "Demo reply with a quote and file", "count": 1, "tier": "reference", "connector": {"thread": "demo-group"}}]}
                elif path == "/connectors/demo-chat/thread":
                    data = {"chat": {"id": "demo-group", "name": "Demo group", "kind": "group", "members": 3}, "messages": [{"id": "demo-own", "at": AT, "sender": {"id": "demo-author", "name": "Demo author"}, "mine": True, "text": "Demo reply", "reply_to": {"id": "demo-earlier", "text": "Demo quote"}, "media": [{"kind": "file", "name": "demo.txt", "size": 120}]}]}
                elif path == "/agents":
                    data = {"agents": []}
                elif re.fullmatch(r"/sessions/demo/[^/]+", path):
                    sid = path.rsplit("/", 1)[-1]
                    data = {"session": ASSISTANT_SESSION if sid == "receipt" else next(s for s in SESSIONS if s["session_id"] == sid)}
                elif path.endswith("/items"):
                    items = [{"id": "demo-user", "seq": 1, "role": "user", "kind": "text", "text": "Prepare a small demo change and ask before running checks.", "ts": AT}]
                    if "/status/" in path:
                        for index, run_status in enumerate(("completed", "stalled", "running"), start=2):
                            items.append({"id": "demo-call-" + run_status, "seq": index, "role": "assistant", "kind": "tool_use", "tool_name": "Workflow", "tool_use_id": "call-" + run_status, "tool_input": {"description": "Demo " + run_status}, "ts": AT})
                    items.append({"id": "demo-answer", "seq": 5, "role": "assistant", "kind": "text", "text": "The demo change is ready for review. The plan and permission are below.", "ts": AT})
                    data = {"items": items, "has_more": False}
                elif path.startswith("/interactions/") and route.request.method == "POST":
                    replies.append(json.loads(route.request.post_data or "{}"))
                    ixid = path.split("/")[2]
                    remaining[:] = [ix for ix in remaining if ix["id"] != ixid]
                    data = {"ok": True}
                elif path == "/extensions/ui":
                    data = {"slots": []}
                elif path in ("/extensions", "/managers"):
                    data = {"extensions": [], "managers": []}
                route.fulfill(status=status, content_type="application/json", body=json.dumps(data))

            context.route("**/api/v1/**", api)

            def socket(ws):
                ws.send(json.dumps({"type": "hello", "server_version": "demo", "hub_name": "Demo hub", "features": []}))
                ws.on_message(lambda message: ws.send(json.dumps({"type": "pong"})) if "app_ping" in message else None)

            context.route_web_socket("**/api/v1/ws**", socket)
            page = context.new_page()
            errors = []
            page.on("pageerror", lambda error: errors.append(str(error)))
            page.set_default_timeout(15000)
            for name, path in (("overview", "/overview"), ("approvals", "/s/demo/review"), ("statuses", "/s/demo/status"), ("success", "/persona/receipt"), ("attention", "/s/demo/attention"), ("connector", "/inbox/demo-thread")):
                page.goto(args.url.rstrip("/") + path)
                expect(page.locator("main")).to_be_visible()
                if name == "overview":
                    expect(page.locator("main").get_by_text("Review the demo change", exact=True).first).to_be_visible()
                elif name == "approvals":
                    expect(page.get_by_role("button", name="Approve", exact=True)).to_be_visible()
                    expect(page.get_by_role("button", name="Allow", exact=True)).to_be_visible()
                elif name == "statuses":
                    for status in ("completed", "stalled", "running"):
                        expect(page.locator(f'[data-workflow-status="{status}"]').first).to_be_visible()
                elif name == "success":
                    # a task's receipt is its row in the trays, not a card in the chat (#8237)
                    expect(page.locator('[data-receipt]')).to_have_count(0)
                elif name == "attention":
                    expect(page.locator('[data-testid="chat-attention"]')).to_be_visible()
                else:
                    expect(page.locator('[data-mid="demo-own"]')).to_be_visible()
                    expect(page.locator('[data-mid="demo-own"]').get_by_text("demo.txt", exact=False)).to_be_visible()
                page.wait_for_timeout(300)
                expect(page.locator("vite-error-overlay")).to_have_count(0)
                assert not errors, errors
                if args.phase == "after":
                    expect(page.locator(".text-red, .bg-red-tint")).to_have_count(0)
                    if name == "statuses":
                        assert page.locator(".bg-red").evaluate_all("nodes => nodes.every(e => e.getBoundingClientRect().width === 0)"), "no visible failed-workflow progress segment"
                    else:
                        expect(page.locator(".bg-red")).to_have_count(0)
                    if name == "approvals":
                        for label in ("Allow", "Approve"):
                            button = page.get_by_role("button", name=label, exact=True)
                            assert button.evaluate("e => { const card=e.closest('.bg-surface.shadow-card'); const footer=card.querySelector('.bg-inset'); return !!card && !!footer && !card.querySelector('.bg-orange-tint, .bg-red-tint, .bg-ai-tint'); }"), "approval card and footer remain neutral"
                        expect(page.locator('[role="progressbar"] .bg-orange')).to_have_count(1)
                        expect(page.locator('[role="progressbar"] .bg-red')).to_have_count(0)
                    if name == "statuses":
                        for status, token in (("completed", "green"), ("stalled", "orange"), ("running", "ai")):
                            chip = page.locator(f'[data-workflow-status="{status}"]').first
                            assert chip.evaluate("(e, token) => { const n=document.createElement('i'); n.style.background='var(--'+token+'-tint)'; document.body.append(n); const ok=getComputedStyle(e).backgroundColor===getComputedStyle(n).backgroundColor; n.remove(); return ok; }", token), "workflow chip uses its restored semantic tint"
                    if name == "connector":
                        own = page.locator('[data-mid="demo-own"]')
                        expect(own.locator('[class*="text-white"]')).to_have_count(0)
                        quote = own.locator("button.border-l-2")
                        body_ratio = contrast_check(own.locator(".bg-user-bubble"))
                        quote_ratio = contrast_check(quote)
                        file_ratio = contrast_check(own.locator("p.text-ink-2"))
                        # A keyboard event establishes focus-visible mode before targeting the quote.
                        page.keyboard.press("Tab")
                        quote.focus()
                        assert quote.evaluate("e => e === document.activeElement && e.matches(':focus-visible')"), "quote has keyboard-visible focus"
                        expect(quote).to_have_css("outline-color", page.locator("body").evaluate("e => getComputedStyle(e).color"))
                        expect(quote).to_have_css("outline-style", "solid")
                        expect(quote).to_have_css("outline-width", "2px")
                        focus_ratio = contrast_check(quote, "outlineColor")
                        page.screenshot(path=str(shots / f"after-connector-focus-{theme}.png"))
                        quote.evaluate("e => e.blur()")
                        print(f"PASS {theme} bubble contrast: body={body_ratio:.2f}, quote={quote_ratio:.2f}, attachment={file_ratio:.2f}, focus={focus_ratio:.2f}", flush=True)
                        quote.hover()
                        expect(quote).to_have_css("color", page.locator("body").evaluate("e => getComputedStyle(e).color"))
                        page.mouse.move(0, 0)
                    if name == "attention":
                        neutral = page.locator('[data-testid="chat-attention"]').evaluate("e => { const n=document.createElement('i'); n.style.background='var(--inset)'; document.body.append(n); const ok=getComputedStyle(e).backgroundColor===getComputedStyle(n).backgroundColor; n.remove(); return ok; }")
                        assert neutral, "attention banner has a neutral surface"
                page.screenshot(path=str(shots / f"{args.phase}-{name}-{theme}.png"))
                print(f"PASS {args.phase} {name} {theme}: real app, synthetic fixture, no page errors", flush=True)
            # Interact with the real approval control, not a rendered component gallery.
            page.goto(args.url.rstrip("/") + "/s/demo/review")
            page.get_by_role("button", name="Allow", exact=True).click()
            page.wait_for_timeout(300)
            assert any(reply.get("allow") is True for reply in replies), replies
            page.get_by_role("button", name="Approve", exact=True).click()
            page.wait_for_timeout(300)
            assert any(reply.get("approve") is True for reply in replies), replies
            print(f"PASS {theme}: Allow and Approve send the unchanged approval requests", flush=True)
            context.close()
        browser.close()
    print("PASS visual-noise browser verification", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("url")
    parser.add_argument("--shots", required=True)
    parser.add_argument("--phase", choices=["before", "after"], required=True)
    parser.add_argument("--chromium", default="/usr/bin/google-chrome")
    run(parser.parse_args())
