"""The release APK must carry the commit it was built from: aapt2's resource dump is read back and compared."""
import importlib.util
from pathlib import Path
import stat
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
import io

SPEC = importlib.util.spec_from_file_location("check_android_build_commit", Path(__file__).with_name("check_android_build_commit.py"))
check = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(check)

SHA = "0123456789abcdef0123456789abcdef01234567"
OTHER = "fedcba9876543210fedcba9876543210fedcba98"

# The shape of `aapt2 dump resources <apk>`: one `resource <id> <type>/<name>` line, then a `(<config>) <value>` line per configuration.
DUMP = """Package name=com.example.app id=7f
  type string id=0f entryCount=3
    resource 0x7f0f0000 string/app_name
      () "Persona"
      (zh) "Persona"
    resource 0x7f0f0001 string/persona_build_commit
{commit}
    resource 0x7f0f0002 string/other
      () "%s"
""" % OTHER


def dump(*values):
    return DUMP.format(commit="\n".join(f'      ({config}) "{value}"' for config, value in values))


class CommitValuesTest(unittest.TestCase):
    def test_injected_commit_passes(self):
        self.assertIsNone(check.problem(dump(("", SHA)), SHA))

    def test_a_build_given_no_commit_says_unknown_and_fails(self):
        self.assertIn("unknown", check.problem(dump(("", "unknown")), SHA))

    def test_another_commit_fails(self):
        self.assertIn(OTHER, check.problem(dump(("", OTHER)), SHA))

    def test_a_shorter_or_longer_value_fails(self):
        for value in (SHA[:12], SHA + "0", SHA.upper()):
            with self.subTest(value=value):
                self.assertIsNotNone(check.problem(dump(("", value)), SHA))

    def test_a_missing_resource_fails_even_when_other_strings_hold_the_commit(self):
        without = DUMP.split("    resource 0x7f0f0001", 1)[0] + '    resource 0x7f0f0002 string/other\n      () "%s"\n' % SHA
        self.assertIn("not in", check.problem(without, SHA))

    def test_every_configuration_must_agree(self):
        self.assertIsNotNone(check.problem(dump(("", SHA), ("v21", OTHER)), SHA))
        self.assertIsNone(check.problem(dump(("", SHA), ("v21", SHA)), SHA))

    def test_a_value_without_a_default_configuration_fails(self):
        self.assertIn("default", check.problem(dump(("v21", SHA)), SHA))

    def test_a_reference_is_not_a_commit(self):
        reference = DUMP.format(commit="      () (reference) @string/other")
        self.assertIsNotNone(check.problem(reference, SHA))

    def test_a_public_marker_on_the_header_is_read(self):
        marked = dump(("", SHA)).replace("string/persona_build_commit", "string/persona_build_commit PUBLIC")
        self.assertIsNone(check.problem(marked, SHA))
        self.assertEqual(check.commit_values(marked), [("", SHA)])


class MainTest(unittest.TestCase):
    """The script runs the aapt2 it is pointed at, so a fake one stands in for the APK's resources."""

    def run_main(self, tools_output, sha=SHA):
        with tempfile.TemporaryDirectory() as tmp:
            tools = Path(tmp)
            (tools / "dump.txt").write_text(tools_output)
            aapt2 = tools / "aapt2"
            aapt2.write_text(f"#!{sys.executable}\nimport sys\n"
                             "assert sys.argv[1:3] == ['dump', 'resources'], sys.argv\n"
                             f"print(open({str(tools / 'dump.txt')!r}).read(), end='')\n")
            aapt2.chmod(aapt2.stat().st_mode | stat.S_IXUSR)
            out, err = io.StringIO(), io.StringIO()
            with redirect_stdout(out), redirect_stderr(err):
                code = check.main([str(tools / "app-release-unsigned.apk"), sha, "--build-tools", str(tools)])
            return code, out.getvalue(), err.getvalue()

    def test_pass_and_fail_exit_codes(self):
        self.assertEqual(self.run_main(dump(("", SHA)))[0], 0)
        code, out, err = self.run_main(dump(("", "unknown")))
        self.assertEqual(code, 1)
        self.assertIn("FAIL", out)
        self.assertIn(SHA, err)

    def test_an_apk_with_no_resource_at_all_fails(self):
        self.assertEqual(self.run_main("Package name=com.example.app id=7f\n")[0], 1)

    def test_the_expected_commit_must_be_a_full_lowercase_sha(self):
        for sha in ("unknown", SHA[:12], SHA.upper(), ""):
            with self.subTest(sha=sha):
                self.assertEqual(self.run_main(dump(("", SHA)), sha)[0], 1)


if __name__ == "__main__":
    unittest.main()
