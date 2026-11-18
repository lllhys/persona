"""Hard-coded pixel font sizes in web/src can stay or fall, never rise.

The cases build throwaway trees; one case checks this checkout's committed baseline.
"""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
import web_font_ratchet as ratchet

ROOT = Path(__file__).resolve().parents[2]


def tree(files, baseline=None):
    root = Path(tempfile.mkdtemp(prefix="font-ratchet-"))
    for rel, text in files.items():
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
    if baseline is not None:
        path = root / ratchet.BASELINE_REL
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(baseline), encoding="utf-8")
    return root


class CountTests(unittest.TestCase):
    def test_arbitrary_sizes_count_and_named_ones_do_not(self):
        text = '<p className="text-[13px] sm:text-[12.5px] text-xs text-sm text-[length:11px] text-[var(--x)] text-ink">'
        self.assertEqual(ratchet.count_sizes(text), 3)

    def test_inline_and_css_sizes_count(self):
        self.assertEqual(ratchet.count_sizes("style={{ fontSize: 13 }} {{ fontSize: '12px' }} {{ fontSize: size }}"), 2)
        self.assertEqual(ratchet.count_sizes("a { font-size: 13px; line-height: 14px } b { max-font-size: 9px }"), 1)

    def test_tests_and_other_files_are_not_scanned(self):
        root = tree({
            "web/src/a.tsx": "text-[13px]",
            "web/src/a.test.tsx": "text-[13px] text-[13px]",
            "web/src/b.json": "text-[13px]",
            "web/other/c.tsx": "text-[13px]",
        })
        self.assertEqual(ratchet.scan(root), {"web/src/a.tsx": 1})


class CheckTests(unittest.TestCase):
    def test_at_the_baseline_passes(self):
        root = tree({"web/src/a.tsx": "text-[13px] text-[12px]"}, {"web/src/a.tsx": 2})
        self.assertEqual(ratchet.check(root), [])

    def test_a_rise_fails(self):
        root = tree({"web/src/a.tsx": "text-[13px] text-[12px] text-[11px]"}, {"web/src/a.tsx": 2})
        self.assertEqual(len(ratchet.check(root)), 1)
        self.assertIn("baseline 2", ratchet.check(root)[0])

    def test_a_new_file_fails(self):
        root = tree({"web/src/a.tsx": "text-[13px]", "web/src/new.tsx": "text-[13px]"}, {"web/src/a.tsx": 1})
        found = ratchet.check(root)
        self.assertEqual(len(found), 1)
        self.assertIn("new.tsx", found[0])

    def test_a_fall_asks_for_an_update(self):
        root = tree({"web/src/a.tsx": "text-[13px]"}, {"web/src/a.tsx": 2})
        self.assertIn("--update", ratchet.check(root)[0])

    def test_a_missing_baseline_is_not_invented(self):
        self.assertIn("baseline missing", ratchet.check(tree({"web/src/a.tsx": "text-[13px]"}))[0])


class UpdateTests(unittest.TestCase):
    def test_update_lowers_and_drops_files_without_sizes(self):
        root = tree({"web/src/a.tsx": "text-[13px]", "web/src/b.tsx": "text-xs"}, {"web/src/a.tsx": 3, "web/src/b.tsx": 1})
        self.assertEqual(ratchet.update(root), [])
        self.assertEqual(json.loads((root / ratchet.BASELINE_REL).read_text()), {"web/src/a.tsx": 1})

    def test_update_refuses_to_raise_or_add(self):
        root = tree({"web/src/a.tsx": "text-[13px] text-[12px]", "web/src/n.tsx": "text-[13px]"}, {"web/src/a.tsx": 1})
        refusals = ratchet.update(root)
        self.assertEqual(len(refusals), 2)
        self.assertEqual(json.loads((root / ratchet.BASELINE_REL).read_text()), {"web/src/a.tsx": 1})

    def test_update_lets_a_split_file_take_what_the_old_one_lost(self):
        root = tree({"web/src/a.tsx": "text-[13px]", "web/src/n.tsx": "text-[13px] text-[12px]"}, {"web/src/a.tsx": 3})
        self.assertEqual(ratchet.update(root), [])
        self.assertEqual(json.loads((root / ratchet.BASELINE_REL).read_text()), {"web/src/a.tsx": 1, "web/src/n.tsx": 2})

    def test_update_refuses_a_new_file_that_takes_more_than_was_lost(self):
        root = tree({"web/src/a.tsx": "text-[13px]", "web/src/n.tsx": "text-[13px] text-[12px] text-[11px]"}, {"web/src/a.tsx": 3})
        self.assertEqual(len(ratchet.update(root)), 1)
        self.assertEqual(json.loads((root / ratchet.BASELINE_REL).read_text()), {"web/src/a.tsx": 3})

    def test_update_does_not_create_a_baseline(self):
        with self.assertRaises(ratchet.FontError):
            ratchet.update(tree({"web/src/a.tsx": "text-[13px]"}))

    def test_a_broken_baseline_is_an_error(self):
        root = tree({"web/src/a.tsx": ""})
        (root / ratchet.BASELINE_REL).parent.mkdir(parents=True, exist_ok=True)
        (root / ratchet.BASELINE_REL).write_text("[1]")
        with self.assertRaises(ratchet.FontError):
            ratchet.check(root)


class CheckoutTests(unittest.TestCase):
    def test_this_checkout_is_at_its_baseline(self):
        self.assertEqual(ratchet.check(ROOT), [])


if __name__ == "__main__":
    unittest.main()
