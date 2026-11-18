#!/usr/bin/env python3
"""The Release archive keeps DEBUG runtime and snapshot facilities out (#9665)."""
import re
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/release/release.sh"


class ReleaseDebugGuardTest(unittest.TestCase):
    def test_archive_checks_runtime_and_snapshot_switches(self):
        text = SCRIPT.read_text()
        self.assertIn('strings "$APP/Persona" | grep -qE', text)
        self.assertIn("DEBUG runtime or snapshot switches found in the Release binary", text)
        for switch in ("runtimeCLI", "RuntimeCLI", "showActionTray", "openNeedsRow", "openPushIntercepts"):
            self.assertIn(switch, text)

    def test_the_same_rule_rejects_a_binary_and_allows_a_clean_one(self):
        # Exercise Release's complete command, rather than a second pattern copied into this test.
        text = SCRIPT.read_text()
        rule = re.search(r'^if strings "\$APP/Persona" \| grep -qE .*?^fi$', text, re.MULTILINE | re.DOTALL)
        self.assertIsNotNone(rule)
        command = rule.group(0).replace("$APP", "$TMP").replace("exit 1", 'echo rejected; exit 23')
        with tempfile.TemporaryDirectory(prefix="persona-release-debug-guard-") as directory:
            Path(directory, "Persona").write_text("runtimeCLI\nshowActionTray\n")
            contaminated = subprocess.run(["bash", "-c", command], env={"TMP": directory, "PATH": "/usr/bin:/bin"}, capture_output=True, text=True)
            self.assertEqual(23, contaminated.returncode)
            Path(directory, "Persona").write_text("an ordinary production executable\n")
            clean = subprocess.run(["bash", "-c", command], env={"TMP": directory, "PATH": "/usr/bin:/bin"}, capture_output=True, text=True)
            self.assertEqual(0, clean.returncode)


if __name__ == "__main__":
    unittest.main()
