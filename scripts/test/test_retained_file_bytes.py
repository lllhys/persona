"""Retained third-party files survive Git text conversion without changing their bytes."""

import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
RETAINED = {
    "harmony/native/ssh/licenses/LICENSE": b"first\nsecond\n",
    "harmony/native/ssh/licenses/nested/LICENSE": b"first\r\nsecond\r\n",
    "harmony/native/ssh/licenses/nested/mixed.txt": b"first\r\nsecond\n",
    "native/app-core/licenses/LICENSE": b"first\nsecond\n",
    "native/app-core/licenses/nested/LICENSE": b"first\r\nsecond\r\n",
    "native/app-core/licenses/nested/mixed.txt": b"first\r\nsecond\n",
    "harmony/entry/src/main/resources/rawfile/native-ssh-licenses.html": b"<p>first</p>\n<p>second</p>\n",
}
ORDINARY = (
    "harmony/native/ssh/source.txt",
    "native/app-core/source.txt",
    "harmony/entry/src/main/resources/rawfile/other.html",
)


class RetainedFileBytesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="retained-file-bytes-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.home = self.root / "home"
        self.repo.mkdir()
        self.home.mkdir()
        self.env = {key: value for key, value in os.environ.items() if not key.startswith("GIT_")}
        self.env.update(HOME=str(self.home), USERPROFILE=str(self.home),
                        XDG_CONFIG_HOME=str(self.home / ".config"), GIT_CONFIG_NOSYSTEM="1",
                        GIT_CONFIG_GLOBAL=os.devnull, GIT_ATTR_NOSYSTEM="1",
                        GIT_AUTHOR_NAME="Test User", GIT_AUTHOR_EMAIL="test@example.com",
                        GIT_COMMITTER_NAME="Test User", GIT_COMMITTER_EMAIL="test@example.com")
        self.git("-c", "init.templateDir=", "init", "-q")
        self.git("config", "core.safecrlf", "false")
        (self.repo / ".gitattributes").write_bytes((ROOT / ".gitattributes").read_bytes())

    def git(self, *args):
        result = subprocess.run(["git", *args], cwd=self.repo, env=self.env,
                                capture_output=True, timeout=30)
        if result.returncode:
            self.fail(result.stderr.decode("utf-8", errors="replace"))
        return result.stdout

    def commit_files(self, autocrlf, ordinary):
        self.git("config", "core.autocrlf", autocrlf)
        for name, data in {**RETAINED, **dict.fromkeys(ORDINARY, ordinary)}.items():
            target = self.repo / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        self.git("add", ".")
        self.git("commit", "-qm", "Retained file fixtures")

    def test_staging_preserves_upstream_bytes_with_autocrlf(self):
        self.commit_files("true", b"ordinary\r\ntext\r\n")
        for name, expected in RETAINED.items():
            with self.subTest(path=name):
                self.assertEqual(self.git("show", "HEAD:" + name), expected)
        for name in ORDINARY:
            with self.subTest(ordinary=name):
                self.assertEqual(self.git("show", "HEAD:" + name), b"ordinary\ntext\n")

    def test_checkout_preserves_committed_bytes_with_autocrlf(self):
        self.commit_files("false", b"ordinary\ntext\n")
        self.git("config", "core.autocrlf", "true")
        for name in (*RETAINED, *ORDINARY):
            (self.repo / name).unlink()
        self.git("checkout-index", "--all", "--force")
        for name, expected in RETAINED.items():
            with self.subTest(path=name):
                self.assertEqual((self.repo / name).read_bytes(), expected)
        for name in ORDINARY:
            with self.subTest(ordinary=name):
                self.assertEqual((self.repo / name).read_bytes(), b"ordinary\r\ntext\r\n")

    def test_archive_preserves_committed_bytes_with_autocrlf(self):
        self.commit_files("false", b"ordinary\ntext\n")
        self.git("config", "core.autocrlf", "true")
        with tarfile.open(fileobj=io.BytesIO(self.git("archive", "--format=tar", "HEAD"))) as archive:
            for name, expected in RETAINED.items():
                with self.subTest(path=name):
                    self.assertEqual(archive.extractfile(name).read(), expected)
            for name in ORDINARY:
                with self.subTest(ordinary=name):
                    self.assertEqual(archive.extractfile(name).read(), b"ordinary\r\ntext\r\n")


if __name__ == "__main__":
    unittest.main()
