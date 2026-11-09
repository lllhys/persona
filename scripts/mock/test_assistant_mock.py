"""A new Persona conversation on the mock hub behaves like the hub's (#511): the session list is pushed at once, the
transcript is empty until claude has written the first message, and then that message is pushed to the open chat."""
import base64
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import tempfile
import time
import unittest
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class NewConversationMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            cls.port = sock.getsockname()[1]
        cls.base = f"http://127.0.0.1:{cls.port}"
        cls.log = tempfile.TemporaryFile()
        cls.process = subprocess.Popen(
            ["python3", "-I", str(Path(__file__).with_name("mock_hub.py"))],
            env=dict(os.environ, PORT=str(cls.port), MOCK_ASSISTANT_TRANSCRIPT_SECONDS="0.5"), stdout=cls.log, stderr=cls.log)
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

    def get(self, path):
        with urlopen(self.base + path, timeout=3) as response:
            return json.load(response)

    def start(self, prompt):
        request = Request(self.base + "/api/v1/assistant/conversations", data=json.dumps({"prompt": prompt}).encode(),
                          method="POST", headers={"Content-Type": "application/json"})
        with urlopen(request, timeout=3) as response:
            return json.load(response)["session"]

    def live(self):
        """The app's live connection (whole session lists, no sessions_delta): a reader of its JSON frames (None once
        nothing comes for 3 s) and a writer of short ones."""
        conn = socket.create_connection(("127.0.0.1", self.port), timeout=3)
        r = conn.makefile("rb")
        self.addCleanup(conn.close)
        self.addCleanup(r.close)
        key = base64.b64encode(os.urandom(16)).decode()
        conn.sendall((f"GET /api/v1/ws HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                      f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
        while r.readline() not in (b"\r\n", b""):
            pass

        def send(obj):  # unmasked: the mock reads either
            data = json.dumps(obj).encode()
            conn.sendall(bytes([0x81, len(data)]) + data)

        def read():
            try:
                head = r.read(2)
                if len(head) < 2:
                    return None
                n = head[1] & 0x7F
                if n == 126:
                    n = struct.unpack(">H", r.read(2))[0]
                elif n == 127:
                    n = struct.unpack(">Q", r.read(8))[0]
                return json.loads(r.read(n))
            except socket.timeout:  # TimeoutError from Python 3.10 on
                return None
        return read, send

    def test_the_transcript_starts_empty_then_with_the_first_message(self):
        s = self.start("第一句")
        path = f"/api/v1/sessions/{s['machine_id']}/{s['session_id']}/items"
        self.assertEqual(self.get(path)["items"], [])  # the node's answer for a session claude has not written to yet
        time.sleep(1)
        items = self.get(path)["items"]
        self.assertEqual([(x["role"], x["kind"], x.get("text")) for x in items], [("user", "text", "第一句")])

    def test_the_first_message_is_pushed_to_the_open_chat(self):
        read, send = self.live()
        s = self.start("打开着")
        send({"type": "subscribe", "machine_id": s["machine_id"], "session_id": s["session_id"]})
        backlog = None
        while msg := read():
            if msg.get("type") != "items" or msg.get("session_id") != s["session_id"]:
                continue
            if backlog is None:  # the answer to subscribe: nothing written yet (MOCK_ASSISTANT_TRANSCRIPT_SECONDS 0.5)
                backlog = msg["items"]
                self.assertEqual(backlog, [])
                continue
            self.assertEqual([x.get("text") for x in msg["items"]], ["打开着"])
            return
        self.fail("no items push with the first message")

    def test_the_conversation_is_new_and_working(self):
        s = self.start("另一句")
        self.assertEqual(s["state"], "working")
        updated = datetime.fromisoformat(s["updated_at"].replace("Z", "+00:00"))
        self.assertLess((datetime.now(timezone.utc) - updated).total_seconds(), 60)
        listed = self.get("/api/v1/assistant")["conversations"]
        self.assertIn(s["session_id"], [c["session_id"] for c in listed])

    def test_the_session_list_is_pushed(self):
        read, _ = self.live()
        while (msg := read()) and not (msg.get("type") == "sessions" and msg["machine_id"] == "server"):
            pass  # hello, machines, then each machine's list on connect: wait for the assistant's, so it is not that one
        self.assertIsNotNone(msg, "no session list on connect")
        s = self.start("推送")
        while msg := read():
            if msg.get("type") == "sessions" and s["session_id"] in [x["session_id"] for x in msg["sessions"]]:
                self.assertEqual(msg["machine_id"], s["machine_id"])
                return
        self.fail("no sessions push with the new conversation")

    def test_inbox_folder_updates_keep_their_response_contract(self):
        def put_folder(item_id, folder):
            request = Request(self.base + f"/api/v1/inbox/{item_id}/folder",
                              data=json.dumps({"folder": folder}).encode(), method="PUT",
                              headers={"Content-Type": "application/json"})
            with urlopen(request, timeout=3) as response:
                return json.load(response)

        item_id = "lark:f1:100"
        self.addCleanup(put_folder, item_id, None)
        placed = put_folder(item_id, "Projects/Release")
        self.assertEqual((placed["folder"], placed["folder_by"]), ("Projects/Release", "user"))
        self.assertIn("version", placed)
        self.assertNotIn("context", placed)
        cleared = put_folder(item_id, None)
        self.assertNotIn("folder", cleared)
        self.assertNotIn("folder_by", cleared)
        with self.assertRaises(HTTPError) as invalid:
            put_folder(item_id, "///")
        self.assertEqual(invalid.exception.code, 400)
        with self.assertRaises(HTTPError) as missing:
            put_folder("missing", "Projects")
        self.assertEqual(missing.exception.code, 404)

    def test_start_fail_cuts_the_next_starts_off(self):
        # #1368: GET /mock/start-fail?n=1 cuts the next start off as the hub does when the link to the assistant drops.
        self.get("/mock/start-fail?n=1")
        self.addCleanup(self.get, "/mock/start-fail?n=0&delay=0")
        with self.assertRaises(HTTPError) as cut:
            self.start("断了")
        self.assertEqual(cut.exception.code, 502)
        body = json.load(cut.exception)
        self.assertEqual((body["code"], body["delivered"]), ("assistant_in_doubt", "unknown"))
        self.assertEqual(self.start("好了")["state"], "working")  # only the next one


if __name__ == "__main__":
    unittest.main()
