#!/usr/bin/env python3
"""Device check for ordinary link reopening (android/feature/chat/LINKS.md, "Reopening a link").

    android_link_reopen_check.py serve [--host ADDRESS] [--name HOSTNAME] [--port PORT] [--log FILE]
    android_link_reopen_check.py map-host --serial SERIAL [--name HOSTNAME] [--address ADDRESS]
    android_link_reopen_check.py unmap-host --serial SERIAL
    android_link_reopen_check.py reopen --serial SERIAL --base URL --log FILE
    android_link_reopen_check.py cookie --serial SERIAL --base URL --log FILE
    android_link_reopen_check.py cache --serial SERIAL --base URL --log FILE

`serve` runs the test pages on this computer and logs every request with a timestamp. Bind it to an address the phone can reach
that is not loopback (`adb reverse` would turn it into a forwarded port, a different code path); with no --host it uses this
computer's outbound address. It prints the links to send, one per message, so the chat shows them. The links carry a placeholder
host name (`persona.test`, or --name) and never this computer's address, so a screenshot of the card shows no real network address;
the device has to resolve that name to the server (an emulator: see the scene file's `localServer`). Pass `--name` with a real
address only when no capture will be kept.

`map-host` makes the placeholder name resolve on a debuggable emulator, for the WebView only: it writes the WebView's command-line
file with one host-resolver rule (default: the name to the emulator's alias for this computer's loopback, so `serve`
can bind 127.0.0.1). Restart the app afterwards; `unmap-host` removes the file. The page requests are real, only the name differs.

The device commands drive the open card through `adb` and read the page back with `uiautomator dump`. They judge page history,
response sequence and request timestamps, the identity of the session cookie, and whether a cacheable script returns to the server.
The cache check proves a shared HTTP cache across reopen, not persistent disk storage after an app process restart. The UI language
must be English, or pass the close label.
"""
import argparse
import http.cookies
import http.server
import json
from pathlib import Path
import re
import socket
import socketserver
import subprocess
import sys
import threading
import time
import uuid
import xml.etree.ElementTree as ET

PAGE = """<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width"><title>Page {name}</title>
<body><h1>Page {name}</h1><p id=state>page={name} load={load} history=loading</p><p><a href="{other}">Go to {other_name}</a></p>
<script>document.getElementById('state').textContent='page={name} load={load} history='+history.length;</script>"""
CACHE_PAGE = """<!doctype html><meta charset=utf-8><meta name=viewport content="width=device-width"><title>Cache page</title>
<body><p id=state>cache load={load} asset=waiting</p>
<script>window.cacheAssetLoaded='missing'</script><script src="{asset}"></script>
<script>document.getElementById('state').textContent='cache load={load} asset='+window.cacheAssetLoaded;</script>"""
CACHE_ASSET = "window.cacheAssetLoaded='loaded';\n"
NO_STORE = {"Cache-Control": "no-store"}
# What links and screenshots show instead of this computer's address.
PLACEHOLDER_HOST = "persona.test"
# The emulator's alias for this computer's loopback, written in parts like other private-range literals in the scripts.
EMULATOR_HOST_ALIAS = ".".join(str(part) for part in (10, 0, 2, 2))


def session_cookie(header):
    """The test session in a Cookie header, not a substring of another cookie."""
    if not header:
        return None
    parsed = http.cookies.SimpleCookie()
    try:
        parsed.load(header)
    except http.cookies.CookieError:
        return None
    value = parsed.get("session")
    return value.value if value else None


