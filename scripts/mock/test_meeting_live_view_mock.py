"""The mock hub's live view (#3862 phase 2, docs/assistant/meeting-minutes.md "Phase 2: live view"): the feature beside
the meeting conversation, `lines` on GET …/live?from= (at most 500, `more`, 400 for a bad `from`), `topics` and
`topics_at`, the room setting made-up topics after a batch with a meeting_topics_set call and the tool's limits,
`lines` and `topics` in meeting_live, in `revs` and in `?parts=`, the follow-up's seed listing the topics, the trim taking
the live log (lines, asks and topics, their revisions 0 again, no event); and managers without it."""
import os
import re
import time
import unittest
import uuid

import test_meeting_conversation_mock as conversation
from test_meeting_attendance_mock import Hub, start

LINES = conversation.LINES


def block(text, title):
    """A batch's context block of that title, its body only (None when there is none)."""
    found = re.search(rf'<persona-context kind="context" title="{re.escape(title)}" url="">\n(.*?)</persona-context>', text, re.S)
    return found and found.group(1)


class MeetingLiveViewMockTests(conversation.Conversation, unittest.TestCase):
    websocket = conversation.MeetingConversationMockTests.websocket

    @classmethod
    def setUpClass(cls):
        cls.base, stop = start({})
        cls.addClassCleanup(stop)

    def setUp(self):
        self.call("POST", "/mock/meeting_attendance", {"feed": "ok", "answer": "ok", "answer_seconds": 3, "kb_read_only": False})
        self.call("POST", "/mock/meeting_conversation", {"room": "ok", "end_seconds": 180, "room_seconds": 2})
        self.call("POST", "/mock/meeting_live_view", {"on": True})

    def post_lines(self, rid, frm, lines):
        status, out = self.call("POST", f"/api/v1/recordings/{rid}/live", {"from": frm, "segments": lines})
        self.assertEqual(status, 200, out)
        return out

    def test_feature_beside_the_meeting_conversation(self):
        self.assertIn("meeting_live_view", self.call("GET", "/api/v1/health")[1]["features"])
        rid = str(uuid.uuid4())
        self.ready_room(rid)
        self.call("POST", "/mock/meeting_live_view", {"on": False})
        self.assertEqual(self.call("GET", "/mock/meeting_live_view")[1]["on"], False)
        self.assertNotIn("meeting_live_view", self.call("GET", "/api/v1/health")[1]["features"])
        live = self.live(rid, "?from=x")  # without it `from` is ignored
        self.assertFalse({"lines", "more", "topics", "topics_at"} & set(live))
        self.assertFalse({"lines", "topics"} & set(live["revs"]))
        self.assertEqual(self.call("POST", "/mock/meeting_topics", {"id": rid, "topics": []})[0], 404)
        self.call("POST", "/mock/meeting_live_view", {"on": True})
        self.assertEqual(self.live(rid)["topics"], [])

    def test_lines_from_a_position(self):
        rid = str(uuid.uuid4())
        self.ready_room(rid)
        self.post_lines(rid, 0, LINES + [{"start": 1810, "end": 1812, "text": ""}])
        live = self.live(rid)
        self.assertNotIn("lines", live)  # without from: none, so #2981 reads stay small
        self.assertEqual((live["topics"] != [], live["revs"]["lines"]), (True, 3))
        self.assertEqual(self.live(rid, "?from=0")["lines"], [
            {"n": 0, "start": 5, "end": 9, "text": LINES[0]["text"]},
            {"n": 1, "start": 1800, "end": 1806, "text": LINES[1]["text"]},
            {"n": 2, "start": 1810, "end": 1812, "text": ""}])
        tail = self.live(rid, "?from=1")
        self.assertEqual(([x["n"] for x in tail["lines"]], "more" in tail), ([1, 2], False))
        self.assertEqual(self.live(rid, "?from=3")["lines"], [])
        self.assertEqual(self.live(rid, "?from=9")["lines"], [])
        for bad in ("-1", "x", "", "1.5", "%201"):
            self.assertEqual(self.call("GET", f"/api/v1/recordings/{rid}/live?from={bad}")[0], 400, bad)
        # A bad `from` is checked after 410 and 404: a never-uploaded id is not found, a deleted one gone.
        status, out = self.call("GET", f"/api/v1/recordings/{uuid.uuid4()}/live?from=x")
        self.assertEqual((status, out["code"]), (404, "recording_not_found"))
        gone = str(uuid.uuid4())
        self.upload(gone, attendance="listener")
        self.call("DELETE", f"/api/v1/recordings/{gone}")
        status, out = self.call("GET", f"/api/v1/recordings/{gone}/live?from=-1")
        self.assertEqual((status, out["code"]), (410, "recording_deleted"))
        # ?parts=lines: its revision, and the lines only with from.
        self.assertEqual(self.live(rid, "?parts=lines"), {"revs": {"lines": 3}})
        self.assertEqual(self.live(rid, "?parts=lines&from=2"), {"lines": [{"n": 2, "start": 1810, "end": 1812, "text": ""}], "revs": {"lines": 3}})
        # At most 500 a read, `more` while there are more: read again from the last n plus one.
        many = [{"start": 2000 + i, "end": 2000 + i, "text": f"Line {i}"} for i in range(600)]
        for i in range(0, 600, 200):
            self.post_lines(rid, 3 + i, many[i:i + 200])
        first = self.live(rid, "?from=0")
        self.assertEqual((len(first["lines"]), first["more"], first["lines"][-1]["n"]), (500, True, 499))
        rest = self.live(rid, "?from=500")
        self.assertEqual((len(rest["lines"]), "more" in rest, rest["lines"][-1]), (103, False, {"n": 602, "start": 2599, "end": 2599, "text": "Line 599"}))

    def test_a_meeting_without_a_room_has_lines_and_no_topics(self):
        rid = str(uuid.uuid4())
        self.upload(rid, attendance="listener")  # a #2981 meeting
        self.post_lines(rid, 0, LINES)
        live = self.live(rid, "?from=0")
        self.assertEqual(([x["text"] for x in live["lines"]], live["topics"], live["revs"]["lines"]), ([x["text"] for x in LINES], [], 2))
        self.assertNotIn("topics_at", live)

    def test_the_room_sets_topics_after_a_batch(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid)
        live = self.live(rid)
        self.assertEqual((live["topics"], "topics_at" in live, live["revs"]["topics"]), ([], False, 0))
        self.post_lines(rid, 0, LINES[:1])
        live = self.live(rid)
        self.assertEqual(live["topics"], [
            {"title": "Today we look at the export flow", "state": "current", "at": 5, "note": "Raised by Ada at 0:05."},
            {"title": "Next steps", "state": "open"}])
        self.assertRegex(live["topics_at"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
        self.assertEqual(live["revs"]["topics"], 1)
        # The batch shows the topics so far after the lines in their block (as the hub's: no line of them starts with
        # "[", which the apps read as a line of the meeting), then the room calls meeting_topics_set and writes nothing.
        items = self.items(room["session_id"])
        batch = next(x for x in reversed(items) if x["kind"] == "text")
        self.assertIn("update the topics", batch["text"])
        self.assertIn("as your rules say; then write nothing in reply.", batch["text"])
        self.assertNotIn("meeting_topics_set", batch["text"])
        self.assertIn("[14:00] Ada: Today we look at the export flow.\n\nTopics so far: none yet.\n", block(batch["text"], "Design review · live"))
        self.assertEqual(batch["text"].count("<persona-context kind=\"context\""), 1)
        self.assertTrue(batch["text"].rstrip().endswith("</persona-context>"))  # not the end batch, which has words after its block
        after = items[items.index(batch) + 1:]
        # The topics' call first; with highlights (phase 2b, test_meeting_highlights_mock.py) the highlights' follows it.
        highlights = [] if os.environ.get("MOCK_NO_MEETING_HIGHLIGHTS") == "1" else ["mcp__persona__meeting_highlights_set"]
        self.assertEqual([(x["role"], x["kind"]) for x in after], [("assistant", "tool_use"), ("user", "tool_result")] * (1 + len(highlights)))
        self.assertEqual([x["tool_name"] for x in after if x["kind"] == "tool_use"], ["mcp__persona__meeting_topics_set"] + highlights)
        self.assertEqual((after[0]["tool_name"], after[0]["tool_input"]), ("mcp__persona__meeting_topics_set", {"topics": live["topics"]}))
        self.assertEqual((after[1]["text"], after[1]["is_error"]), ("Saved.", False))
        # The meeting moved on: the current topic is done, the new one current, the open one stays. ?parts=topics is the
        # topics only (the highlights are a part of their own).
        self.post_lines(rid, 1, LINES[1:])
        moved = self.live(rid, "?parts=topics")
        self.assertEqual(set(moved), {"topics", "topics_at", "revs"})
        self.assertEqual([(t["title"], t["state"]) for t in moved["topics"]],
                         [("Today we look at the export flow", "done"), ("Who owns the regression test", "current"), ("Next steps", "open")])
        self.assertEqual(moved["revs"], {"topics": 2})
        second = self.last_text(room["session_id"])
        self.assertEqual(block(second["text"], "Design review · live"), "[14:30] Bo: Who owns the regression test?\n")
        # It did not: the same topic again changes nothing.
        self.post_lines(rid, 2, [{"start": 1830, "end": 1833, "text": "Bo: Who owns the regression test?"}])
        self.assertEqual(self.live(rid)["revs"]["topics"], 2)
        # A batch with only blank lines moves the lines, not the topics.
        self.post_lines(rid, 3, [{"start": 1840, "end": 1841, "text": " "}])
        self.assertEqual((self.live(rid)["revs"]["lines"], self.live(rid)["revs"]["topics"]), (4, 2))
        self.assertNotIn("topics", self.meta(rid))  # never in detail.meta

    def test_a_room_that_comes_up_late_sets_topics_from_the_meeting_so_far(self):
        self.call("POST", "/mock/meeting_conversation", {"room": "slow", "room_seconds": 0.5})
        rid = str(uuid.uuid4())
        self.upload(rid, room={"attendance": "listener"})
        self.post_lines(rid, 0, LINES)
        self.assertEqual(self.live(rid)["topics"], [])  # no room yet
        self.wait(lambda: self.live(rid)["topics"], "the topics")
        self.assertEqual(self.live(rid)["topics"][0]["title"], "Today we look at the export flow")

    def test_chinese_topics_for_a_chinese_meeting(self):
        rid = str(uuid.uuid4())
        self.ready_room(rid, language="zh-Hans")
        self.post_lines(rid, 0, [{"start": 65, "end": 70, "text": "Ada：我们先看导出流程。"}])  # i18n-ignore: a Chinese meeting's line
        self.assertEqual(self.live(rid)["topics"], [
            {"title": "我们先看导出流程", "state": "current", "at": 65, "note": "Ada 在 1:05 提出。"},  # i18n-ignore: the mock's topic
            {"title": "下一步", "state": "open"}])  # i18n-ignore: the mock's topic

    def test_meeting_topics_set_refuses_what_breaks_a_limit(self):
        rid = str(uuid.uuid4())
        room = self.ready_room(rid)
        topic = {"title": "Pricing", "state": "open"}
        cases = [([topic] * 21, "21 topics, over the 20"),
                 ([dict(topic, title="x" * 81)], "topic 1's title is 81 characters, over the 80"),
                 ([topic, dict(topic, title=" ")], "topic 2 has no title"),
                 ([dict(topic, state="now")], "not done, current or open"),
                 ([dict(topic, state="current"), dict(topic, state="current")], "2 topics are current"),
                 ([dict(topic, at=-1)], "topic 1's at"),
                 ([dict(topic, note="n" * 201)], "note is 201 characters, over the 200"),
                 ("Pricing", "topics must be a list")]
        for topics, why in cases:
            status, out = self.call("POST", "/mock/meeting_topics", {"id": rid, "topics": topics})
            self.assertEqual(status, 400, topics)
            self.assertIn(why, out["error"])
        self.assertEqual((self.live(rid)["topics"], self.live(rid)["revs"]["topics"]), ([], 0))
        refused = [x for x in self.items(room["session_id"]) if x["kind"] == "tool_result"]
        self.assertEqual((len(refused), all(x["is_error"] for x in refused)), (len(cases), True))
        # At the limits: taken, titles and notes on one line, blank notes left out.
        full = [{"title": "  Export\ncap  " + "y" * 60, "state": "done", "at": 0, "note": " "}] + \
            [{"title": f"Topic {i}", "state": "open", "note": "z" * 200} for i in range(18)] + [{"title": "t" * 80, "state": "CURRENT ", "at": 12.5}]
        status, out = self.call("POST", "/mock/meeting_topics", {"id": rid, "topics": full})
        self.assertEqual(status, 200, out)
        self.assertEqual(len(out["topics"]), 20)
        self.assertEqual(out["topics"][0], {"title": "Export cap " + "y" * 60, "state": "done", "at": 0})
        self.assertEqual(out["topics"][-1], {"title": "t" * 80, "state": "current", "at": 12.5})
        self.assertEqual(self.live(rid)["topics"], out["topics"])
        self.assertEqual(self.call("POST", "/mock/meeting_topics", {"id": str(uuid.uuid4()), "topics": []})[0], 404)
        # The made-up topics keep within the limits too: the oldest done ones go first.
        for i in range(3):
            self.post_lines(rid, i, [{"start": 60 * i, "end": 60 * i + 5, "text": f"Ada: Point {i}"}])
        topics = self.live(rid)["topics"]
        self.assertEqual((len(topics), sum(t["state"] == "current" for t in topics), topics[-19]["title"]), (20, 1, "Point 2"))

    def test_meeting_live_has_lines_and_topics(self):
        next_of = self.websocket()
        rid = str(uuid.uuid4())
        mine = lambda f: f.get("recording_id") == rid
        self.ready_room(rid)
        self.post_lines(rid, 0, LINES)
        seen = {}
        end = time.monotonic() + 5
        while not {"lines", "topics"} <= set(seen) and time.monotonic() < end:
            got = next_of("meeting_live", where=mine)
            self.assertIsNotNone(got, "an event with the lines and the topics")
            self.assertEqual(set(got), {"type", "recording_id", "changed", "revs"})  # content-free
            seen.update({p: got["revs"][p] for p in got["changed"] if p in ("lines", "topics")})
        live = self.live(rid)
        self.assertEqual(seen, {"lines": 2, "topics": 1})
        self.assertEqual((live["revs"]["lines"], live["revs"]["topics"]), (2, 1))

    def seed(self, rid):
        status, out = self.call("POST", f"/api/v1/recordings/{rid}/followup", {"id": str(uuid.uuid4()), "text": "Draft it."})
        self.assertEqual(status, 200, out)
        return next(x["text"] for x in self.items(out["followup"]["session_id"]) if x["kind"] == "text" and x["role"] == "user")

    def test_the_seed_lists_the_topics_after_the_minutes(self):
        self.call("POST", "/mock/recording_minutes", {"mode": "ready"})
        self.addCleanup(self.call, "POST", "/mock/recording_minutes", {"mode": "pending"})
        rid = str(uuid.uuid4())
        self.ready_room(rid, segments=LINES)
        self.post_lines(rid, 0, LINES)
        self.upload(rid, segments=LINES, complete=True, ended_at="2026-11-10T15:00:00+08:00", room={"attendance": "listener"})
        seed = self.seed(rid)
        topics = ("\nTopics (as the meeting's room kept them: done, current or open):\n"
                  "- current: Today we look at the export flow (from 0:05 into the meeting): Raised by Ada at 0:05.\n- open: Next steps\n")
        self.assertIn(topics, seed)
        self.assertLess(seed.index("\nMinutes:\n"), seed.index(topics))
        self.assertLess(seed.index(topics), seed.index("\nTranscript:\n"))

    def test_the_trim_takes_the_live_log(self):
        self.call("POST", "/mock/recording_minutes", {"mode": "ready"})
        self.addCleanup(self.call, "POST", "/mock/recording_minutes", {"mode": "pending"})
        next_of = self.websocket()
        mine = lambda f: f.get("recording_id") == rid
        rid = str(uuid.uuid4())
        self.ready_room(rid, segments=LINES)
        self.post_lines(rid, 0, LINES)
        self.assertEqual(self.call("POST", f"/api/v1/recordings/{rid}/ask", {"id": str(uuid.uuid4()), "question": "Who owns the test?"})[0], 200)
        self.upload(rid, segments=LINES, complete=True, ended_at="2026-11-10T15:00:00+08:00", room={"attendance": "listener"})
        before = self.live(rid, "?from=0")
        self.assertTrue(before["lines"] and before["asks"] and before["topics"])
        self.assertTrue(all(before["revs"][p] > 0 for p in ("lines", "asks", "topics")))
        while next_of("meeting_live", wait=1.5, where=mine):  # what the lines, the ask and the end moved has gone out
            pass
        self.assertEqual(self.call("POST", "/mock/recording_trim", {"id": rid})[0], 200)
        # As the hub's trim removes live.json: lines, asks and topics empty, their revisions 0 again, the role off.
        live = self.live(rid, "?from=0")
        self.assertEqual((live["next"], live["fed"], live["attendance"], live["lines"], live["asks"], live["topics"], "topics_at" in live),
                         (0, 0, "off", [], [], [], False))
        self.assertEqual({p: live["revs"][p] for p in ("lines", "asks", "topics")}, {"lines": 0, "asks": 0, "topics": 0})
        # No meeting_live for them: only the room's deletion goes out.
        while (got := next_of("meeting_live", wait=1.5, where=mine)) is not None:
            self.assertEqual(got["changed"], ["room"])
        self.assertNotIn(rid, self.call("GET", "/mock/meeting_live_view")[1]["topics"])
        # A trimmed recording's seed has its minutes only: no topics, no asks.
        seed = self.seed(rid)
        self.assertIn("\nMinutes:\n", seed)
        self.assertNotIn("Topics (", seed)
        self.assertNotIn("Asked in the room", seed)


class WithoutLiveViewMockTests(conversation.Conversation, unittest.TestCase):
    def test_a_manager_without_meeting_live_view(self):
        self.base, stop = start({"MOCK_NO_MEETING_LIVE_VIEW": "1"})
        try:
            features = self.call("GET", "/api/v1/health")[1]["features"]
            self.assertIn("meeting_conversation", features)
            self.assertNotIn("meeting_live_view", features)
            rid = str(uuid.uuid4())
            room = self.ready_room(rid)
            self.call("POST", f"/api/v1/recordings/{rid}/live", {"from": 0, "segments": LINES})
            live = self.live(rid, "?from=-1")  # ignored
            self.assertFalse({"lines", "more", "topics", "topics_at"} & set(live))
            self.assertEqual(set(live["revs"]), {"asks", "room"})
            self.assertEqual(self.live(rid, "?parts=lines,topics"), {"revs": {}})
            items = self.items(room["session_id"])
            self.assertFalse(any(x["kind"] == "tool_use" for x in items))
            self.assertNotIn("Topics so far", items[-1]["text"])
            self.assertIn("Write nothing in reply.", items[-1]["text"])
            self.assertEqual(self.call("POST", "/mock/meeting_topics", {"id": rid, "topics": []})[0], 404)
        finally:
            stop()
        self.base, stop = start({"MOCK_NO_MEETING_CONVERSATION": "1"})  # live view comes only with the meeting conversation
        try:
            self.assertNotIn("meeting_live_view", self.call("GET", "/api/v1/health")[1]["features"])
            rid = str(uuid.uuid4())
            self.upload(rid, attendance="listener")
            live = self.live(rid, "?from=0")
            self.assertFalse({"lines", "topics", "revs"} & set(live))
        finally:
            stop()


if __name__ == "__main__":
    unittest.main()
