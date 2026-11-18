"""Tests mock_hub's decision row (MOCK_TASKS=1 MOCK_TASK_DECISION=1): decision_review on the task that waits for the owner,
POST /todos/{id}/decision-dismiss and decision-undo-dismiss with the hub's 400 / 409 rules, and MOCK_TASKS_STARTING."""
import json
from pathlib import Path
import sys
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen

sys.path.insert(0, str(Path(__file__).parent))
from test_task_briefs_mock import MockHub  # noqa: E402

TASK = "todo_task_tests"


class DecisionDismissMockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.hub = MockHub(MOCK_TASKS="1", MOCK_TASK_DECISION="1", MOCK_TASKS_STARTING="1")
        cls.addClassCleanup(cls.hub.stop)

    def post(self, path, body):
        request = Request(self.hub.base + path, json.dumps(body).encode(), method="POST",
                          headers={"Content-Type": "application/json", "Authorization": "Bearer mock"})
        try:
            with urlopen(request, timeout=3) as response:
                return response.status, json.load(response)
        except HTTPError as error:
            return error.code, json.load(error)

    def todo(self, tid=TASK):
        return {t["id"]: t for t in self.hub.request("/api/v1/todos")["todos"]}[tid]

    def setUp(self):
        self.post("/mock/reset", {})

    def test_decision_review_only_on_the_waiting_task(self):
        review = self.todo()["decision_review"]
        self.assertTrue(review["review_at"])
        self.assertNotIn("dismissal_id", review)
        self.assertNotIn("decision_review", self.todo("todo_task_widget"))

    def test_dismiss_then_undo(self):
        at = self.todo()["decision_review"]["review_at"]
        status, got = self.post(f"/api/v1/todos/{TASK}/decision-dismiss", {"review_at": at})
        self.assertEqual((status, got["review_at"]), (200, at))
        receipt = got["dismissal_id"]
        self.assertEqual(self.todo()["decision_review"]["dismissal_id"], receipt)
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-undo-dismiss", {"review_at": at, "dismissal_id": receipt})[0], 200)
        self.assertNotIn("dismissal_id", self.todo()["decision_review"])
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-undo-dismiss", {"review_at": at, "dismissal_id": receipt})[0], 409)

    def test_stale_round_and_bad_bodies(self):
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-dismiss", {"review_at": "2020-01-01T00:00:00Z"})[0], 409)
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-dismiss", {})[0], 400)
        at = self.todo()["decision_review"]["review_at"]
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-undo-dismiss", {"review_at": at})[0], 400)
        self.assertEqual(self.post(f"/api/v1/todos/{TASK}/decision-undo-dismiss", {"review_at": at, "dismissal_id": "nope"})[0], 409)
        self.assertEqual(self.post("/api/v1/todos/todo_task_widget/decision-dismiss", {"review_at": at})[0], 409)

    def test_starting_task_has_no_agent(self):
        starting = self.todo("todo_task_starting")
        self.assertEqual((starting["agents"], starting.get("start_error", "")), ([], ""))


if __name__ == "__main__":
    unittest.main()