class Pages(http.server.BaseHTTPRequestHandler):
    """History pages, a one-time login, a session echo and a page with one cacheable script."""
    sequence = 0
    lock = threading.Lock()
    log_path = None
    session_token = "test-session"
    login_issued = False
    cache_token = "test-cache"

    def log_message(self, *args):
        pass

    def reply(self, status, body, headers=(), content_type="text/html; charset=utf-8"):
        data = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        # A response such as the cache asset explicitly overrides the default no-store.
        for key, value in {**NO_STORE, **dict(headers)}.items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        cls = type(self)
        path = self.path.split("?", 1)[0]
        with cls.lock:
            cls.sequence += 1
            load = cls.sequence
            session = session_cookie(self.headers.get("Cookie"))
            issued = None
            if path == "/login" and not cls.login_issued:
                # A release preload of /login must not repair a cleared store or rotate the session being checked.
                cls.login_issued = True
                issued = cls.session_token
            if cls.log_path:
                with open(cls.log_path, "a") as stream:
                    stream.write(json.dumps({"seq": load, "t": time.time(), "path": path, "target": self.path,
                                             "cookie": session is not None, "session": session, "issued": issued}) + "\n")
        if path == "/a":
            self.reply(200, PAGE.format(name="a", load=load, other="/b", other_name="B"))
        elif path == "/b":
            self.reply(200, PAGE.format(name="b", load=load, other="/a", other_name="A"))
        elif path == "/login":
            headers = {"Location": "/a"}
            if issued is not None:
                headers["Set-Cookie"] = f"session={issued}; Max-Age=86400; Path=/"
            self.reply(302, "", headers)
        elif path == "/whoami":
            self.reply(200, f"<!doctype html><meta charset=utf-8><body><p>whoami load={load} session={session or 'missing'}</p>")
        elif path == "/cache":
            self.reply(200, CACHE_PAGE.format(load=load, asset="/cache-asset.js?run=" + cls.cache_token))
        elif path == "/cache-asset.js":
            self.reply(200, CACHE_ASSET, {"Cache-Control": "public, max-age=3600"}, "application/javascript; charset=utf-8")
        else:
            self.reply(404, "not found")


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True


def outbound_address():
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
        probe.connect(("192.0.2.1", 9))  # a documentation address: nothing is sent
        return probe.getsockname()[0]


def make_server(host, port, log_path):
    if log_path:
        # A new random session and cache key need a log from the same server run.
        Path(log_path).write_text("")
    handler = type("Handler", (Pages,), {"sequence": 0, "lock": threading.Lock(), "log_path": log_path,
                                        "session_token": uuid.uuid4().hex, "cache_token": uuid.uuid4().hex})
    return Server((host, port), handler)


WEBVIEW_COMMAND_LINE = "/data/local/tmp/webview-command-line"


def webview_command_line(name, address):
    """The file a debuggable WebView reads at start: a placeholder program name, then one resolver rule."""
    if not re.fullmatch(r"[A-Za-z0-9.-]+", name) or not re.fullmatch(r"[0-9a-fA-F:.]+", address):
        raise SystemExit("map-host: the name and the address must be a host name and an IP address")
    return f'_ --host-resolver-rules="MAP {name} {address}"\n'


def read_log(path):
    if not Path(path).exists():
        return []
    return [json.loads(line) for line in Path(path).read_text().splitlines() if line.strip()]


def log_entry(entries, load, path):
    return next((entry for entry in entries if entry["seq"] == load and entry["path"] == path), None)


# What uiautomator dump gives: nodes with text, content-desc and bounds "[l,t][r,b]".
def parse_nodes(xml_text):
    nodes = []
    for node in ET.fromstring(xml_text).iter("node"):
        match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", node.get("bounds", ""))
        if match:
            nodes.append({"text": node.get("text", ""), "desc": node.get("content-desc", ""),
                          "bounds": tuple(int(group) for group in match.groups())})
    return nodes


def center(bounds):
    left, top, right, bottom = bounds
    return (left + right) // 2, (top + bottom) // 2


def find(nodes, *, text=None, desc=None):
    for node in nodes:
        if text is not None and text in node["text"]:
            return node
        if desc is not None and desc == node["desc"]:
            return node
    return None


STATE = re.compile(r"page=(\w+) load=(\d+) history=(\d+)")
WHOAMI_STATE = re.compile(r"whoami load=(\d+) session=(\S+)")
CACHE_STATE = re.compile(r"cache load=(\d+) asset=(\w+)")


def page_state(nodes):
    """(page, load, history) from the test page's state line, or None while it is not on screen."""
    for node in nodes:
        match = STATE.search(node["text"])
        if match:
            return match.group(1), int(match.group(2)), int(match.group(3))
    return None


def whoami_state(nodes):
    for node in nodes:
        match = WHOAMI_STATE.search(node["text"])
        if match:
            return int(match.group(1)), match.group(2)
    return None


def cache_state(nodes):
    for node in nodes:
        match = CACHE_STATE.search(node["text"])
        if match and match.group(2) == "loaded":
            return int(match.group(1)), match.group(2)
    return None


