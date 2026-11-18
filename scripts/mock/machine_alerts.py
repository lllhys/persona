"""The Machines page's alerts and what fixes them (#8037), for the fake hub.

MOCK_MACHINE_ALERTS=1 turns the mock's computers into a fleet with one of each alert, under made-up names only:
  Travel Laptop   memory critical, with idle, finished, working and needs-you Agents (the MacBook's sessions)
  Build Box       disk almost full (a cache in use by a running build)
  Studio Mac      not the hub's build (Redeploy walks the phases: waiting for idle, transferring, restarting, confirming)
  Garage NAS      offline, timed out
  Cloud Runner    a launcher at its usage limit and one whose key was rejected (Test again fixes the second)
  Office Mini     healthy
and adds GET /api/v1/launchers/health, POST /api/v1/launchers/health/check and the `launcher_health` event.

In every mode: GET /api/v1/machines/{mid}/diagnosis?sections= and POST /api/v1/machines/{mid}/cleanup {ids}, with
MOCK_DIAGNOSIS=ok (default) | outdated (the node predates it: 409 node_outdated) | hub_outdated (404 without a code, as
an older hub answers) | slow (3 s, then ok) | error (the node failed). Cleanup empties the picked caches, reports the
bytes it freed and lowers the disk the metrics show by as much (FREED).
"""
import json
import os
import threading
import time
from datetime import datetime, timedelta, timezone
from urllib.parse import parse_qs, urlparse

GB = 1 << 30
MB = 1 << 20
ON = os.environ.get("MOCK_MACHINE_ALERTS") == "1"
# The hub's build and an older node's, named as scripts/deploy/deploy.sh names builds (<describe>-<UTC minute>).
HUB_VERSION = "v0.9.0-14-g1a2b3c4-202611141020"
OLD_NODE = "v0.9.0-3-g9f8e7d6-202611110815"


def _ago(**kw):
    return (datetime.now(timezone.utc) - timedelta(**kw)).isoformat().replace("+00:00", "Z")


def _later(**kw):
    return (datetime.now(timezone.utc) + timedelta(**kw)).isoformat().replace("+00:00", "Z")


# What each computer's latest sample reads (fractions of memory and disk, CPU percent).
METRICS = {
    "macbook": {"cpu": 46.0, "mem": 0.95, "disk": 0.58},
    "server": {"cpu": 31.0, "mem": 0.47, "disk": 0.86},
    "studio": {"cpu": 22.0, "mem": 0.34, "disk": 0.41},
    "cloud-runner": {"cpu": 18.0, "mem": 0.38, "disk": 0.52},
    "office-mini": {"cpu": 9.0, "mem": 0.29, "disk": 0.33},
}

_NAMES = {"macbook": "Travel Laptop", "server": "Build Box", "studio": "Studio Mac", "nas": "Garage NAS"}

_EXTRA = [
    ({"id": "cloud-runner", "name": "Cloud Runner", "kind": "ssh", "icon": "server", "ssh_host": "cloud-runner", "online": True,
      "hostname": "runner", "os": "linux", "arch": "amd64", "home": "/home/me", "herdr_version": "0.9.5", "claude_version": "2.1.320"},
     {"os": "linux", "distro": "ubuntu", "like": "debian", "name": "Ubuntu 24.04.1 LTS", "version": "24.04", "kernel": "6.8.0-48-generic",
      "arch": "amd64", "model": "Example Cloud VM", "cpu": "Example vCPU", "cores": 8, "memory": 32 * GB}),
    ({"id": "office-mini", "name": "Office Mini", "kind": "ssh", "icon": "desktop", "ssh_host": "office-mini", "online": True,
      "hostname": "Mini", "os": "darwin", "arch": "arm64", "home": "/Users/me", "herdr_version": "0.9.5", "claude_version": "2.1.320"},
     {"os": "darwin", "distro": "macos", "name": "macOS 26.1", "version": "26.1", "build": "25B78", "kernel": "Darwin 25.1.0",
      "arch": "arm64", "model": "Mac mini", "cpu": "Apple M4", "cores": 10, "memory": 32 * GB}),
]


