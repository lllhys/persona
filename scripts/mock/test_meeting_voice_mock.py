"""The mock's meeting calls (#3862 phase 4, docs/assistant/meeting-minutes.md "Phase 4: realtime voice in the meeting";
scripts/mock/voice_meeting_mock.py): `meeting` in the voice extension's features, a meeting call on a ready room
(ready, Talk spans, audio counted inside and outside them, the answer by script and in the room's thread, typed
questions), the fatal not_meeting_room for any other start in the mode, meeting_room for an ordinary call bound to a
room, talk frames ignored in an ordinary call, and the extension from before phase 4 (?meeting=off)."""
import struct
import time
import unittest
import uuid

from test_meeting_attendance_mock import start
from test_meeting_conversation_mock import Conversation
from test_voice_mock import Socket, TARGET

ZH_QUESTION = "上次导出上限是怎么定的？"
ZH_ANSWER = "上次定的是保留 4 MB 的导出上限，回归测试由 Bo 负责。"
EN_ANSWER = "You kept the 4 MB export cap; Bo owns the regression test."
PCM = b"\x01\x00" * 320  # 20 ms of 16 kHz PCM16


class MeetingVoiceMockTests(Conversation, unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base, stop = start({"MOCK_VOICE": "1", "MOCK_VOICE_PACE": "0.1"})
        cls.addClassCleanup(stop)
        cls.port = int(cls.base.rsplit(":", 1)[1])

    def setUp(self):
        self.call("POST", "/mock/meeting_attendance", {"feed": "ok", "answer": "ok", "answer_seconds": 3, "kb_read_only": False})
        self.call("POST", "/mock/meeting_conversation", {"room": "ok", "end_seconds": 180, "room_seconds": 2})
        self.log(reset=True)

    def log(self, reset=False, meeting=None):
        query = "&".join(([] if not reset else ["reset=1"]) + ([] if meeting is None else ["meeting=" + meeting]))
        status, out = self.call("GET", "/mock/voice_meeting" + ("?" + query if query else ""))
        self.assertEqual(status, 200)
        return out

    def socket(self, prefix="/api/v1/extensions"):
        ws = Socket(self.port, prefix + "/voice/ws")
        self.addCleanup(ws.close)
        self.assertEqual(ws.status, 101)
        return ws

    @staticmethod
    def meeting_start(rid, target, **extra):
        return {"type": "start", "protocol": 2, "mode": "meeting", "recording_id": rid,
                "target": {"machine_id": target["machine_id"], "session_id": target["session_id"]}, "echo_cancellation": True, **extra}

    def fatal(self, ws):
        """The fatal error frame and the close code that follows it."""
        error = ws.until(lambda f: f["type"] == "error")[-1]
        self.assertTrue(error["fatal"])
        while True:
            op, data = ws.frame()
            if op == 8:
                return error, struct.unpack(">H", data[:2])[0]

    def texts(self, sid):
        return [x for x in self.items(sid) if x["kind"] == "text"]

    def test_info_lists_meeting(self):
        for prefix in ("/api/v1/extensions", "/api/v1/plugins"):
            info = self.call("GET", prefix + "/voice/info")[1]
            self.assertEqual(info["protocol"], 2)
            self.assertIn("meeting", info["features"])
        try:
            self.assertFalse(self.log(meeting="off")["meeting"])
            self.assertNotIn("meeting", self.call("GET", "/api/v1/extensions/voice/info")[1]["features"])
        finally:
            self.assertTrue(self.log(meeting="on")["meeting"])

    def test_talk_span_is_heard_and_answered_in_the_room(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid, language="zh-Hans")
        ws = self.socket()
        ws.send(self.meeting_start(rid.upper(), room, model="voice-deep"))  # the phone's own UUID spelling
        ready = ws.until(lambda f: f["type"] == "ready")[-1]
        self.assertEqual((ready["protocol"], ready["input_rate"]), (2, 16000))
        self.assertIn("meeting", ready["features"])
        self.assertEqual({k: ready["target"][k] for k in ("machine_id", "session_id", "kind", "agent_label")},
                         {"machine_id": room["machine_id"], "session_id": room["session_id"], "kind": "assistant", "agent_label": "Persona"})
        for _ in range(3):
            ws.send_bytes(PCM)  # the meeting outside a span: the extension feeds silence instead
        ws.send({"type": "talk", "on": True})
        for _ in range(5):
            ws.send_bytes(PCM)
        ws.send({"type": "talk", "on": False})
        frames = ws.until(lambda f: f["type"] == "transcript" and f["role"] == "assistant", timeout=10)
        user = [f for f in frames if f["type"] == "transcript" and f["role"] == "user"]
        self.assertEqual([(f["id"], f["final"]) for f in user], [("u1", False), ("u1", True)])
        self.assertEqual(user[-1]["text"], ZH_QUESTION)
        runs = [f for f in frames if f["type"] == "run"]
        self.assertEqual([f["state"] for f in runs], ["sending", "sent", "working", "done"])
        self.assertRegex(runs[0]["id"], r"^voice-[0-9a-f]{16}$")
        self.assertEqual(runs[0]["task"], ZH_QUESTION)
        self.assertEqual(frames[-1]["text"], ZH_ANSWER)
        self.assertNotIn("link", [f["type"] for f in frames], "a meeting call never pauses")
        while ws.frame()[0] != 2:  # the answer's audio
            pass
        log = self.log()
        self.assertEqual((log["audio_out"], log["audio_in"], log["audio_in_bytes"]), (3, 5, 5 * len(PCM)))
        self.assertEqual((log["spans"], log["talk_on"], log["talk_off"], log["spans_empty"], log["answers"]), (1, 1, 1, 0, 1))
        self.assertEqual(log["start"], {"protocol": 2, "mode": "meeting", "recording_id": rid.upper(), "target": {k: room[k] for k in ("machine_id", "session_id")},
                                        "echo_cancellation": True, "model": "voice-deep", "voice": None, "recent": None, "history": None, "resume": None})
        # The question went to the room as the owner's message, and its answer is the room's.
        question, answer = self.texts(room["session_id"])[-2:]
        self.assertEqual((question["role"], question["text"], question["client_msg_id"]), ("user", ZH_QUESTION, runs[0]["id"]))
        self.assertNotIn("source", question)
        self.assertEqual((answer["role"], answer["text"]), ("assistant", ZH_ANSWER))
        ws.close()
        # A call picked up again: the same checks, its history and runs handed back.
        again = self.socket()
        again.send(self.meeting_start(rid, room, history=[{"role": "assistant", "text": ZH_ANSWER}], resume={"runs": []}))
        again.until(lambda f: f["type"] == "ready")
        log = self.log()
        self.assertEqual((log["calls"], log["start"]["history"], log["start"]["resume"]), (2, [{"role": "assistant", "text": ZH_ANSWER}], {"runs": []}))

    def test_empty_span_repeats_and_typed_questions(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid)
        ws = self.socket("/api/v1/plugins")
        ws.send(self.meeting_start(rid, room, echo_cancellation=False))
        ws.until(lambda f: f["type"] == "ready")
        ws.send({"type": "talk", "on": True})
        ws.send({"type": "talk", "on": True})  # again: changes nothing
        ws.send({"type": "talk", "on": "yes"})  # unreadable: changes nothing
        ws.send({"type": "talk"})  # no `on` is off: nothing heard, nothing to answer
        ws.send({"type": "talk", "on": False})
        ws.send_bytes(PCM)
        time.sleep(0.6)  # longer than the script for a span (4.2 s at pace 0.1)
        ws.send({"type": "text", "text": "Who owns the regression test?"})
        frames = ws.until(lambda f: f["type"] == "transcript" and f["role"] == "assistant", timeout=10)
        self.assertEqual([(f["id"], f["text"]) for f in frames if f["type"] == "transcript"],
                         [("t1", "Who owns the regression test?"), ("a1", EN_ANSWER)])
        self.assertEqual([f["state"] for f in frames if f["type"] == "run"], ["sending", "sent", "working", "done"])
        log = self.log()
        self.assertEqual((log["spans"], log["spans_empty"], log["talk_repeats"], log["audio_in"], log["audio_out"]), (1, 1, 3, 0, 1))
        self.assertEqual((log["texts"], log["answers"], log["start"]["echo_cancellation"]), (["Who owns the regression test?"], 1, False))
        self.assertEqual([x["text"] for x in self.texts(room["session_id"])[-2:]], ["Who owns the regression test?", EN_ANSWER])

    def test_a_question_after_the_meeting_ended_fails_its_run(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid)
        ws = self.socket()
        ws.send(self.meeting_start(rid, room))
        ws.until(lambda f: f["type"] == "ready")
        self.upload(rid, room={"attendance": "participant"}, ended_at="2026-11-10T15:00:00+08:00", complete=True)
        self.wait(lambda: self.live(rid)["room"]["state"] == "ended", "the room's end")
        ws.send({"type": "text", "text": "One more thing"})
        failed = ws.until(lambda f: f["type"] == "run" and f["state"] == "failed", timeout=10)[-1]
        self.assertEqual(failed["code"], "meeting_ended")
        self.assertNotIn("One more thing", [x["text"] for x in self.texts(room["session_id"])])

    def test_not_meeting_room(self):
        ready = str(uuid.uuid4())
        room = self.ready_room(ready)
        plain = str(uuid.uuid4())
        self.upload(plain)  # a meeting without a room
        ended = str(uuid.uuid4())
        ended_room = self.ready_room(ended)
        self.upload(ended, room={"attendance": "participant"}, ended_at="2026-11-10T15:00:00+08:00", complete=True)
        self.wait(lambda: self.live(ended)["room"]["state"] == "ended", "the room's end")
        self.call("POST", "/mock/meeting_conversation", {"room": "slow", "room_seconds": 30})
        starting = str(uuid.uuid4())
        self.upload(starting, room={"attendance": "participant"})
        self.wait(lambda: self.live(starting)["room"]["state"] == "starting", "the room starting")
        starting_room = {"machine_id": "server", "session_id": str(uuid.uuid5(uuid.NAMESPACE_URL, f"persona-meeting:{starting}:room"))}
        cases = [
            ("no such recording", self.meeting_start(str(uuid.uuid4()), room)),
            ("not a room meeting", self.meeting_start(plain, room)),
            ("the target is not this recording's room", self.meeting_start(ready, TARGET)),
            ("the target is not this recording's room", self.meeting_start(ready, ended_room)),
            ("the target is not this recording's room", self.meeting_start(starting, starting_room)),  # no ids until ready
            ("the room is ended", self.meeting_start(ended, ended_room)),
            ("no recording_id", self.meeting_start("", room)),
            ("no target", dict(self.meeting_start(ready, room), target=None)),
            ("not protocol 2", dict(self.meeting_start(ready, room), protocol=1)),
        ]
        for why, frame in cases:
            ws = self.socket()
            ws.send(frame)
            error, close = self.fatal(ws)
            self.assertEqual((error["code"], close), ("not_meeting_room", 1011), why)
            self.assertEqual(self.log()["refused"][-1]["why"], why)
        self.assertEqual(self.log()["calls"], len(cases))

    def test_an_ordinary_call_bound_to_a_room_is_refused(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid)
        bound = {k: room[k] for k in ("machine_id", "session_id")}
        ws = self.socket()
        ws.send({"type": "start", "protocol": 2, "target": bound})
        error, close = self.fatal(ws)
        self.assertEqual((error["code"], close), ("meeting_room", 1011))
        self.assertEqual(self.log()["refused"][-1]["code"], "meeting_room")
        # Anywhere else the call is the ordinary one, which ignores talk frames.
        other = self.socket()
        other.send({"type": "start", "protocol": 2, "target": TARGET})
        self.assertEqual(other.until(lambda f: f["type"] == "ready")[-1]["target"]["kind"], "agent")
        other.send({"type": "talk", "on": True})
        other.send({"type": "talk", "on": False})
        other.send({"type": "text", "text": "顺便看下 lint"})
        other.until(lambda f: f["type"] == "transcript" and f["text"] == "顺便看下 lint")
        self.assertEqual(self.log()["talks_ignored"], 2)
        # An extension from before phase 4 knows neither: the room's call is an ordinary one.
        try:
            self.log(meeting="off")
            old = self.socket()
            old.send({"type": "start", "protocol": 2, "target": bound})
            self.assertNotIn("meeting", old.until(lambda f: f["type"] == "ready")[-1]["features"])
        finally:
            self.log(meeting="on")


if __name__ == "__main__":
    unittest.main()
