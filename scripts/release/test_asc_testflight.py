"""asc_testflight.py: beta group lookup (duplicates, flaky listings, when a group may be created, #945) and the review
submission limit (#4034)."""
import atexit
import contextlib
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE = Path(__file__).resolve().parent
_config = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
json.dump({"app": {"bundle_id": "com.example.app"},
           "app_store": {"app_id": "1234567890", "external": {"group": "Friends"}}}, _config)
_config.close()
atexit.register(os.unlink, _config.name)
os.environ["PERSONA_CONFIG"] = _config.name
sys.path.insert(0, str(HERE))
sys.path.insert(0, str(HERE.parent))
import asc_testflight as tf  # noqa: E402


def group(gid, name, created, internal=False):
    return {"id": gid, "attributes": {"name": name, "isInternalGroup": internal, "createdDate": created,
                                      "publicLinkEnabled": False}}


INTERNAL = group("int", "Internal", "2026-10-24T13:18:34Z", internal=True)
FRIENDS_OLD = group("old", "Friends", "2026-10-28T12:52:00Z")
FRIENDS_NEW = group("new", "Friends", "2026-11-03T14:03:00Z")


class FindGroupTests(unittest.TestCase):
    def setUp(self):
        patcher = patch.object(tf.time, "sleep")
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(setattr, tf, "CREATE_GROUPS", False)

    def test_duplicates_resolve_to_the_oldest(self):
        with patch.object(tf, "get_all", return_value=[FRIENDS_NEW, INTERNAL, FRIENDS_OLD]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_kind_must_match(self):
        with patch.object(tf, "get_all", return_value=[group("x", "Friends", "2026-01-01", internal=True)]):
            self.assertIsNone(tf.find_group("Friends", internal=False))

    def test_empty_listing_then_complete_one(self):
        with patch.object(tf, "get_all", side_effect=[[], [INTERNAL, FRIENDS_OLD]]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_timeout_then_listing(self):
        with patch.object(tf, "get_all", side_effect=[TimeoutError("read timed out"), [FRIENDS_OLD]]):
            self.assertEqual(tf.find_group("Friends", internal=False)["id"], "old")

    def test_missing_group_is_not_created_by_wait(self):
        with patch.object(tf, "get_all", return_value=[INTERNAL]), patch.object(tf, "request") as request:
            with self.assertRaises(tf.GroupMissing):
                tf.external_group_id()
        request.assert_not_called()

    def test_missing_internal_group_is_not_created_by_wait(self):
        with patch.object(tf, "get_all", return_value=[]), patch.object(tf, "request") as request:
            with self.assertRaises(tf.GroupMissing):
                tf.group_id()
        request.assert_not_called()

    def test_setup_creates_a_missing_group(self):
        tf.CREATE_GROUPS = True
        created = group("made", "Friends", "2026-11-04T00:00:00Z")
        with patch.object(tf, "get_all", return_value=[INTERNAL]), \
                patch.object(tf, "request", return_value={"data": created}) as request, \
                patch.object(tf, "public_link"):
            self.assertEqual(tf.external_group_id(), "made")
        self.assertEqual(request.call_args[0][:2], ("POST", "/v1/betaGroups"))

    def test_share_external_reports_a_missing_group_without_failing(self):
        with patch.object(tf, "get_all", return_value=[INTERNAL]), patch.object(tf, "request") as request:
            tf.share_external({"id": "b", "attributes": {"version": "1"}})
        request.assert_not_called()


LIMIT = tf.APIError(422, json.dumps({"errors": [{"status": "422", "code": "ENTITY_UNPROCESSABLE.SUBMISSION_LIMIT_REACHED",
                                                 "title": "Submission limit has been reached."}]}))
BUILD = {"id": "b226", "attributes": {"version": "226"}}


class SubmissionLimitTests(unittest.TestCase):
    """422 SUBMISSION_LIMIT_REACHED is a wait for a later catch-up round, not a failure, and is never asked again (#4034)."""

    def setUp(self):
        for patcher in (patch.object(tf, "external_state", return_value="READY_FOR_BETA_SUBMISSION"),
                        patch.object(tf, "external_group_id", return_value="friends"),
                        patch.object(tf, "get_all", return_value=[{"id": "b226"}]),
                        patch.object(tf, "write_review_details", return_value=True),
                        patch.object(tf, "add_tester"),
                        patch.dict(tf.EXTERNAL, {"group": "Friends", "testers": [{"email": "friend@example.com"}]})):
            patcher.start()
            self.addCleanup(patcher.stop)

    def submissions(self, request):
        return [c for c in request.call_args_list if c.args[:2] == ("POST", "/v1/betaAppReviewSubmissions")]

    def test_one_line_and_no_second_try(self):
        out = io.StringIO()
        with patch.object(tf, "request", side_effect=LIMIT) as request, contextlib.redirect_stdout(out):
            self.assertFalse(tf.submit_for_review(BUILD))
        self.assertEqual(len(self.submissions(request)), 1)
        lines = out.getvalue().strip().splitlines()
        self.assertEqual(len(lines), 1)
        self.assertIn("submission limit is used up", lines[0])
        self.assertIn("catch-up", lines[0])

    def test_catch_up_waits_instead_of_failing(self):
        with patch.object(tf, "newest_valid", return_value=BUILD), patch.object(tf, "request", side_effect=LIMIT), \
                contextlib.redirect_stdout(io.StringIO()):
            tf.catch_up()  # returns: auto-tf's catch-up step no longer exits 1
        tf.add_tester.assert_not_called()  # testers wait for the build that is submitted

    def test_release_wait_does_not_suggest_running_external_again(self):
        out = io.StringIO()
        with patch.object(tf, "request", side_effect=LIMIT), contextlib.redirect_stdout(out):
            tf.share_external(BUILD)
        self.assertNotIn("retry: python3", out.getvalue())
        self.assertIn("submission limit is used up", out.getvalue())

    def test_other_422s_still_fail(self):
        other = tf.APIError(422, '{"errors": [{"code": "ENTITY_UNPROCESSABLE.ANOTHER_BUILD_IN_REVIEW"}]}')
        with patch.object(tf, "request", side_effect=other), self.assertRaises(tf.APIError):
            tf.submit_for_review(BUILD)

    def test_submitted_build_invites_testers(self):
        with patch.object(tf, "request", return_value={"data": {}}), contextlib.redirect_stdout(io.StringIO()):
            self.assertTrue(tf.submit_for_review(BUILD))
            tf.share(BUILD)
        tf.add_tester.assert_called_once()


class MainArgumentTests(unittest.TestCase):
    def setUp(self):
        self.addCleanup(setattr, tf, "PLATFORM", tf.PLATFORM)

    def test_an_unknown_command_or_option_exits_with_the_usage_and_calls_nothing(self):
        for argv in (["nex-build"], ["--platfrom", "mac", "status"]):
            with self.subTest(argv=argv), patch.object(sys, "argv", ["asc_testflight.py", *argv]), \
                    patch.object(tf, "request") as request, patch.object(tf, "get_all") as get_all:
                with self.assertRaises(SystemExit) as raised:
                    tf.main()
                self.assertIn("asc_testflight.py next-build", str(raised.exception.code))
                request.assert_not_called()
                get_all.assert_not_called()


if __name__ == "__main__":
    unittest.main()
