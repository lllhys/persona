#!/usr/bin/env python3
"""Extension-rendered mail fixture for InboxDirectMailSourceTests.

The Mac runner starts this foreground process and owns its lifetime. Use --port 0
(or an explicitly free port), allow 15 seconds for the READY line, and pass its URL
as TEST_RUNNER_PERSONA_MOCK_HUB. Stop only the printed PID with SIGTERM, wait up to
five seconds, and SIGKILL that same PID only if it remains. The fixture creates and
removes its own temporary HOME; it never reads or changes production hub state.

Only mail:acc1:t1:501 gains an inbox_item page here. Its real open action answers
with that item's source URL; every other request and extension action uses the
unmodified stock mock's data and handlers.
"""
import argparse
import importlib.util
import os
from pathlib import Path
import signal
import sys
import tempfile


MAIL_ITEM = "mail:acc1:t1:501"
MAIL_SOURCE_URL = "persona://inbox/%s/source" % MAIL_ITEM
MAIL_MARKER = "Extension-rendered mail detail"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=int(os.environ.get("PORT", "0")))
    args = parser.parse_args()
    if not 0 <= args.port <= 65535:
        parser.error("--port must be between 0 and 65535")

    with tempfile.TemporaryDirectory(prefix="persona-inbox-direct-source-") as home:
        os.environ["HOME"] = home
        for key in list(os.environ):
            if key.startswith("MOCK_"):
                del os.environ[key]
        os.environ["PORT"] = str(args.port)
        os.environ["MOCK_EXTENSIONS"] = "1"
        os.environ["MOCK_INBOX_ITEM_UI"] = "1"
        os.environ["MOCK_MANAGERS"] = "0"

        root = Path(__file__).resolve().parents[3]
        spec = importlib.util.spec_from_file_location(
            "inbox_direct_source_stock_mock", root / "scripts/mock/mock_hub.py")
        hub = importlib.util.module_from_spec(spec)
        sys.modules[spec.name] = hub
        sys.dont_write_bytecode = True
        spec.loader.exec_module(hub)

        item = next((entry for entry in hub.INBOX if entry["id"] == MAIL_ITEM), None)
        if item is None or item.get("source") != "mail" or (item.get("mail") or {}).get("uids") != [501]:
            raise RuntimeError("the stock mock no longer provides the non-event mail fixture " + MAIL_ITEM)

        # Reuse the stock extension route. The registry selects this contribution only
        # for connector mail; the original contribution and handler remain intact.
        hub.INBOX_ITEM_UI.append({
            "plugin": "example-chat",
            "plugin_name": "Example mail detail",
            "id": "event",
            "title": "Mail detail",
            "placement": "inbox_item",
            "size": "full",
            "connector": "mail",
        })
        stock_inbox_item_ui = hub.inbox_item_ui

        def inbox_item_ui(ui, req):
            context = req.get("context") or {}
            if ui == "event" and context.get("item") == MAIL_ITEM:
                action = req.get("action") or ""
                if action == "open":
                    return 200, {"open_url": MAIL_SOURCE_URL}
                if action:
                    return 404, {"error": "no such mail detail action", "code": "not_found"}
                return 200, {"view": {"sections": [
                    {"title": "Extension mail detail", "items": [
                        {"type": "text", "text": MAIL_MARKER, "style": "headline"},
                        {"type": "row", "title": "Mailbox", "value": "Inbox"},
                    ]},
                    {"items": [
                        {"type": "button", "title": "Open native mail", "action": {"id": "open"}},
                    ]},
                ]}}
            return stock_inbox_item_ui(ui, req)

        hub.inbox_item_ui = inbox_item_ui

        server = hub.ThreadingHTTPServer(("127.0.0.1", args.port), hub.H)
        server.daemon_threads = True
        hub.PORT = server.server_port
        hub.ENDPOINTS.append(hub.endpoint("127.0.0.1", hub.PORT, "http", "lan"))
        signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
        print(f"INBOX_DIRECT_SOURCE_HUB_READY url=http://127.0.0.1:{hub.PORT} pid={os.getpid()}", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass
        finally:
            server.server_close()


if __name__ == "__main__":
    main()
