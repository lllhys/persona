"""The bilingual README: reciprocal switches, the same shape in both languages, and reproducible assets."""
from pathlib import Path
import importlib.util
import re
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / "scripts/assets/make_readme_assets.py"
SPEC = importlib.util.spec_from_file_location("readme_assets", SCRIPT)
ASSETS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ASSETS)
SVG = "{http://www.w3.org/2000/svg}"
README_ASSETS = ROOT / "docs/assets/readme"
READMES = {"en": ROOT / "README.md", "zh-CN": ROOT / "README.zh-CN.md"}
FEATURES = 8


def read(lang):
    return READMES[lang].read_text(encoding="utf-8")


def section(text, index):
    """The body of the index-th "## " section."""
    return re.split(r"^## .*$", text, flags=re.M)[index + 1]


class ReadmeLanguageTests(unittest.TestCase):
    def test_readmes_switch_languages_at_the_top(self):
        en, zh = read("en"), read("zh-CN")
        self.assertIn("[简体中文](README.zh-CN.md)", "\n".join(en.splitlines()[:12]))
        self.assertIn("[English](README.md)", "\n".join(zh.splitlines()[:12]))

    def test_both_languages_have_the_same_shape(self):
        en, zh = read("en"), read("zh-CN")
        heads = lambda s: re.findall(r"^(#{1,6}) ", s, re.M)  # noqa: E731
        self.assertEqual(heads(en), heads(zh))
        for lang in READMES:
            with self.subTest(lang=lang):
                video, features = section(read(lang), 0), section(read(lang), 1)
                self.assertRegex(video, r"(?m)^https://github\.com/user-attachments/assets/\S+$", "the video tour comes first")
                features = re.findall(r"^- \*\*", features, re.M)
                self.assertEqual(FEATURES, len(features), "the section after the video tour lists the eight features")

    def test_each_language_uses_its_own_pictures(self):
        for lang, suffix in ASSETS.LANGS.items():
            text = read(lang)
            pictures = re.findall(r'(?:src|srcset)="docs/assets/readme/((?:banner|hero)-[^"]+)"', text)
            with self.subTest(lang=lang):
                self.assertEqual(4, len(pictures))
                for name in pictures:
                    self.assertTrue(name.endswith(f"{suffix}.svg") or name.endswith(f"{suffix}.webp"), name)
                    if not suffix:
                        self.assertNotIn(".zh-CN.", name)

    def test_badges_form_one_row_per_kind(self):
        kinds = list(dict.fromkeys(kind for _, kind, _ in ASSETS.BADGES))  # client, agent, engine
        for lang in READMES:
            lines = [ln for ln in read(lang).splitlines() if "docs/assets/readme/badge-" in ln]
            with self.subTest(lang=lang):
                self.assertEqual(len(kinds), len(lines), "one source line per row: clients, agents, engines")
                for kind, line in zip(kinds, lines):
                    found = re.findall(r"\[!\[([a-z]+): [^\]]+\]\(docs/assets/readme/badge-([a-z-]+)\.svg\)", line)
                    self.assertEqual([(kind, name) for name, k, _ in ASSETS.BADGES if k == kind], found)
                for line in lines[:-1]:
                    self.assertTrue(line.endswith("<br>"), "end each row but the last with <br>")

    def test_badges_link_to_maintained_docs_with_matching_labels(self):
        expected = [(name, f"{kind}: {product}") for name, kind, product in ASSETS.BADGES]
        for lang in READMES:
            badges = re.findall(
                r"\[!\[([^\]]+)\]\(docs/assets/readme/badge-([a-z-]+)\.svg\)\]\(([^)]+)\)",
                read(lang),
            )
            with self.subTest(lang=lang):
                self.assertEqual(expected, [(name, label) for label, name, _ in badges])
                for _, name, target in badges:
                    path = target.split("#", 1)[0]
                    self.assertTrue(path.startswith("docs/"), name)
                    self.assertNotRegex(path, r"^docs/(plans|archive)/", name)
                    self.assertTrue((ROOT / path).is_file(), target)

    def test_badges_keep_the_existing_pill_style(self):
        for name, kind, product in ASSETS.BADGES:
            with self.subTest(badge=name):
                root = ET.fromstring(ASSETS.badge(kind, product))
                self.assertEqual("24", root.attrib["height"])
                self.assertEqual(f"{kind}: {product}", root.attrib["aria-label"])
                pill = root.find(SVG + "rect")
                self.assertEqual("6", pill.attrib["rx"])
                self.assertEqual("#26211e", pill.attrib["fill"])
                self.assertEqual("#4a413a", pill.attrib["stroke"])
                labels = root.findall(f"{SVG}g/{SVG}text")
                self.assertEqual(["#b3a79b", "#ffffff"], [node.attrib["fill"] for node in labels])

    def test_superagent_is_spelled_superagent(self):
        for lang in READMES:
            with self.subTest(lang=lang):
                spellings = set(re.findall(r"(?i)super[ -]?agent", read(lang)))
                self.assertEqual({"SuperAgent"}, spellings)
        for lang in ASSETS.LANGS:
            self.assertIn("SuperAgent", ASSETS.TAGLINE[lang][0])

    def test_local_links_and_pictures_exist(self):
        for lang in READMES:
            text = read(lang)
            targets = re.findall(r"\]\(([^)#]+)(?:#[^)]*)?\)", text) + re.findall(r'(?:src|srcset)="([^"]+)"', text)
            for target in targets:
                if re.match(r"https?://", target):
                    continue
                with self.subTest(lang=lang, target=target):
                    self.assertTrue((ROOT / target).exists(), target)

    def test_every_readme_asset_is_used(self):
        used = read("en") + read("zh-CN")
        for path in README_ASSETS.iterdir():
            if path.is_file():
                with self.subTest(asset=path.name):
                    self.assertIn(f"docs/assets/readme/{path.name}", used, "delete assets the README no longer shows")

    def test_generated_svgs_match_the_committed_files(self):
        for lang, suffix in ASSETS.LANGS.items():
            for theme in ASSETS.THEMES:
                with self.subTest(lang=lang, theme=theme):
                    actual = ASSETS.banner(theme, lang)
                    committed = README_ASSETS / f"banner-{theme}{suffix}.svg"
                    self.assertEqual(actual, committed.read_text(encoding="utf-8"), "regenerate the README SVG assets")
                    root = ET.fromstring(actual)
                    self.assertEqual(lang, root.attrib["lang"])
                    text = " ".join("".join(node.itertext()) for tag in ("title", "text")
                                    for node in root.iter(SVG + tag))
                    if lang == "en":
                        self.assertNotRegex(text, r"[一-鿿]")
                    else:
                        self.assertRegex(text, r"[一-鿿]")
        for name, kind, product in ASSETS.BADGES:
            with self.subTest(badge=name):
                committed = README_ASSETS / f"badge-{name}.svg"
                self.assertEqual(ASSETS.badge(kind, product), committed.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