class Device:
    def __init__(self, serial):
        self.serial = serial

    def adb(self, *args):
        return subprocess.run(["adb", "-s", self.serial, *args], check=True, capture_output=True, text=True).stdout

    def nodes(self):
        out = self.adb("exec-out", "uiautomator", "dump", "/dev/tty")
        return parse_nodes(out[out.index("<?xml"):out.rindex("</hierarchy>") + len("</hierarchy>")])

    def tap(self, node):
        x, y = center(node["bounds"])
        self.adb("shell", "input", "tap", str(x), str(y))

    def wait(self, ready, what, timeout=20):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            found = ready(self.nodes())
            if found:
                return found
            time.sleep(0.5)
        raise SystemExit(f"timed out waiting for {what}")

    def tap_when_present(self, what, **target):
        self.tap(self.wait(lambda nodes: find(nodes, **target), what))


def open_link(device, base, path):
    device.tap_when_present(f"the message link {path}", text=base + path)
    return time.time()


def close_card(device, label):
    """Tap close and return when the tap was sent: the release (and the fresh preload after it) starts then, while the
    card is only seen gone a UI dump later, which would make a request that followed the tap look like one before the close."""
    device.tap_when_present("the close button", desc=label)
    tapped = time.time()
    # The card is gone once its close control is. Ordinary history pages must also have left the semantics tree.
    device.wait(lambda nodes: page_state(nodes) is None and find(nodes, desc=label) is None, "the card to close")
    return tapped


def reopen(device, base, log, close_label, settle):
    timeline, failures, notes = {}, [], []
    timeline["first_open"] = open_link(device, base, "/a")
    _, first_load, first_history = device.wait(
        lambda nodes: (state := page_state(nodes)) and state[0] == "a" and state, "page A")
    device.tap_when_present("the link to B", text="Go to B")
    device.wait(lambda nodes: (state := page_state(nodes)) and state[0] == "b" and state, "page B")
    timeline["closed"] = close_card(device, close_label)
    time.sleep(settle)
    timeline["second_open"] = open_link(device, base, "/a")
    _, second_load, second_history = device.wait(
        lambda nodes: (state := page_state(nodes)) and state[0] == "a" and state, "page A again")
    entries = read_log(log)
    produced = log_entry(entries, second_load, "/a")
    if first_history != 1:
        failures.append(f"first opening showed history.length {first_history}, expected 1")
    if second_history != 1:
        failures.append(f"the reopened page has history.length {second_history}: it kept the in-page history")
    if second_load == first_load:
        failures.append("the reopened page is the load the first opening showed, not a new one")
    if produced is None:
        failures.append(f"load {second_load} is not a logged /a request")
    else:
        if produced["t"] < timeline["closed"] - 0.5:
            failures.append("the reopened page was requested before the card closed")
        # A device that prerenders also loads a prerendered page again when it is opened, so the time of this request
        # relative to the tap is reported, not judged.
        notes.append("the reopened page's request came %s the tap" % ("after" if produced["t"] > timeline["second_open"] else "before"))
    if any(entry["path"] == "/b" and entry["t"] > timeline["closed"] for entry in entries):
        failures.append("page B was requested after the card closed")
    return dict(timeline=timeline, first=dict(load=first_load, history=first_history),
                reopened=dict(load=second_load, history=second_history, requested_at=produced and produced["t"]),
                notes=notes, failures=failures)


def judge_cookie(expected, login_time, observations):
    """Check (load, session, producing log entry) tuples without involving a device."""
    failures = []
    if expected is None:
        failures.append("the server did not issue exactly one test session before the checks")
    previous_load = None
    for index, (load, session, produced) in enumerate(observations, 1):
        if expected is not None and session != expected:
            failures.append(f"whoami opening {index} returned {session}, not the originally issued session")
        if previous_load == load:
            failures.append("both whoami openings showed the same response, not two independently produced responses")
        if produced is None:
            failures.append(f"whoami response {load} has no matching /whoami request log")
        elif produced["t"] < login_time:
            failures.append(f"whoami response {load} predates login and cannot prove the session survived release and preload")
        previous_load = load
    return failures


def cookie(device, base, log, close_label, settle):
    answers, observations = [], []
    open_link(device, base, "/login")
    device.wait(lambda nodes: (state := page_state(nodes)) and state[0] == "a" and state, "page A after login")
    issued = [entry for entry in read_log(log) if entry["path"] == "/login" and entry.get("issued")]
    expected = issued[0]["issued"] if len(issued) == 1 else None
    login_time = issued[0]["t"] if len(issued) == 1 else 0
    close_card(device, close_label)
    for _ in range(2):
        time.sleep(settle)
        open_link(device, base, "/whoami")
        load, session = device.wait(whoami_state, "the whoami page")
        answers.append(dict(load=load, session=session))
        observations.append((load, session, log_entry(read_log(log), load, "/whoami")))
        close_card(device, close_label)
    failures = judge_cookie(expected, login_time, observations)
    served = [entry for entry in read_log(log) if entry["path"] == "/whoami"]
    return dict(expected_session=expected, answers=answers, whoami_requests=served, failures=failures)


