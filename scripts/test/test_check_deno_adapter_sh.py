"""pre-push refuses a push whose trusted Deno adapter is not what its sdk/ts/src builds (check-deno-adapter.sh, #6001).

Each test pushes from a temporary repository holding the real hooks, privacy_check.py and scripts/gen/deno_adapter.sh,
with a fake bun on PATH (or none) that "bundles" sdk/ts/src/index.ts, so no real Bun is needed.
"""

import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

SCRIPTS = Path(__file__).resolve().parents[1]
ADAPTER = "server/internal/extension/denohost/adapter.mjs"
FAKE_BUN = """#!/bin/bash
# A stand-in for bun: its version, and a "bundle" of sdk/ts/src/index.ts with the flags it was given.
if [ "$1" = --version ]; then echo "${FAKE_BUN_VERSION:-9.9.9}"; exit 0; fi
[ "$1" = build ] || exit 2
entry="$2"; out=""; flags=""
shift 2
while [ $# -gt 0 ]; do
  case "$1" in --outfile) out="$2"; shift ;; *) flags="$flags $1" ;; esac
  shift
done
root="$(cd "$(dirname "$entry")/../../../.." && pwd)"
{ printf 'bundle%s:' "$flags"; cat "$root/sdk/ts/src/index.ts"; } >"$out"
"""


class DenoAdapterPrePushTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="deno-adapter-hook-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.home = self.root / "home"
        self.tools = self.root / "tools"  # python3 only: the hooks run it, and no bun may come with it
        self.fake = self.root / "fake-bun"
        for d in (self.repo, self.home, self.tools, self.fake):
            d.mkdir()
        (self.tools / "python3").symlink_to(sys.executable)
        (self.fake / "bun").write_text(FAKE_BUN)
        (self.fake / "bun").chmod(0o755)
        self.env = {k: v for k, v in os.environ.items() if not k.startswith("GIT_") and k not in {"PYTHONPATH", "PYTHONHOME"}}
        self.env.update({"HOME": str(self.home), "XDG_CONFIG_HOME": str(self.home / ".config"),
                         "GIT_CONFIG_NOSYSTEM": "1", "GIT_CONFIG_GLOBAL": os.devnull,
                         "GIT_AUTHOR_NAME": "Test User", "GIT_AUTHOR_EMAIL": "test@example.com",
                         "GIT_COMMITTER_NAME": "Test User", "GIT_COMMITTER_EMAIL": "test@example.com"})
        self.without_bun = dict(self.env, PATH=os.pathsep.join([str(self.tools), "/usr/bin", "/bin"]))
        if shutil.which("bun", path=self.without_bun["PATH"]):
            self.skipTest("a bun in /usr/bin or /bin: cannot test a push without bun")
        self.with_bun = dict(self.without_bun, PATH=os.pathsep.join([str(self.fake), self.without_bun["PATH"]]))
        self.git("init", "-q")
        self.git("config", "core.hooksPath", "scripts/hooks")
        self.git("config", "commit.gpgSign", "false")
        (self.repo / "scripts/gen").mkdir(parents=True)
        shutil.copy2(SCRIPTS / "privacy_check.py", self.repo / "scripts/privacy_check.py")
        shutil.copytree(SCRIPTS / "hooks", self.repo / "scripts/hooks")
        shutil.copy2(SCRIPTS / "gen/deno_adapter.sh", self.repo / "scripts/gen/deno_adapter.sh")
        self.write("config.example.json", "{}\n")
        self.write(".github/workflows/server-test.yml", "      - uses: oven-sh/setup-bun@v2\n        with:\n          bun-version: '9.9.9'\n")
        self.write("server/internal/extension/denohost/adapter.ts", "import { connect } from '../../../../sdk/ts/src/index.ts'\n")
        self.write("sdk/ts/src/index.ts", "export const connect = 1\n")
        self.regenerate()
        self.git("add", "-A")
        self.git("commit", "-q", "--no-verify", "-m", "base")
        self.git("branch", "-M", "main")
        self.git("update-ref", "refs/remotes/origin/main", "HEAD")
        self.git("checkout", "-q", "-b", "topic")
        self.remote = self.root / "remote.git"
        self.run_command("git", "init", "-q", "--bare", str(self.remote))

    def run_command(self, *args, env=None):
        return subprocess.run(args, cwd=self.repo, env=env or self.env, capture_output=True, text=True, timeout=60)

    def git(self, *args):
        result = self.run_command("git", *args)
        if result.returncode:
            self.fail(result.stdout + result.stderr)
        return result

    def write(self, name, text):
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, name, text):
        self.write(name, text)
        self.git("add", name)
        self.git("commit", "-q", "--no-verify", "-m", "change " + name)

    def regenerate(self):
        result = self.run_command("bash", "scripts/gen/deno_adapter.sh", env=self.with_bun)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def push(self, env):
        return self.run_command("git", "push", "-q", str(self.remote), "topic", env=env)

    def assert_pushed(self, result):
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def assert_refused(self, result, *words):
        self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn("pre-push: refused refs/heads/topic;", result.stderr)
        for word in words:
            self.assertIn(word, result.stderr)

    def test_a_push_that_leaves_the_adapter_alone_needs_no_bun(self):
        self.commit("README.md", "notes\n")
        self.assert_pushed(self.push(self.without_bun))

    def test_without_bun_an_sdk_change_is_refused_with_the_pinned_version(self):
        self.commit("sdk/ts/src/index.ts", "export const connect = 2\n")
        self.assert_refused(self.push(self.without_bun), "there is no bun", "Install Bun 9.9.9",
                            "npm install --prefix ~/.bun-9.9.9 bun@9.9.9", "scripts/gen/deno_adapter.sh")

    def test_an_sdk_change_without_a_regenerated_adapter_is_refused(self):
        self.commit("sdk/ts/src/index.ts", "export const connect = 2\n")
        self.assert_refused(self.push(self.with_bun), ADAPTER + " is stale", "Bun 9.9.9")

    def test_a_regenerated_adapter_is_pushed(self):
        self.write("sdk/ts/src/index.ts", "export const connect = 2\n")
        self.regenerate()
        self.git("add", "sdk", "server")
        self.git("commit", "-q", "--no-verify", "-m", "sdk change with its adapter")
        self.assert_pushed(self.push(self.with_bun))
        self.assertIn("--minify-syntax --minify-whitespace", (self.repo / ADAPTER).read_text())  # the generator's flags

    def test_the_pushed_commit_is_checked_not_the_worktree(self):
        self.commit("sdk/ts/src/index.ts", "export const connect = 2\n")
        self.regenerate()  # regenerated but not committed
        self.assert_refused(self.push(self.with_bun), "is stale")

    def test_an_adapter_edited_by_hand_is_refused(self):
        self.commit(ADAPTER, "tampered\n")
        self.assert_refused(self.push(self.with_bun), "is stale")

    def test_another_bun_version_whose_build_differs_asks_for_the_pinned_one(self):
        self.commit("sdk/ts/src/index.ts", "export const connect = 2\n")
        result = self.push(dict(self.with_bun, FAKE_BUN_VERSION="9.8.0"))
        self.assert_refused(result, "bun is 9.8.0, not the pinned 9.9.9", "npm install --prefix ~/.bun-9.9.9 bun@9.9.9")
        self.assertNotIn("is stale: it is not", result.stderr)

    def pinned_bun(self, where):
        """A fake bun of the pinned version where the install hint (node_modules/.bin) or a release zip (bin) puts it."""
        target = self.home / ".bun-9.9.9" / where
        target.mkdir(parents=True)
        (target / "bun").write_text('#!/bin/bash\nFAKE_BUN_VERSION=9.9.9 exec "%s" "$@"\n' % (self.fake / "bun"))
        (target / "bun").chmod(0o755)

    def test_the_pinned_bun_is_found_without_being_on_path(self):
        for where in ("node_modules/.bin", "bin"):
            with self.subTest(where=where):
                shutil.rmtree(self.home / ".bun-9.9.9", ignore_errors=True)
                self.pinned_bun(where)
                self.commit("sdk/ts/src/index.ts", "export const connect = '%s'\n" % where)
                self.assert_refused(self.push(self.without_bun), "is stale", "Bun 9.9.9")
                self.regenerate()
                self.git("add", "server")
                self.git("commit", "-q", "--no-verify", "-m", "regenerated")
                # Another version first on PATH: the pinned one is used all the same.
                self.assert_pushed(self.push(dict(self.with_bun, FAKE_BUN_VERSION="9.8.0")))

    def test_an_orphan_branch_has_no_adapter_to_compare(self):
        self.git("checkout", "-q", "--orphan", "shots")
        self.git("rm", "-r", "-q", "--cached", ".")
        for path in self.repo.iterdir():
            if path.name not in {".git", "scripts", "config.example.json"}:
                shutil.rmtree(path) if path.is_dir() else path.unlink()
        self.git("add", "scripts", "config.example.json")
        self.commit("shot.txt", "an image\n")
        result = self.run_command("git", "push", "-q", str(self.remote), "shots", env=self.without_bun)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
