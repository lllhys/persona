import unittest

from apple_subagent_results import deliveries


class AppleSubagentFixtureTests(unittest.TestCase):
    def test_results_keep_the_original_call_and_independent_report_ids(self):
        source = [
            {"id": "use", "seq": 1, "kind": "tool_use", "tool_name": "Agent", "tool_use_id": "a", "tool_input": {"description": "Review"}},
            {"id": "receipt", "seq": 2, "kind": "tool_result", "tool_use_id": "a", "text": "Async agent launched successfully."},
            {"id": "use-b", "seq": 3, "kind": "tool_use", "tool_name": "Agent", "tool_use_id": "b"},
            {"id": "result-b", "seq": 4, "kind": "tool_result", "tool_use_id": "b", "text": "Final report."},
        ]
        result = deliveries(source, [{"id": "worker-a", "tool_use_id": "a"}])
        self.assertEqual(len(result), 6)
        self.assertNotIn("task", source[1])
        self.assertEqual(result[1]["task"]["status"], "running")
        self.assertNotIn("detail", result[1]["task"])
        self.assertEqual(result[-2]["id"], "receipt-interim")
        self.assertEqual(result[-2]["task"]["status"], "running")
        self.assertEqual(result[-2]["tool_use_id"], "a")
        self.assertEqual(result[-1]["id"], "result-b-status")
        self.assertEqual(result[-1]["task"]["status"], "completed")
        self.assertNotIn("text", result[-1])
        self.assertNotIn("detail", result[-1]["task"])

    def test_compatibility_preview_has_a_byte_budget(self):
        source = [
            {"id": "use", "seq": 1, "kind": "tool_use", "tool_name": "Agent", "tool_use_id": "a"},
            {"id": "report", "seq": 2, "kind": "tool_result", "tool_use_id": "a", "text": "🧪" * 4096, "full_size": 16384},
        ]
        result = deliveries(source, [])
        self.assertLessEqual(len(result[1]["task"]["detail"].encode("utf-8")), 4096)
        self.assertEqual(result[1]["full_size"], 16384)
        self.assertNotIn("full_size", result[-1])


if __name__ == "__main__":
    unittest.main()