def judge_cache(first_load, second_load, produced, asset_requests, closed):
    failures = []
    if second_load == first_load:
        failures.append("the reopened cache page is the first response, not a new main-document response")
    if produced is None:
        failures.append(f"cache response {second_load} has no matching /cache request log")
    elif produced["t"] < closed - 0.5:
        failures.append("the reopened cache page was requested before the card closed")
    if len(asset_requests) != 1:
        failures.append(f"the unique cacheable script reached the server {len(asset_requests)} times, expected only its first load")
    return failures


def cache(device, base, log, close_label, settle):
    open_link(device, base, "/cache")
    first_load, _ = device.wait(cache_state, "the cache page with its script loaded")
    closed = close_card(device, close_label)
    time.sleep(settle)
    opened = open_link(device, base, "/cache")
    second_load, _ = device.wait(cache_state, "the reopened cache page with its script loaded")
    entries = read_log(log)
    produced = log_entry(entries, second_load, "/cache")
    assets = [entry for entry in entries if entry["path"] == "/cache-asset.js"]
    failures = judge_cache(first_load, second_load, produced, assets, closed)
    return dict(first_load=first_load, reopened=dict(load=second_load, requested_at=produced and produced["t"], opened_at=opened),
                asset_requests=assets, cache_scope="shared HTTP cache across reopen; process-restart disk persistence is not judged",
                failures=failures)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    parser.add_argument("command", choices=["serve", "map-host", "unmap-host", "reopen", "cookie", "cache"])
    parser.add_argument("--address", default=EMULATOR_HOST_ALIAS, help="map-host: where the name points (default: the emulator's host alias)")
    parser.add_argument("--host", help="the address to bind (default: this computer's outbound address)")
    parser.add_argument("--name", default=PLACEHOLDER_HOST, help="the host name the printed links carry (default: %(default)s)")
    parser.add_argument("--port", type=int, default=0)
    parser.add_argument("--log", default="link-reopen-requests.jsonl")
    parser.add_argument("--serial")
    parser.add_argument("--base", help="the server's address as the phone sees it, e.g. http://<address>:<port>")
    parser.add_argument("--close-label", default="Close page")
    parser.add_argument("--settle", type=float, default=3.0, help="seconds between closing and reopening")
    args = parser.parse_args(argv)
    if args.command == "serve":
        host = args.host or outbound_address()
        server = make_server(host, args.port, args.log)
        base = f"http://{args.name}:{server.server_address[1]}"
        print(f"Serving {base} (bound to port {server.server_address[1]} of the chosen address); requests are logged to {args.log}")
        print("Send each link as its own message. Use an Agent reply for /a to check first-open preload; send the other links")
        print("as your own messages so /whoami cannot be preloaded before login and the first cache asset request is observable:")
        for path in ("/a", "/login", "/whoami", "/cache"):
            print(f"  {base}{path}", flush=True)
        try:
            server.serve_forever()
        except KeyboardInterrupt:
            pass
        return 0
    if args.command in ("map-host", "unmap-host"):
        if not args.serial:
            parser.error("--serial is required")
        device = Device(args.serial)
        if args.command == "map-host":
            line = webview_command_line(args.name, args.address)
            device.adb("shell", f"printf '%s' '{line}' > {WEBVIEW_COMMAND_LINE} && chmod 644 {WEBVIEW_COMMAND_LINE}")
            print(f"{args.name} now resolves to {args.address} in the WebView of {args.serial}; restart the app")
        else:
            device.adb("shell", "rm", "-f", WEBVIEW_COMMAND_LINE)
            print(f"the WebView host mapping of {args.serial} is removed; restart the app")
        return 0
    if not (args.serial and args.base):
        parser.error("--serial and --base are required")
    device = Device(args.serial)
    run = {"reopen": reopen, "cookie": cookie, "cache": cache}[args.command]
    report = run(device, args.base.rstrip("/"), args.log, args.close_label, args.settle)
    print(json.dumps(report, indent=2))
    print(f"android_link_reopen_check: {'FAIL' if report['failures'] else 'PASS'} {args.command}")
    return 1 if report["failures"] else 0


if __name__ == "__main__":
    sys.exit(main())
