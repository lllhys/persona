"""Anonymous machines that exercise every Machines concern level. Opt in with MOCK_MACHINE_CONCERNS=1."""


_BASE_SYSTEM = {
    "os": "linux",
    "distro": "example",
    "name": "Example OS",
    "version": "1",
    "kernel": "example",
    "arch": "arm64",
    "cores": 4,
    "memory": 16 * 1024 ** 3,
}

_MACHINES = [
    {"id": "concern-pending", "name": "Pending computer", "online": True, "trust": "pending"},
    {"id": "concern-offline", "name": "Offline computer", "online": False, "last_seen": "2026-01-02T03:04:05Z"},
    {"id": "concern-critical", "name": "Critical computer", "online": True},
    {"id": "concern-warning", "name": "Warning computer", "online": True},
    {"id": "concern-needs", "name": "Waiting agents computer", "online": True},
    {"id": "concern-mismatch", "name": "Deploying computer", "online": True, "deploying": True, "deploy_phase": "transferring"},
    {"id": "concern-normal", "name": "Normal computer", "online": True, "node_version": "fixture-version"},
]

_LEVELS = {"concern-critical": "critical", "concern-warning": "warning"}


def install_machines(machines, systems):
    for machine in _MACHINES:
        item = dict(machine, kind="local", icon="desktop")
        machines.append(item)
        systems[item["id"]] = dict(_BASE_SYSTEM)
    return dict(_LEVELS)


def install_sessions(sessions, sess):
    states = ["needs_you", "needs_you", "working", "unread", "idle", "idle"]
    for index, state in enumerate(states, 1):
        status = {"needs_you": "blocked", "unread": "done"}.get(state, state)
        sessions.append(sess(
            "concern-needs",
            f"concern-agent-{index}",
            f"Concern agent {index}",
            "/work/concern",
            status,
            state,
            "concern",
            90,
            f"concern-{index}",
            index,
            index,
            "Synthetic concern fixture",
            pending=1 if state == "needs_you" else 0,
        ))