def install_machines(machines, systems):
    """Renames and reshapes the mock's computers and adds two; returns METRICS for metric_at."""
    for item, system in _EXTRA:
        if not any(m["id"] == item["id"] for m in machines):
            machines.append(dict(item, last_seen=_ago()))
            systems[item["id"]] = dict(system)
    for m in machines:
        if m["id"] in _NAMES:
            m["name"] = _NAMES[m["id"]]
        m["node_version"] = HUB_VERSION
    studio = next((m for m in machines if m["id"] == "studio"), None)
    if studio:
        studio.update(node_version=OLD_NODE, node_mismatch=True, can_deploy=True)
    nas = next((m for m in machines if m["id"] == "nas"), None)
    if nas:
        nas.update(online=False, error="ssh: connect to host garage-nas port 22: Operation timed out", last_seen=_ago(hours=3))
        nas.pop("node_version", None)
    return METRICS


def install_sessions(sessions, sess):
    """Two more idle Agents on the Travel Laptop, one for a to-do, and one working on the Studio Mac."""
    sessions.append(sess("macbook", "c8e1a7b2-0000-4000-8000-000000008037", "Draft the onboarding email", "/Users/me/code/mail",
                         "idle", "idle", "mail", 3, "t1", 1, 95, "The draft is in drafts/onboarding.md.",
                         todo={"id": "todo_onboarding", "title": "Onboarding email"}, state_since=_ago(minutes=95)))
    sessions.append(sess("macbook", "c8e1a7b2-0000-4000-8000-000000008038", "Resize the screenshots", "/Users/me/code/site",
                         "idle", "idle", "site", 4, "t1", 1, 240, "All 14 screenshots are 1280 wide now.", state_since=_ago(hours=4)))
    sessions.append(sess("studio", "c8e1a7b2-0000-4000-8000-000000008039", "Render the product video", "/Users/me/code/video",
                         "working", "working", "video", 5, "t1", 1, 2, "Rendering frame 1200 of 3600."))


# ── deploys: the phases a real hub pushes ──

PHASES = [("waiting_idle", 1.2), ("transferring", 1.6), ("restarting", 1.4), ("confirming", 1.2)]


def phased_deploy(machine, push, error=None):
    """Walks the machine through the deploy phases, pushing each; ends on the hub's build or on `error`."""
    machine["deploying"] = True
    machine.pop("deploy_error", None)

    def run():
        for phase, secs in PHASES:
            machine["deploy_phase"] = phase
            if phase == "restarting":
                machine["online"] = False
            if phase == "confirming":
                machine["online"] = True
            push()
            time.sleep(secs)
            if error and phase == "transferring":
                machine.update(deploy_error=error, online=True)
                break
        machine.pop("deploying", None)
        machine.pop("deploy_phase", None)
        machine["online"] = True
        if not error:
            machine.pop("node_mismatch", None)
            machine["node_version"] = HUB_VERSION
        push()

    threading.Thread(target=run, daemon=True).start()
    push()


# ── launcher health ──

RECORDS = []


def _reset_records():
    RECORDS[:] = [
        {"machine": "cloud-runner", "launcher": "codex", "agent": "codex", "state": "failing", "kind": "usage_limit",
         "error": "You've hit your usage limit. Try again later.", "checked_at": _ago(minutes=18), "source": "session", "fails": 3,
         "resets_at": _later(hours=2, minutes=40)},
        {"machine": "cloud-runner", "launcher": "team_gw", "agent": "claude", "state": "blocked", "kind": "auth",
         "error": "API Error: 401 {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid x-api-key\"}}",
         "checked_at": _ago(minutes=42), "source": "check", "fails": 2, "blocked_reason": "The repair didn't fix it."},
        {"machine": "cloud-runner", "launcher": "claude", "agent": "claude", "state": "ok", "checked_at": _ago(minutes=12), "source": "check"},
    ]


_reset_records()


def with_health(mid, launchers):
    """GET /machines/{mid}/launchers's list with each launcher's record; the Cloud Runner also has team_gw."""
    out = [dict(l) for l in launchers]
    if mid == "cloud-runner" and not any(l["name"] == "team_gw" for l in out):
        out.append({"name": "team_gw", "display_name": "Team gateway", "model": "sonnet"})
    for l in out:
        r = next((r for r in RECORDS if r["machine"] == mid and r["launcher"] == l["name"]), None)
        if r:
            l["health"] = {k: r[k] for k in ("state", "kind", "error", "checked_at", "resets_at") if r.get(k)}
    return out


# ── diagnosis and cleanup ──

CLEANED = {}  # mid -> set of cache ids emptied
FREED = {}  # mid -> bytes the cleanups freed (the hub's metrics read the disk this much emptier)


