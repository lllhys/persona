import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import sign_darwin_cli


class DarwinSigningTests(unittest.TestCase):
    def test_fixed_identifier_and_no_adhoc(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / "personad-darwin-arm64"
            path.write_bytes(b"binary")
            calls = []

            def run(argv, secret=False):
                calls.append(argv)
                if argv[2] == "-r-":
                    return 'designated => identifier "com.nwverse.personad" and anchor apple generic and certificate leaf[subject.OU] = "TEAM"'
                return ""

            with patch.object(sign_darwin_cli, "run", side_effect=run):
                sign_darwin_cli.sign_one(path, "identity")
            self.assertIn("--options", calls[0])
            self.assertIn("com.nwverse.personad", calls[0])
            self.assertNotIn("cdhash", calls[2])

    def test_rejects_adhoc(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "persona-node-darwin-arm64"
            path.write_bytes(b"binary")
            with patch.object(sign_darwin_cli, "run", return_value='designated => cdhash H"1234"'):
                with self.assertRaisesRegex(RuntimeError, "ad-hoc"):
                    sign_darwin_cli.sign_one(path, "identity")


if __name__ == "__main__":
    unittest.main()
