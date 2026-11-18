"""Saved-place routes in the mock enforce the hub's read-only role contract."""
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, Request, build_opener

# The mock is on loopback: a proxy from the environment must not get in the way.
urlopen = build_opener(ProxyHandler({})).open


class SavedPlacesMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(port)), stdout=cls.log, stderr=cls.log)
        cls.addClassCleanup(cls.stop)
        for _ in range(60):
            if cls.process.poll() is not None:
                raise RuntimeError("mock hub exited before startup")
            try:
                with urlopen(cls.base + "/api/v1/health", timeout=1):
                    return
            except (URLError, TimeoutError):
                time.sleep(0.1)
        raise RuntimeError("mock hub startup timed out")

    @classmethod
    def stop(cls):
        cls.process.terminate()
        try:
            cls.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            cls.process.kill()
            cls.process.wait(timeout=5)
        cls.log.close()

    def request(self, method, path, body=None, role="full"):
        request = Request(self.base + path,
                          data=None if body is None else json.dumps(body).encode(),
                          method=method, headers={"Content-Type": "application/json", "X-Mock-Role": role})
        try:
            with urlopen(request, timeout=3) as response:
                return response.status, json.load(response)
        except HTTPError as error:
            return error.code, json.load(error)

    def test_read_only_writes_are_forbidden_without_changing_places(self):
        status, created = self.request("POST", "/api/v1/assistant/places",
                                       {"name": "Full-role control", "latitude": 3, "longitude": 3})
        self.assertEqual(status, 200)
        pid = created["place"]["id"]
        status, _ = self.request("PUT", f"/api/v1/assistant/places/{pid}",
                                 {"name": "Full-role update", "latitude": 4, "longitude": 4})
        self.assertEqual(status, 200)
        status, _ = self.request("DELETE", f"/api/v1/assistant/places/{pid}")
        self.assertEqual(status, 200)

        status, before = self.request("GET", "/api/v1/assistant/places", role="readonly")
        self.assertEqual(status, 200)
        results = [
            self.request("POST", "/api/v1/assistant/places",
                         {"name": "Read-only injection", "latitude": 1, "longitude": 1}, role="readonly"),
            self.request("PUT", "/api/v1/assistant/places/pl1",
                         {"name": "Changed without full role", "latitude": 2, "longitude": 2}, role="readonly"),
            self.request("DELETE", "/api/v1/assistant/places/pl1", role="readonly"),
        ]
        status, after = self.request("GET", "/api/v1/assistant/places", role="readonly")
        self.assertEqual(status, 200)
        self.assertEqual([status for status, _ in results], [403, 403, 403])
        self.assertEqual(before, after)


if __name__ == "__main__":
    unittest.main()
