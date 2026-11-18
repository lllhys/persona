"""mock_hub.py's MOCK_PASEO_NOT_LOADED (#8321): a Grok agent Paseo let go, whose reads its bridge refuses as not_loaded
until a subscribe watches it (#8132, docs/integrations/paseo.md "Content"); the subscribe loads it and the backlog
brings its history, as the hub's does. The web's web/e2e/paseo-not-loaded.mjs runs against it."""
import base64
import json
import os
import socket
import struct
import unittest

from test_readiness_mock import hub

CLOSED = "paseo-9a5e0000-0000-4000-8000-000000000012"
ITEMS = "/api/v1/sessions/server/%s/items?turns=2&fold_work=1&lazy_images=1" % CLOSED


def live(test, port):
    """The app's socket: a reader of its JSON frames (None once nothing comes for 3 s) and a writer of frames up to 64 KB."""
    conn = socket.create_connection(("127.0.0.1", port), timeout=3)
    r = conn.makefile("rb")
    test.addCleanup(conn.close)
    test.addCleanup(r.close)
    key = base64.b64encode(os.urandom(16)).decode()
    conn.sendall((f"GET /api/v1/ws HTTP/1.1\r\nHost: 127.0.0.1\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                  f"Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n").encode())
    while r.readline() not in (b"\r\n", b""):
        pass

    def send(obj):  # unmasked: the mock reads either
        data = json.dumps(obj).encode()
        size = bytes([len(data)]) if len(data) < 126 else bytes([126]) + struct.pack(">H", len(data))
        conn.sendall(bytes([0x81]) + size + data)

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
        except socket.timeout:
            return None
    return read, send


class PaseoNotLoadedMockTests(unittest.TestCase):
    def test_reads_are_refused_until_a_subscribe_loads_the_agent(self):
        h = hub(self, MOCK_PASEO_NOT_LOADED="1", MOCK_PASEO_LOAD_SECONDS="0.3")
        _, listed = h.call("GET", "/api/v1/sessions")
        row = next(s for s in listed["sessions"] if s["session_id"] == CLOSED)
        self.assertEqual((row["status"], row["agent"], row["agent_label"]), ("offline", "paseo", "Grok"))
        status, out = h.call("GET", ITEMS)
        self.assertEqual((status, out.get("code")), (409, "not_loaded"))

        read, send = live(self, int(h.base.rsplit(":", 1)[1]))
        send({"type": "subscribe", "machine_id": "server", "session_id": CLOSED, "lazy_images": True, "fold_work": True})
        while (msg := read()) is not None:
            if msg.get("type") == "items" and msg.get("session_id") == CLOSED:
                break
        else:
            self.fail("no backlog for the subscribe")
        self.assertTrue(msg["backlog"])
        self.assertIn("has_more", msg)  # the page a subscribe without since_seq stands in for
        texts = [x.get("text") for x in msg["items"] if x.get("kind") == "text"]
        self.assertEqual(texts[-1], "Done: 212 photos in 9 albums, one per town.")

        status, out = h.call("GET", ITEMS)
        self.assertEqual(status, 200)
        self.assertTrue(out["items"])
        h.call("GET", "/mock/paseo-unload")
        status, out = h.call("GET", ITEMS)
        self.assertEqual((status, out.get("code")), (409, "not_loaded"))


if __name__ == "__main__":
    unittest.main()
