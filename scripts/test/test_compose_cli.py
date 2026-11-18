"""skip_without_compose skips a Compose check only when `docker compose` does not run in the check's environment."""
import os
from pathlib import Path
import tempfile
import unittest

from compose_cli import skip_without_compose

# A docker that answers `compose version` with the given exit status and stderr, as Docker 29 does without the plugin.
FAKE_DOCKER = '#!/bin/sh\n[ "$1 $2" = "compose version" ] || exit 99\nprintf "%s\\n" "$FAKE_STDERR" >&2\nexit "$FAKE_STATUS"\n'


class SkipWithoutComposeTests(unittest.TestCase):
    def environment(self, status, stderr=""):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        docker = Path(directory.name) / "docker"
        docker.write_text(FAKE_DOCKER)
        docker.chmod(0o755)
        return {"PATH": directory.name + os.pathsep + "/usr/bin:/bin", "HOME": directory.name,
                "FAKE_STATUS": str(status), "FAKE_STDERR": stderr}

    def test_a_compose_that_runs_is_not_skipped(self):
        skip_without_compose(self, self.environment(0))

    def test_docker_without_the_plugin_is_skipped_whatever_it_says(self):
        for stderr in ("docker: unknown command: docker compose", "docker: 'compose' is not a docker command.", ""):
            with self.subTest(stderr=stderr), self.assertRaises(unittest.SkipTest) as skipped:
                skip_without_compose(self, self.environment(1, stderr))
            self.assertIn("PERSONA_TEST_DOCKER_CONFIG", str(skipped.exception))
            if stderr:
                self.assertIn(stderr, str(skipped.exception))

    def test_no_docker_is_skipped(self):
        with tempfile.TemporaryDirectory() as directory, self.assertRaises(unittest.SkipTest):
            skip_without_compose(self, {"PATH": directory, "HOME": directory})


if __name__ == "__main__":
    unittest.main()
