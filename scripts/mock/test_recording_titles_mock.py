"""The mock hub names recordings as the hub does (#8873, docs/assistant/meeting-minutes.md "Titles (#8873)"): with
MOCK_RECORDING_TITLES=1 (or POST /mock/recording_titles) a recording nobody named gets a made-up title once it has 40
characters of text; the owner's title and the calendar's come first. The upload's answer, GET /api/v1/recordings and the
detail carry name and name_source; without a name they carry neither."""
import time
import unittest
import uuid

from test_meeting_attendance_mock import Hub, start


class RecordingTitlesMockTests(Hub, unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base, stop = start({"MOCK_RECORDING_TITLES": "1"})
        cls.addClassCleanup(stop)

    def lines(self, *texts):
        return [{"start": 10 * i, "end": 10 * i + 9, "text": t} for i, t in enumerate(texts)]

    def test_names(self):
        rid = str(uuid.uuid4())
        # Too little said: no name anywhere.
        status, out = self.upload(rid, title="", segments=self.lines("大家好"))
        self.assertEqual(status, 200)
        self.assertNotIn("name", out)
        self.assertNotIn("name_source", out)
        # Enough: the subject before the colon.
        out = self.upload(rid, title="", segments=self.lines("导出：超过 4 MB 的条目会被丢掉", "这一版先保留上限，下周再看要不要按条还是按录音来算"))[1]
        self.assertEqual((out["name"], out["name_source"]), ("导出", "model"))
        rows = {r["id"]: r for r in self.call("GET", "/api/v1/recordings")[1]["recordings"]}
        self.assertEqual((rows[rid]["name"], rows[rid]["name_source"]), ("导出", "model"))
        meta = self.call("GET", "/api/v1/kb/recordings/" + rid)[1]["detail"]["meta"]
        self.assertEqual((meta["name"], meta["name_source"], meta["ai_title"], meta["ai_title_from"]), ("导出", "model", "导出", "live"))
        # The calendar's, then the owner's, come first.
        out = self.upload(rid, revision=int(time.time() * 1000) + 1, title="周会", calendar={"title": "周会"},
                          segments=self.lines("导出：超过 4 MB 的条目会被丢掉", "这一版先保留上限，下周再看要不要按条还是按录音来算"))[1]
        self.assertEqual((out["name"], out["name_source"]), ("周会", "calendar"))
        out = self.upload(rid, revision=int(time.time() * 1000) + 2, title="我起的名字", calendar={"title": "周会"})[1]
        self.assertEqual((out["name"], out["name_source"]), ("我起的名字", "title"))
        # Switched off: a recording nobody named has no name again.
        self.assertEqual(self.call("POST", "/mock/recording_titles", {"on": "yes"})[0], 400)
        self.assertEqual(self.call("POST", "/mock/recording_titles", {"on": False})[1], {"on": False})
        other = str(uuid.uuid4())
        out = self.upload(other, title="", segments=self.lines("导出：超过 4 MB 的条目会被丢掉", "这一版先保留上限，下周再看要不要按条还是按录音来算"))[1]
        self.assertNotIn("name", out)
        self.call("POST", "/mock/recording_titles", {"on": True})

    def test_web_rename(self):
        rid = str(uuid.uuid4())
        p = f"/api/v1/recordings/{rid}/title"
        self.assertEqual(self.call("PUT", p, {"title": "x"})[1]["code"], "recording_not_found")
        rev = int(time.time() * 1000)
        self.upload(rid, revision=rev, title="Design review")
        for bad in ({"title": None}, {"title": "two\nlines"}, {"title": "长" * 201}, {}):
            self.assertEqual(self.call("PUT", p, bad)[0], 400, bad)
        self.assertEqual(self.call("PUT", p, {"title": " Export cap "})[1], {"name": "Export cap", "name_source": "renamed"})
        rows = {r["id"]: r for r in self.call("GET", "/api/v1/recordings")[1]["recordings"]}
        self.assertEqual(rows[rid]["name_source"], "renamed")
        self.assertEqual(self.call("GET", "/api/v1/kb/recordings/" + rid)[1]["detail"]["meta"]["renamed"], "Export cap")
        # The phone's title as it was keeps it; a new one takes it back.
        self.assertEqual(self.upload(rid, revision=rev + 1, title="Design review")[1]["name"], "Export cap")
        out = self.upload(rid, revision=rev + 2, title="Release date")[1]
        self.assertEqual((out["name"], out["name_source"]), ("Release date", "title"))
        self.call("PUT", p, {"title": "Export cap"})
        self.assertEqual(self.call("PUT", p, {"title": ""})[1]["name"], "Release date")
        self.call("DELETE", f"/api/v1/recordings/{rid}")
        self.assertEqual(self.call("PUT", p, {"title": "x"})[1]["code"], "recording_deleted")


if __name__ == "__main__":
    unittest.main()
