"""Run the existing synthetic hub with Apple subagent tool-result deliveries."""

import argparse
import copy
import os
from http.server import ThreadingHTTPServer


def deliveries(items, agents):
    output = copy.deepcopy(items)
    calls = {item["tool_use_id"]: item for item in output
             if item.get("kind") == "tool_use" and item.get("tool_name") in ("Agent", "Task")}
    workers = {agent.get("tool_use_id"): agent for agent in agents}
    running = None
    completed = None
    for item in output:
        call = calls.get(item.get("tool_use_id"))
        if item.get("kind") != "tool_result" or call is None:
            continue
        receipt = item.get("text", "").startswith("Async agent launched successfully")
        status = "running" if receipt else "failed" if item.get("is_error") else "completed"
        task = {"kind": "agent", "status": status, "tool_use_id": item["tool_use_id"],
                "title": call.get("tool_input", {}).get("description", "Synthetic review")}
        worker = workers.get(item["tool_use_id"], {})
        if worker.get("id"):
            task["task_id"] = worker["id"]
        if not receipt and item.get("text"):
            task["detail"] = item["text"].encode("utf-8")[:4096].decode("utf-8", "ignore")
        item["task"] = task
        if receipt:
            running = item
        else:
            completed = item
    seq = max((item["seq"] for item in output), default=0)
    if running:
        report = copy.deepcopy(running)
        report.update(id=running["id"] + "-interim", seq=seq + 1,
                      text="Synthetic interim checks passed. The remaining checks are still running.")
        report["task"]["detail"] = report["text"]
        output.append(report)
        seq += 1
    if completed:
        pointer = copy.deepcopy(completed)
        pointer.update(id=completed["id"] + "-status", seq=seq + 1)
        pointer.pop("text", None)
        pointer.pop("full_size", None)
        pointer["task"].pop("detail", None)
        output.append(pointer)
    return output


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=18878)
    args = parser.parse_args()
    os.environ.update(PORT=str(args.port), MOCK_SUBAGENT_CARDS="1", MOCK_REPORT_FOLD="1")
    import mock_hub as hub
    session = next(session for session in hub.SESSIONS if session["session_id"] == hub.SUBCARDS)
    hub.ITEMS[hub.SUBCARDS] = deliveries(hub.ITEMS[hub.SUBCARDS], session.get("subagents", []))
    server = ThreadingHTTPServer(("127.0.0.1", args.port), hub.H)
    server.daemon_threads = True
    hub.ENDPOINTS.append(hub.endpoint("127.0.0.1", args.port, "http", "lan"))
    print("Synthetic Apple subagent protocol hub ready", flush=True)
    try:
        server.serve_forever()
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
