#!/usr/bin/env python3
"""scripts/app/share-from-apps.sh: what its third argument selects with -only-testing (#2282)."""
import os
import subprocess
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
SCRIPT = os.path.join(ROOT, "scripts", "app", "share-from-apps.sh")


def selected(arg):
    return subprocess.run(["bash", SCRIPT, "--only-testing", arg], capture_output=True, text=True, check=True,
                          timeout=10).stdout.strip()


class OnlyTestingTests(unittest.TestCase):
    def test_every_form_names_one_bundle_once(self):
        cases = {
            "": "PersonaUITests/ShareFromAppsTests",
            "testSafariSendsThePagesTitleAndLink": "PersonaUITests/ShareFromAppsTests/testSafariSendsThePagesTitleAndLink",
            "ShareSheetTests": "PersonaUITests/ShareSheetTests",
            "ShareSheetTests/testLargeFiles": "PersonaUITests/ShareSheetTests/testLargeFiles",
            "PersonaUITests/ShareFromAppsTests/testPhotos": "PersonaUITests/ShareFromAppsTests/testPhotos",
            "PersonaUITests/ShareSheetTests": "PersonaUITests/ShareSheetTests",
        }
        for arg, want in cases.items():
            with self.subTest(arg=arg):
                self.assertEqual(selected(arg), want)
                self.assertNotIn("PersonaUITests/PersonaUITests", selected(arg))


if __name__ == "__main__":
    unittest.main()
