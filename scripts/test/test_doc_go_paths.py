"""Go files the current docs name by their package (`hub/deploy.go`) must exist under server/internal.

test_doc_paths.py checks paths that start with a top-level directory or with internal/. Docs often name a Go file
relative to server/internal instead, by its package (`devices/deploy.go`, `assistant/todos.go`); those were not checked,
and went stale when files moved between packages (#2448 moved hub/deploy.go, claudedirs.go, link.go and tailcat.go to
devices/). This checks every `backticked` <pkg>/<file>.go whose first part is a directory under server/internal, in the
same docs test_doc_paths.py checks and with the same skips (globs, placeholders, lines quoting another project).

A file that is not in this repository any more but is named on purpose (one that moved to an extension, a record of
an old design) is written without backticks, so it reads as a name and not as a path to open.
"""
from pathlib import Path
import re
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
from test_doc_paths import QUOTED, doc_files  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]


def packages():
    """The package directories under server/internal, the first part of a package-relative Go path."""
    return {p.name for p in (ROOT / "server/internal").iterdir() if p.is_dir()}


def package_go_paths(line, names):
    """Yield each backticked <pkg>/<file>.go in line whose <pkg> is one of names."""
    for token in QUOTED.findall(line):
        # A symbol may follow a colon (`hub/hub.go:120`, `devices/link.go` + full-width colon + `send`), as in test_doc_paths.
        path = re.split(r"[:：]", token.split("#", 1)[0], maxsplit=1)[0].rstrip(".,;/")
        if not path.endswith(".go") or "/" not in path or path.split("/", 1)[0] not in names:
            continue
        if any(mark in path for mark in ("<", ">", "*", "{", "$", "…", "...")):
            continue
        yield path


def missing_package_go_paths(doc, names):
    """Return doc:line: path for every package-relative Go path in doc that is not under server/internal."""
    found = []
    for number, line in enumerate(doc.read_text(errors="ignore").splitlines(), 1):
        if "github.com/" in line:  # a path inside another project, quoted with its source
            continue
        found += [f"{doc.relative_to(ROOT).as_posix()}:{number}: {path}" for path in package_go_paths(line, names)
                  if not (ROOT / "server/internal" / path).is_file()]
    return found


class DocGoPathTests(unittest.TestCase):
    def test_package_relative_go_paths_are_recognised(self):
        names = {"hub", "devices", "connector"}
        line = ("`devices/deploy.go` 的 `posixCommand`，`hub/hub.go:120`，`devices/link.go：send`，"
                "`connector/imessage/remote.go`，`hub/lark*.go`，`node/x.go`，`internal/hub/hub.go`，`hub/README.md`")
        self.assertEqual(["devices/deploy.go", "hub/hub.go", "devices/link.go", "connector/imessage/remote.go"],
                         list(package_go_paths(line, names)))
        self.assertEqual([], list(package_go_paths("原 hub 的 kb_moments.go、hub/deploy.go", names)))

    def test_moved_files_are_reported(self):
        with tempfile.TemporaryDirectory(prefix="doc-go-paths-") as directory:
            root = Path(directory)
            (root / "server/internal/devices").mkdir(parents=True)
            (root / "server/internal/devices/deploy.go").write_text("package devices\n")
            (root / "server/internal/hub").mkdir()
            doc = root / "docs/operations/hub-update.md"
            doc.parent.mkdir(parents=True)
            doc.write_text("「重新部署」（`hub/deploy.go`）\n`devices/deploy.go`\n"
                           "see `hub/deploy.go` in https://github.com/example/other\n")
            with patch(__name__ + ".ROOT", root):
                self.assertEqual({"devices", "hub"}, packages())
                self.assertEqual(["docs/operations/hub-update.md:1: hub/deploy.go"], missing_package_go_paths(doc, packages()))

    def test_package_relative_go_paths_exist(self):
        names = packages()
        missing = [entry for doc in doc_files() for entry in missing_package_go_paths(doc, names)]
        self.assertEqual([], missing, "docs name Go files that are not under server/internal; update them to where "
                                      "the file is now, or drop the backticks for a file named on purpose")


if __name__ == "__main__":
    unittest.main()
