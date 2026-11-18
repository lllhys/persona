#!/usr/bin/env python3
import base64
import importlib.util
import os
import subprocess
import threading
from pathlib import Path
from urllib.parse import parse_qs, urlparse

os.environ["MOCK_FILES"] = "1"
root = Path(__file__).resolve().parents[3]
spec = importlib.util.spec_from_file_location("file_text_mock", root / "scripts/mock/mock_hub.py")
hub = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hub)
original_get = hub.H.do_GET
empty_reads = 0
reads_lock = threading.Lock()


def get(self):
    global empty_reads
    url = urlparse(self.path)
    query = parse_qs(url.query)
    if url.path == "/mock/preview-reset":
        with reads_lock:
            empty_reads = 0
        return self.send_json({"reset": True})
    if url.path == "/mock/preview-shot":
        directory, sim = os.environ.get("PERSONA_PREVIEW_SHOTS"), os.environ.get("PERSONA_PREVIEW_SIM")
        name = query.get("name", [""])[0]
        if not directory or not sim:
            return self.send_json({"saved": False})
        if not name or any(c not in "abcdefghijklmnopqrstuvwxyz0123456789.-" for c in name):
            return self.send_json({"error": "Invalid screenshot name"}, 400)
        output = Path(directory)
        output.mkdir(parents=True, exist_ok=True)
        try:
            subprocess.run(["xcrun", "simctl", "io", sim, "screenshot", str(output / (name + ".png"))], check=True, timeout=30)
        except (subprocess.SubprocessError, OSError) as error:
            return self.send_json({"error": str(error)}, 500)
        return self.send_json({"saved": True})
    path = query.get("path", [""])[0]
    if url.path.endswith("/file") and path.startswith("/tmp/preview/"):
        name = Path(path).name
        text = "Command output\nAll checks passed.\n"
        fields = {"path": path, "name": name, "size": len(text.encode()), "mime": "text/plain; charset=utf-8",
                  "truncated": False, "is_dir": False, "text": text}
        if name == "empty.output":
            with reads_lock:
                empty_reads += 1
                empty = empty_reads % 2
            if empty:
                fields.pop("text")
                fields["size"] = 0
            else:
                fields["text"] = "Command output is now available.\n"
                fields["size"] = len(fields["text"].encode())
        elif name == "task":
            fields.pop("text")
            fields["mime"] = "application/octet-stream"
            fields["data"] = base64.b64encode(text.encode()).decode()
        return self.send_json(fields)
    return original_get(self)


hub.H.do_GET = get
if __name__ == "__main__":
    server = hub.ThreadingHTTPServer(("127.0.0.1", hub.PORT), hub.H)
    server.daemon_threads = True
    hub.ENDPOINTS.append(hub.endpoint("127.0.0.1", hub.PORT, "http", "lan"))
    print(f"File text fixture ready on port {hub.PORT}", flush=True)
    try:
        server.serve_forever()
    finally:
        server.server_close()
