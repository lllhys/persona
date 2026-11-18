"""Review screenshots belong in the PR, not in the repository (#9030).

A screenshot taken to show a change (light / dark, before / after, a UI test's capture) is evidence for the review: once the
PR merges nothing reads it again, yet every checkout carries it. So screenshots are attached to the PR (CLAUDE.md,
"Workflow"), and this guard keeps new ones out of the tree.

What counts as a screenshot here: an image or video under docs/ (outside docs/assets/, the generated README and store
images), or anywhere under a directory whose name says screenshots or shots. App resources, website images, test fixtures
and extension examples live elsewhere and are not checked.

The screenshots that maintained docs still link to are listed in committed_screenshots_baseline.txt. The list can only
shrink: a new file fails, and so does a listed file that no longer exists (delete its line). Never add lines.
"""
from pathlib import Path
import re
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[2]
BASELINE = ROOT / "scripts/test/committed_screenshots_baseline.txt"
MEDIA = re.compile(r"\.(png|jpe?g|gif|webp|heic|mp4|mov)$", re.IGNORECASE)
SHOT_DIR = re.compile(r"(^|[-_])(screenshots?|shots)($|[-_])", re.IGNORECASE)


def is_screenshot(path: str) -> bool:
    if not MEDIA.search(path):
        return False
    parts = path.split("/")
    if parts[0] == "docs" and not path.startswith("docs/assets/"):
        return True
    return any(SHOT_DIR.search(part) for part in parts[:-1])


def tracked_files() -> list[str]:
    out = subprocess.run(["git", "ls-files", "-z"], cwd=ROOT, check=True, capture_output=True).stdout
    return [p for p in out.decode().split("\0") if p and (ROOT / p).exists()]


def baseline() -> set[str]:
    lines = BASELINE.read_text().splitlines()
    return {line.strip() for line in lines if line.strip() and not line.startswith("#")}


class NoCommittedScreenshotsTest(unittest.TestCase):
    def test_no_new_screenshots(self):
        allowed = baseline()
        new = sorted(p for p in tracked_files() if is_screenshot(p) and p not in allowed)
        self.assertEqual(new, [], "screenshots go in the PR, not the repository (CLAUDE.md, \"Workflow\"); remove:\n"
                         + "\n".join(new))

    def test_baseline_only_lists_existing_files(self):
        tracked = set(tracked_files())
        stale = sorted(p for p in baseline() if p not in tracked)
        self.assertEqual(stale, [], f"delete these lines from {BASELINE.relative_to(ROOT)}:\n" + "\n".join(stale))

    def test_classification(self):
        self.assertTrue(is_screenshot("docs/web/screenshots/x/a.png"))
        self.assertTrue(is_screenshot("docs/design/some-review/a.jpg"))
        self.assertTrue(is_screenshot("android/feature/settings/screenshots/a/b.png"))
        self.assertTrue(is_screenshot("web/e2e/shots/a.webp"))
        self.assertFalse(is_screenshot("docs/assets/readme/hero-en.png"))
        self.assertFalse(is_screenshot("apple/Persona/Resources/icon.png"))
        self.assertFalse(is_screenshot("web/public/logo.png"))
        self.assertFalse(is_screenshot("docs/web/screenshots/x/README.md"))


if __name__ == "__main__":
    unittest.main()
