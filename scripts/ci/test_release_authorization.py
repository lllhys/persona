import os
from pathlib import Path
import subprocess
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
WORKFLOW = ROOT / ".github/workflows/release.yml"


class ReleaseAuthorizationTests(unittest.TestCase):
    def setUp(self):
        self.workflow = yaml.load(WORKFLOW.read_text(), Loader=yaml.BaseLoader)
        self.jobs = self.workflow["jobs"]
        self.script = self.jobs["authorize"]["steps"][0]["run"]

    def run_authorization(self, actor, triggering_actor):
        return subprocess.run(
            ["bash", "-euo", "pipefail", "-c", self.script],
            cwd=ROOT,
            env={**os.environ, "ACTOR": actor, "TRIGGERING_ACTOR": triggering_actor},
            capture_output=True,
            text=True,
            check=False,
        )

    def test_only_chyroc_can_dispatch_or_rerun_a_release(self):
        self.assertEqual(self.run_authorization("chyroc", "chyroc").returncode, 0)
        for actor, triggering_actor in (
            ("wispyard", "wispyard"),
            ("chyroc", "wispyard"),
            ("wispyard", "chyroc"),
            ("github-actions[bot]", "chyroc"),
            ("", ""),
        ):
            with self.subTest(actor=actor, triggering_actor=triggering_actor):
                result = self.run_authorization(actor, triggering_actor)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("Only chyroc", result.stderr)

    def test_authorization_precedes_every_checkout_build_and_upload(self):
        self.assertEqual(self.jobs["authorize"]["runs-on"], "ubuntu-latest")
        self.assertNotIn("permissions", self.jobs["authorize"])
        self.assertEqual(self.jobs["smoke"]["needs"], "authorize")
        self.assertEqual(self.jobs["publish"]["needs"], ["authorize", "smoke"])
        self.assertEqual(self.workflow["permissions"]["contents"], "read")
        self.assertEqual(self.jobs["publish"]["permissions"], {"contents": "write", "packages": "write"})
        for job_name in ("smoke", "publish"):
            for step in self.jobs[job_name]["steps"]:
                with self.subTest(job=job_name, step=step.get("name", step.get("uses"))):
                    self.assertNotIn("github.actor ==", str(step))
                    self.assertNotIn("github.triggering_actor ==", str(step))

    def test_actor_values_are_environment_data_not_shell_source(self):
        steps = self.jobs["authorize"]["steps"]
        self.assertEqual(steps[0]["env"], {
            "ACTOR": "${{ github.actor }}",
            "TRIGGERING_ACTOR": "${{ github.triggering_actor }}",
        })
        self.assertNotIn("${{", self.script)
        self.assertRegex(self.script, r'"\$ACTOR" != chyroc')
        self.assertRegex(self.script, r'"\$TRIGGERING_ACTOR" != chyroc')

    def test_tag_ruleset_matches_the_workflow_boundary(self):
        text = (ROOT / "docs/operations/release.md").read_text()
        self.assertIn("Release tags: administrators only", text)
        self.assertIn("create, update or delete a `v*` tag", text)
        self.assertIn("both the run's actor and its triggering actor", text)
        self.assertNotIn("refs/tags/v", str(self.workflow["on"]["push"]["tags"]))
        self.assertEqual(self.workflow["on"]["push"]["tags"], ["v*"])


if __name__ == "__main__":
    unittest.main()