def _caches(mid, home_os):
    mac = home_os == "darwin"
    rows = [("go-build", "~/Library/Caches/go-build" if mac else "~/.cache/go-build", 6.4 * GB, mid == "server"),
            ("go-mod", "~/go/pkg/mod", 3.1 * GB, False),
            ("npm-cache", "~/.npm/_cacache", 2.2 * GB, False),
            ("pip-cache", "~/Library/Caches/pip" if mac else "~/.cache/pip", 1.3 * GB, False),
            ("uv-cache", "~/.cache/uv", 0.9 * GB, False)]
    if mac:
        rows += [("homebrew-cache", "~/Library/Caches/Homebrew", 1.8 * GB, False),
                 ("xcode-derived-data", "~/Library/Developer/Xcode/DerivedData", 14.6 * GB, mid == "macbook")]
    else:
        rows += [("cargo-registry", "~/.cargo/registry", 2.7 * GB, False), ("gradle-cache", "~/.gradle/caches", 1.1 * GB, False)]
    gone = CLEANED.get(mid, set())
    return [{"id": i, "path": p, "bytes": int(b), "in_use": u} for i, p, b, u in rows if i not in gone]


_GROUPS = {
    "macbook": [("claude", "agent", 6, 7.9 * GB, 14.0), ("node", "other", 14, 6.1 * GB, 9.5), ("Xcode", "other", 3, 5.2 * GB, 4.0),
                ("Safari", "other", 22, 4.4 * GB, 3.1), ("codex", "agent", 2, 2.6 * GB, 6.2), ("herdr", "engine", 1, 0.9 * GB, 1.2),
                ("personad", "persona", 1, 0.4 * GB, 0.8), ("Simulator", "other", 4, 3.3 * GB, 12.0), ("Docker", "other", 5, 2.9 * GB, 2.2),
                ("Slack", "other", 6, 1.1 * GB, 0.6)],
    "server": [("go", "other", 4, 3.6 * GB, 180.0), ("claude", "agent", 2, 2.1 * GB, 8.0), ("postgres", "other", 9, 1.9 * GB, 4.0),
               ("node", "other", 6, 1.4 * GB, 3.0), ("personad", "persona", 1, 0.3 * GB, 1.0), ("herdr", "engine", 1, 0.2 * GB, 0.5)],
}


def _processes(mid, system):
    total = system.get("memory") or 16 * GB
    rows = _GROUPS.get(mid) or [("claude", "agent", 1, 1.2 * GB, 4.0), ("node", "other", 3, 0.8 * GB, 2.0),
                                ("personad", "persona", 1, 0.25 * GB, 0.5), ("herdr", "engine", 1, 0.15 * GB, 0.3)]
    groups = [{"name": n, "kind": k, "count": c, "rss": int(r), "cpu": cpu} for n, k, c, r, cpu in rows]
    groups.sort(key=lambda g: -g["rss"])
    return {"mem_total": total, "count": 180 + sum(g["count"] for g in groups) * 3, "groups": groups}


def _disk(mid, machine, latest):
    used, disk_total = (latest or {}).get("disk_used") or 0, (latest or {}).get("disk_total") or 0
    mac = machine.get("os") == "darwin"
    folders = [("~/code", 0.34), ("~/Library", 0.22), ("~/Downloads", 0.09), ("~/Movies", 0.07), ("~/.cache", 0.05),
               ("~/Documents", 0.04), ("~/Pictures", 0.03)] if mac else \
              [("~/code", 0.38), ("~/.cache", 0.16), ("~/data", 0.12), ("~/go", 0.06), ("~/.cargo", 0.04), ("~/Downloads", 0.02)]
    return {"volumes": [{"mount": "/System/Volumes/Data" if mac else "/", "used": used, "total": disk_total}],
            "folders": [{"path": p, "bytes": int(f * used)} for p, f in folders],
            "partial": mid == "server", "caches": _caches(mid, machine.get("os"))}


_TOOLS = {
    "darwin": [("xcode", "26.1"), ("swift", "6.2"), ("git", "2.47.1"), ("gh", "2.63.0"), ("brew", "4.4.10"), ("node", "22.11.0"),
               ("python", "3.13.0"), ("go", "1.24.1"), ("docker", "27.3.1"), ("claude", "2.1.320"), ("codex", "0.98.0")],
    "linux": [("docker", "27.3.1"), ("podman", ""), ("go", "1.24.1"), ("node", "22.11.0"), ("python", "3.12.3"), ("rust", "1.83.0"),
              ("git", "2.43.0"), ("gh", "2.63.0"), ("tmux", "3.4"), ("uv", "0.5.9"), ("claude", "2.1.320")],
}


