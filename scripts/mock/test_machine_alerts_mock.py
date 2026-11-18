"""The fake hub's Machines alert fleet and diagnosis routes (#8037, scripts/mock/machine_alerts.py)."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

GB = 1 << 30


def start(env):
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    log = tempfile.TemporaryFile()
    process = subprocess.Popen(["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
                               env=dict(os.environ, PORT=str(port), **env), stdout=log, stderr=log)
    base = f"http://127.0.0.1:{port}"
    for _ in range(100):
        if process.poll() is not None:
            log.seek(0)
            raise RuntimeError("mock hub did not start: " + log.read().decode(errors="replace")[-2000:])
        try:
            with urlopen(base + "/api/v1/health", timeout=1):
                return process, log, base
        except (URLError, TimeoutError):
            time.sleep(0.1)
    raise RuntimeError("mock hub start timed out")


def stop(process, log):
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=5)
    log.close()


def call(base, path, method="GET", body=None):
    data = json.dumps(body).encode() if body is not None else (b"{}" if method == "POST" else None)
    req = Request(base + path, method=method, data=data, headers={"Content-Type": "application/json", "Authorization": "Bearer mock"})
    try:
        with urlopen(req, timeout=10) as response:
            return response.status, json.load(response)
    except HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")


class MachineAlertsMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.process, cls.log, cls.base = start({"MOCK_MACHINE_ALERTS": "1", "MOCK_MANAGERS": "1"})
        cls.addClassCleanup(stop, cls.process, cls.log)

    def machines(self):
        return {m["id"]: m for m in call(self.base, "/api/v1/machines")[1]["machines"]}

    def latest(self, mid):
        return next(m for m in call(self.base, "/api/v1/metrics")[1]["machines"] if m["machine_id"] == mid)["latest"]

    def test_fleet_has_one_of_each_alert_under_made_up_names(self):
        ms = self.machines()
        self.assertEqual({m["name"] for m in ms.values()},
                         {"Travel Laptop", "Build Box", "Studio Mac", "Garage NAS", "Cloud Runner", "Office Mini"})
        self.assertFalse(ms["nas"]["online"])
        self.assertIn("timed out", ms["nas"]["error"])
        self.assertTrue(ms["studio"]["node_mismatch"])
        self.assertNotEqual(ms["studio"]["node_version"], call(self.base, "/api/v1/health")[1]["version"])
        laptop = self.latest("macbook")
        self.assertGreaterEqual(laptop["mem_used"] / laptop["mem_total"], 0.9)
        box = self.latest("server")
        self.assertTrue(0.8 <= box["disk_used"] / box["disk_total"] < 0.9)
        health = call(self.base, "/api/v1/launchers/health?machine=cloud-runner")[1]["records"]
        self.assertEqual(sorted(r["state"] for r in health), ["blocked", "failing", "ok"])

    def test_diagnosis_answers_the_sections_asked_for(self):
        status, d = call(self.base, "/api/v1/machines/macbook/diagnosis?sections=processes")
        self.assertEqual(status, 200)
        self.assertEqual(set(d) - {"machine_id", "at"}, {"processes"})
        self.assertTrue(all(g["kind"] in ("agent", "engine", "persona", "other") for g in d["processes"]["groups"]))
        self.assertLessEqual(len(d["processes"]["groups"]), 15)
        status, d = call(self.base, "/api/v1/machines/server/diagnosis?sections=disk,tools")
        self.assertEqual(status, 200)
        self.assertTrue(any(c["in_use"] for c in d["disk"]["caches"]))
        self.assertTrue(all(c["path"].startswith("~/") for c in d["disk"]["caches"]))
        self.assertTrue(d["tools"])
        self.assertEqual(call(self.base, "/api/v1/machines/server/diagnosis?sections=argv")[0], 400)
        status, d = call(self.base, "/api/v1/machines/nas/diagnosis")
        self.assertEqual((status, d["code"]), (503, "machine_offline"))

    def test_cleanup_frees_what_it_cleaned_and_skips_what_is_in_use(self):
        call(self.base, "/mock/reset")
        before = self.latest("server")["disk_used"]
        caches = {c["id"]: c for c in call(self.base, "/api/v1/machines/server/diagnosis?sections=disk")[1]["disk"]["caches"]}
        status, r = call(self.base, "/api/v1/machines/server/cleanup", "POST", {"ids": ["npm-cache", "go-build"]})
        self.assertEqual(status, 200)
        self.assertEqual({i["id"]: i["status"] for i in r["items"]}, {"npm-cache": "cleaned", "go-build": "in_use"})
        self.assertTrue(0 < r["freed_bytes"] <= caches["npm-cache"]["bytes"])
        self.assertLess(self.latest("server")["disk_used"], before)
        left = {c["id"] for c in call(self.base, "/api/v1/machines/server/diagnosis?sections=disk")[1]["disk"]["caches"]}
        self.assertNotIn("npm-cache", left)
        self.assertEqual(call(self.base, "/api/v1/machines/server/cleanup", "POST", {"ids": ["../etc"]})[0], 400)
        self.assertEqual(call(self.base, "/api/v1/machines/server/cleanup", "POST", {"ids": []})[0], 400)

    def test_test_again_returns_the_new_record(self):
        call(self.base, "/mock/reset")
        status, r = call(self.base, "/api/v1/launchers/health/check?machine=cloud-runner&launcher=team_gw", "POST")
        self.assertEqual(status, 200)
        self.assertEqual([(x["launcher"], x["state"]) for x in r["records"]], [("team_gw", "ok")])

    def test_redeploy_walks_the_phases(self):
        status, _ = call(self.base, "/api/v1/machines/studio/deploy", "POST")
        self.assertEqual(status, 200)
        seen = set()
        deadline = time.time() + 15
        while time.time() < deadline:
            studio = self.machines()["studio"]
            if studio.get("deploy_phase"):
                seen.add(studio["deploy_phase"])
            if not studio.get("deploying") and not studio.get("node_mismatch"):
                break
            time.sleep(0.2)
        self.assertFalse(self.machines()["studio"].get("node_mismatch"))
        self.assertTrue({"waiting_idle", "transferring"} <= seen, seen)


class DiagnosisFallbackMockTests(unittest.TestCase):
    def test_an_older_node_and_an_older_hub(self):
        for mode, want in (("outdated", (409, "node_outdated")), ("hub_outdated", (404, None))):
            process, log, base = start({"MOCK_DIAGNOSIS": mode})
            try:
                status, body = call(base, "/api/v1/machines/server/diagnosis?sections=processes")
                self.assertEqual((status, body.get("code")), want)
            finally:
                stop(process, log)


if __name__ == "__main__":
    unittest.main()
