"""Exercise the knowledge selection mock (GET / PUT /api/v1/kb/selection) without a simulator."""
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


class KBSelectionMockTests(unittest.TestCase):
    mode = ""

    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(port), MOCK_KB_SELECTION=cls.mode), stdout=cls.log, stderr=cls.log)
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

    def request(self, path, body=None, role=None):
        headers = {"Content-Type": "application/json"}
        if role:
            headers["X-Mock-Role"] = role
        request = Request(self.base + path,
                          data=None if body is None else json.dumps(body).encode(),
                          method="GET" if body is None else "PUT",
                          headers=headers)
        with urlopen(request, timeout=3) as response:
            return json.load(response)

    def refused(self, body, role=None):
        with self.assertRaises(HTTPError) as error:
            self.request("/api/v1/kb/selection", body, role)
        return error.exception.code

    def test_defaults_then_the_saved_document(self):
        doc = self.request("/api/v1/kb/selection")
        self.assertEqual(doc["version"], 1)
        self.assertEqual(doc["messages"]["order"][0], ["mentions", "mine"])
        self.assertEqual([n for n, r in doc["messages"]["rules"].items() if r["on"]], ["mentions", "mine", "dm"])
        self.assertTrue(all(r["on"] for r in doc["docs"]["rules"].values()))
        doc["messages"]["rules"]["group"] = {"on": True, "days": 30}
        doc["items"] = [{"source": "lark", "kind": "chat", "id": "oc_team", "read": True, "store": True, "days": 7}]
        self.assertEqual(self.request("/api/v1/kb/selection", doc), doc)
        self.assertEqual(self.request("/api/v1/kb/selection"), doc)

    def test_refuses_what_the_hub_would(self):
        doc = self.request("/api/v1/kb/selection")
        def changed(edit):
            copy = json.loads(json.dumps(doc))
            edit(copy)
            return copy
        self.assertEqual(self.refused(changed(lambda d: d["docs"]["rules"]["viewed"].update(attachment_days=3))), 400)
        self.assertEqual(self.refused(changed(lambda d: d["messages"]["rules"]["dm"].update(days=0))), 400)
        self.assertEqual(self.refused(changed(lambda d: d["messages"]["order"].pop())), 400)
        self.assertEqual(self.refused(changed(lambda d: d.update(items=[{"source": "lark", "kind": "chat", "id": "a", "read": False, "store": True}]))), 400)
        self.assertEqual(self.refused(changed(lambda d: d.update(extra=True))), 400)
        self.assertEqual(self.refused(changed(lambda d: d["messages"]["rules"]["mine"].update(context=True))), 400)

    def test_a_read_only_device_cannot_save(self):
        doc = self.request("/api/v1/kb/selection")
        doc["messages"]["rules"]["group"] = {"on": True}
        self.assertEqual(self.refused(doc, "readonly"), 403)
        unchanged = self.request("/api/v1/kb/selection")
        self.assertFalse(unchanged["messages"]["rules"]["group"]["on"])

    def test_a_chat_and_a_document_say_where_they_came_from(self):
        chat = self.request("/api/v1/kb/chats/oc_team")["detail"]["meta"]
        self.assertEqual((chat["source"], chat["source_id"], chat["kind"]), ("lark", "oc_team", "group"))
        doc = self.request("/api/v1/kb/docs/doc_widget")["detail"]["meta"]
        self.assertEqual((doc["source"], doc["id"]), ("lark", "doc_widget"))


class OldHubTests(KBSelectionMockTests):
    mode = "missing"
    test_defaults_then_the_saved_document = test_refuses_what_the_hub_would = test_a_chat_and_a_document_say_where_they_came_from = None
    test_a_read_only_device_cannot_save = None

    def test_a_hub_from_before_the_selection_answers_404(self):
        with self.assertRaises(HTTPError) as error:
            self.request("/api/v1/kb/selection")
        self.assertEqual(error.exception.code, 404)


class FutureDocumentTests(KBSelectionMockTests):
    mode = "future"
    test_defaults_then_the_saved_document = test_refuses_what_the_hub_would = test_a_chat_and_a_document_say_where_they_came_from = None
    test_a_read_only_device_cannot_save = None

    def test_a_newer_hub_adds_a_field(self):
        self.assertIn("later", self.request("/api/v1/kb/selection"))


if __name__ == "__main__":
    unittest.main()