def _tools(mid, machine):
    rows = _TOOLS.get(machine.get("os") or "linux", _TOOLS["linux"])
    if mid == "office-mini":
        rows = [r for r in rows if r[0] not in ("docker", "xcode")]
    return [{"id": i, "version": v} for i, v in rows]


def route(h, method, machines, systems, latest, push):
    """The diagnosis, cleanup and launcher health routes; False when the request is none of them."""
    u = urlparse(h.path)
    p, q = u.path, parse_qs(u.query)
    parts = p.split("/")
    if ON and p == "/api/v1/launchers/health" and method == "GET":
        state, mid = q.get("state", [""])[0], q.get("machine", [""])[0]
        h.send_json({"records": [r for r in RECORDS if (not state or r["state"] == state) and (not mid or r["machine"] == mid)]})
        return True
    if ON and p == "/api/v1/launchers/health/check" and method == "POST":
        mid, launcher = q.get("machine", [""])[0], q.get("launcher", [""])[0]
        time.sleep(1.0)
        out = []
        for r in RECORDS:
            if (mid and r["machine"] != mid) or (launcher and r["launcher"] != launcher):
                continue
            r["checked_at"] = _ago()
            if r.get("kind") == "auth":  # the key was fixed meanwhile: the check passes
                for k in ("kind", "error", "fails", "blocked_reason"):
                    r.pop(k, None)
                r["state"] = "ok"
            out.append(dict(r))
            push({"type": "launcher_health", "record": dict(r)})
        h.send_json({"records": out})
        return True
    if not (p.startswith("/api/v1/machines/") and len(parts) == 6 and parts[5] in ("diagnosis", "cleanup")):
        return False
    if (parts[5] == "diagnosis") != (method == "GET"):
        return False
    mid = parts[4]
    machine = next((m for m in machines if m["id"] == mid), None)
    mode = os.environ.get("MOCK_DIAGNOSIS", "ok")
    if mode == "hub_outdated":
        h.send_json({"error": "not found"}, 404, add_error_code=False)
        return True
    if machine is None:
        h.send_json({"error": "unknown machine", "code": "not_found"}, 404)
        return True
    if not machine.get("online"):
        h.send_json({"error": "This computer is not connected.", "code": "machine_offline"}, 503)
        return True
    if mode == "outdated":
        h.send_json({"error": "Persona on this computer is too old for this.", "code": "node_outdated"}, 409)
        return True
    if mode == "slow":
        time.sleep(3)
    if mode == "error":
        h.send_json({"error": "The computer couldn't answer: broken pipe", "code": "internal"}, 502)
        return True
    system = systems.get(mid, {})
    if parts[5] == "diagnosis":
        sections = [s for s in (q.get("sections", [""])[0] or "processes,disk,tools").split(",") if s]
        bad = [s for s in sections if s not in ("processes", "disk", "tools")]
        if bad:
            h.send_json({"error": "unknown section " + bad[0], "code": "bad_request"}, 400)
            return True
        out = {"machine_id": mid, "at": int(time.time())}
        if "processes" in sections:
            out["processes"] = _processes(mid, system)
        if "disk" in sections:
            out["disk"] = _disk(mid, machine, latest(mid))
        if "tools" in sections:
            out["tools"] = _tools(mid, machine)
        h.send_json(out)
        return True
    ids = json.loads(h.last_body or b"{}").get("ids") or []
    known = {c["id"]: c for c in _caches(mid, machine.get("os"))}
    if not ids or any(i not in known and i not in CLEANED.get(mid, set()) for i in ids):
        h.send_json({"error": "unknown or empty cache ids", "code": "bad_request"}, 400)
        return True
    time.sleep(1.2)
    items, freed = [], 0
    for i in ids:
        c = known.get(i)
        if c is None:
            items.append({"id": i, "bytes": 0, "status": "missing"})
        elif c["in_use"]:
            items.append({"id": i, "bytes": c["bytes"], "status": "in_use"})
        else:
            CLEANED.setdefault(mid, set()).add(i)
            items.append({"id": i, "bytes": c["bytes"], "status": "cleaned"})
            freed += int(c["bytes"] * 0.97)  # what statfs sees: a little less than the folders held
    now = latest(mid) or {}
    before = max(0, (now.get("disk_total") or 0) - (now.get("disk_used") or 0))
    FREED[mid] = FREED.get(mid, 0) + freed
    h.send_json({"freed_bytes": freed, "available_before": before, "available_after": before + freed, "items": items})
    return True


def reset():
    CLEANED.clear()
    FREED.clear()
    _reset_records()
