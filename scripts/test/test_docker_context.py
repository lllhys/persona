"""What the hub image's build reads from outside its stage's own folder reaches it: the Dockerfile copies it into that
stage and .dockerignore lets it into the context (#9626). Every integrations/<dir> that server/internal/runtimepkg embeds
(its Packages and Assets, read by `go generate` in the build stage), and every file the web app imports from outside
web/ (read by tsc -b and vite in the web stage). A missing line broke only the image build, after the change merged:
the runtimepkg package added for the Claude Code plugin, the desktop bridge's types, and four times before (#5197)."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
RUNTIMEPKG = ROOT / "server/internal/runtimepkg/runtimepkg.go"
WEB = ROOT / "web"
# import x from "…", import "…", import("…"), export … from "…": the specifier up to a query (?raw)
IMPORT = re.compile(r"""(?:\bfrom\s+|\bimport\s*\(\s*|\bimport\s+)["'](\.\.?/[^"'?]+)""")


def map_block(source, name):
    """The body of `var <name> = map[string][]string{ ... }`."""
    match = re.search(r"^var " + name + r" = map\[string\]\[\]string\{\n(.*?)^\}", source, re.M | re.S)
    if not match:
        raise AssertionError("runtimepkg.go has no " + name + " map")
    return match.group(1)


def runtimepkg_paths():
    """integrations/<dir>/ for every top-level directory runtimepkg's Packages and Assets patterns read."""
    source = RUNTIMEPKG.read_text()
    dirs = set()
    for name in ("Packages", "Assets"):
        body = re.sub(r"//[^\n]*", "", map_block(source, name))
        for pattern in re.findall(r'"([^"]+)"', body):
            if "/" in pattern:  # a pattern is <dir>/<path>; the map's keys (package names) have no slash
                dirs.add("integrations/" + pattern.split("/", 1)[0] + "/")
    return dirs


def web_outside_paths():
    """Repo-relative paths the web app's sources import from outside web/."""
    paths = set()
    for source in (WEB / "src").rglob("*"):
        if source.suffix not in (".ts", ".tsx", ".mts") or "node_modules" in source.parts:
            continue
        for specifier in IMPORT.findall(source.read_text(errors="replace")):
            target = (source.parent / specifier).resolve()
            if not target.is_relative_to(WEB):
                paths.add(target.relative_to(ROOT).as_posix())
    return paths


def stage_copies(name):
    """The repo paths `COPY <path> /src/<path>` brings into the Dockerfile stage `name`."""
    stages = re.split(r"(?m)^FROM ", (ROOT / "Dockerfile").read_text())
    stage = next((s for s in stages if re.match(r"\S+(?: \S+)* AS " + name + r"\n", s)), None)
    if stage is None:
        raise AssertionError("the Dockerfile has no stage " + name)
    return re.findall(r"(?m)^COPY (\S+) /src/(\S+)$", stage)


def covered(path, prefixes):
    """Whether one of `prefixes` (a folder ending in / or a file) holds `path`."""
    return any(path == p or path.startswith(p if p.endswith("/") else p + "/") for p in prefixes)


class DockerContextTests(unittest.TestCase):
    def setUp(self):
        self.let_in = [line[1:] for line in (ROOT / ".dockerignore").read_text().splitlines() if line.startswith("!")]

    def assert_reaches(self, paths, stage):
        copies = stage_copies(stage)
        for source, destination in copies:
            self.assertEqual(source, destination, "COPY keeps the repo path under /src")
        for path in sorted(paths):
            with self.subTest(path=path, stage=stage):
                self.assertTrue((ROOT / path).exists(), path)
                self.assertTrue(covered(path, [s for s, _ in copies]), path + " is not copied into the " + stage + " stage")
                self.assertTrue(covered(path, self.let_in), path + " is not let in by .dockerignore")

    def test_finds_what_it_checks(self):
        self.assertIn("integrations/runtime-sdk/", runtimepkg_paths())
        self.assertIn("integrations/claude-code-plugin/", runtimepkg_paths())
        self.assertIn("spec/desktop-bridge.d.ts", web_outside_paths())

    def test_runtimepkg_sources_reach_the_build_stage(self):
        self.assert_reaches(runtimepkg_paths(), "build")

    def test_web_imports_from_outside_web_reach_the_web_stage(self):
        self.assert_reaches(web_outside_paths(), "web")


if __name__ == "__main__":
    unittest.main()
