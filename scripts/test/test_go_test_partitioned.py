"""go_test_partitioned: test-server.sh's go test runs internal/node and internal/assistant in go_partitions'
partitions (#5594, #7644)."""
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import go_partitions
import go_test_partitioned as gtp

MODULE = ["example/server/cmd/personad", "example/server/internal/assistant", "example/server/internal/assistantwire",
          "example/server/internal/node", "example/server/internal/node/sub"]
NODE, ASSISTANT = "./internal/node", "./internal/assistant"
PATTERNS = {NODE: ["^(TestA|TestC)$", "^(TestB)$"], ASSISTANT: ["^(TestX|TestZ)$", "^(TestY)$"]}


def plan(argv):
    return gtp.plan(argv, lambda: list(MODULE), lambda package: list(PATTERNS[package]))


def partitions(flags, package, trailing=()):
    return [["go", "test", *flags, "-run", pattern, package, *trailing] for pattern in PATTERNS[package]]


class PlanTests(unittest.TestCase):
    def test_the_whole_module_runs_the_partitioned_packages_in_partitions_beside_the_rest(self):
        flags = ["-count=1", "-p", "1"]
        self.assertEqual(plan(["go", "test", *flags, "./..."]), [
            ["go", "test", *flags, "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub"],
            *partitions(flags, ASSISTANT), *partitions(flags, NODE),
        ])
        self.assertEqual(list(PATTERNS), list(go_partitions.PACKAGES))  # the fixture covers every partitioned package

    def test_every_command_keeps_the_callers_flags(self):
        # A deploy's skip list (scripts/deploy): every partition leaves the same tests out.
        commands = plan(["go", "test", "-count=1", "-p", "1", "-skip", "TestX|TestY", "-v", "./..."])
        for command in commands:
            self.assertEqual(command[2:8], ["-count=1", "-p", "1", "-skip", "TestX|TestY", "-v"])

    def test_a_partitioned_package_named_alone_runs_only_its_partitions(self):
        for package, named in ((NODE, "./internal/node"), (NODE, "./internal/node/"), (ASSISTANT, "./internal/assistant/")):
            self.assertEqual(plan(["go", "test", "-count=1", named]), partitions(["-count=1"], package))

    def test_partitioned_packages_beside_other_packages(self):
        self.assertEqual(plan(["go", "test", "./internal/hub/", "./internal/node/", "./internal/assistant"]),
                         [["go", "test", "./internal/hub/"], *partitions([], NODE), *partitions([], ASSISTANT)])

    def test_run_or_other_packages_or_another_command_run_as_given(self):
        for argv in (["go", "test", "-run", "TestX", "./..."], ["go", "test", "-run=TestX", "./internal/node/"],
                     ["go", "test", "-run", "TestX", "./internal/assistant"], ["go", "test", "-count=1", "./internal/hub/"],
                     ["go", "test", "./internal/node/sub"], ["go", "test", "./internal/assistantwire"],
                     ["go", "vet", "./..."]):
            self.assertEqual(plan(argv), [argv], argv)

    def test_flag_values_are_not_packages(self):
        flags, packages = gtp.split(["-coverprofile", "/tmp/cover.out", "-skip", "A|B", "-timeout=20m", "./..."])
        self.assertEqual((flags, packages), (["-coverprofile", "/tmp/cover.out", "-skip", "A|B", "-timeout=20m"],
                                             ["./..."]))

    def test_coverprofile_runs_directly_before_package_discovery(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for form in ((alias, "/tmp/c"), (alias + "=/tmp/c",)):
                for package in ("./...", NODE):
                    with self.subTest(form=form, package=package):
                        argv = ["go", "test", *form, package]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_coverprofile_tokens_in_values_and_double_dash_args_are_not_coverage(self):
        for flag in ("-skip", "--skip", "-run", "--run", "-exec", "--exec", "-outputdir", "-test.list"):
            for value in ("-coverprofile", "--test.coverprofile"):
                with self.subTest(flag=flag, value=value):
                    self.assertFalse(gtp.has_coverage_profile([flag, value, "./..."]))
        for args in (["./...", "--", "-coverprofile"], [NODE, "--", "--test.coverprofile=/tmp/c"]):
            with self.subTest(args=args):
                self.assertFalse(gtp.has_coverage_profile(args))

    def test_args_test_coverprofile_is_coverage(self):
        for terminator in ("-args", "--args"):
            for form in (("-test.coverprofile", "/tmp/c"), ("-test.coverprofile=/tmp/c",)):
                with self.subTest(terminator=terminator, form=form):
                    self.assertTrue(gtp.has_coverage_profile([NODE, terminator, *form]))

    def test_skip_and_exec_values_that_look_like_coverprofile_keep_the_partition_plan(self):
        for flag in ("-skip", "--skip", "-exec", "--exec", "-outputdir", "-test.list"):
            for value in ("-coverprofile", "--test.coverprofile"):
                flags = [flag, value]
                with self.subTest(flags=flags):
                    self.assertEqual(plan(["go", "test", *flags, NODE]), partitions(flags, NODE))

    def test_run_value_that_looks_like_coverprofile_still_narrows_without_coverage(self):
        argv = ["go", "test", "-run", "-coverprofile", "./..."]
        self.assertFalse(gtp.has_coverage_profile(argv[2:]))
        self.assertEqual(plan(argv), [argv])

    def test_double_dash_args_stay_after_generated_partition_flags_and_packages(self):
        trailing = ["--", "-coverprofile"]
        self.assertEqual(plan(["go", "test", NODE, *trailing]), partitions([], NODE, trailing))
        self.assertEqual(plan(["go", "test", "./...", *trailing]), [
            ["go", "test", "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub", *trailing],
            *partitions([], ASSISTANT, trailing), *partitions([], NODE, trailing),
        ])

    def test_args_passthrough_runs_directly_before_package_discovery(self):
        trailing_forms = ((),
                          ("-test.coverprofile", "/tmp/c"),
                          ("-test.coverprofile=/tmp/c",),
                          ("-test.run", "TestAlpha"),
                          ("-test.run=TestAlpha",),
                          ("-test.v",))
        for terminator in ("-args", "--args"):
            for trailing in trailing_forms:
                for package in ("./...", NODE):
                    with self.subTest(terminator=terminator, trailing=trailing, package=package):
                        argv = ["go", "test", package, terminator, *trailing]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_empty_coverprofile_values_keep_the_partition_plan(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for flags in ([alias + "="], [alias, ""]):
                with self.subTest(flags=flags):
                    self.assertEqual(plan(["go", "test", *flags, NODE]), partitions(flags, NODE))

    def test_last_coverprofile_value_wins_across_aliases(self):
        for empty, nonempty in (("-coverprofile=", "--test.coverprofile=/tmp/c"),
                                ("--test.coverprofile=", "-coverprofile=/tmp/c")):
            with self.subTest(empty=empty, nonempty=nonempty):
                self.assertEqual(plan(["go", "test", empty, NODE]), partitions([empty], NODE))
                argv = ["go", "test", nonempty, empty, NODE]
                self.assertEqual(plan(argv), partitions([nonempty, empty], NODE))
                argv = ["go", "test", empty, nonempty, NODE]
                self.assertEqual(plan(argv), [argv])

    def test_goflags_coverprofile_runs_directly_before_package_discovery(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for goflags in (alias + "=/tmp/c", "'" + alias + "=/tmp/a b/c.out'",
                            '"' + alias + '=/tmp/a b/c.out"'):
                for package in ("./...", NODE):
                    with self.subTest(goflags=goflags, package=package):
                        argv = ["go", "test", package]
                        list_packages = mock.Mock(return_value=list(MODULE))
                        list_patterns = mock.Mock(side_effect=lambda name: list(PATTERNS[name]))
                        self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags=goflags), [argv])
                        list_packages.assert_not_called()
                        list_patterns.assert_not_called()

    def test_goflags_empty_or_overridden_coverprofile_keeps_the_partition_plan(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for goflags, flags in ((alias + "=", []),
                                   (alias + "=/tmp/from-env", [alias + "="]),
                                   (alias + "=/tmp/from-env", ["-coverprofile", ""])):
                with self.subTest(goflags=goflags, flags=flags):
                    argv = ["go", "test", *flags, NODE]
                    self.assertEqual(gtp.plan(argv, lambda: list(MODULE), lambda package: list(PATTERNS[package]),
                                              goflags=goflags), partitions(flags, NODE))

    def test_goflags_explicit_nonempty_profile_override_runs_directly(self):
        argv = ["go", "test", "-test.coverprofile=/tmp/from-cli", NODE]
        list_packages = mock.Mock(return_value=list(MODULE))
        list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
        self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags="-coverprofile=/tmp/from-env"), [argv])
        list_packages.assert_not_called()
        list_patterns.assert_not_called()

    def test_goflags_values_and_ordinary_noncoverage_flags_do_not_false_positive(self):
        for goflags in ("-skip=-coverprofile", "-run=-test.coverprofile", "-exec=-coverprofile",
                        "-mod=readonly -count=1", "-ldflags=-w"):
            with self.subTest(goflags=goflags):
                argv = ["go", "test", NODE]
                commands = gtp.plan(argv, lambda: list(MODULE), lambda package: list(PATTERNS[package]),
                                    goflags=goflags)
                if goflags.startswith("-run="):
                    self.assertEqual(commands, [argv])
                else:
                    self.assertEqual(commands, partitions([], NODE))

    def test_invalid_goflags_and_unknown_command_flags_run_directly(self):
        argv = ["go", "test", NODE]
        for goflags in ("-coverprofile /tmp/c", "'-coverprofile=/tmp/unterminated", "not-a-flag"):
            with self.subTest(goflags=goflags):
                list_packages = mock.Mock(return_value=list(MODULE))
                list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
                self.assertEqual(gtp.plan(argv, list_packages, list_patterns, goflags=goflags), [argv])
                list_packages.assert_not_called()
                list_patterns.assert_not_called()
        for argv in (["go", "test", "-custom", "value", NODE], ["go", "test", NODE, "-custom=value"],
                     ["go", "test", "-coverprofile"]):
            with self.subTest(argv=argv):
                list_packages = mock.Mock(return_value=list(MODULE))
                list_patterns = mock.Mock(return_value=list(PATTERNS[NODE]))
                self.assertEqual(gtp.plan(argv, list_packages, list_patterns), [argv])
                list_packages.assert_not_called()
                list_patterns.assert_not_called()

    def test_covermode_without_coverprofile_keeps_the_partition_plan(self):
        flags = ["-covermode=atomic"]
        self.assertEqual(plan(["go", "test", *flags, "./..."]), [
            ["go", "test", *flags, "example/server/cmd/personad", "example/server/internal/assistantwire",
             "example/server/internal/node/sub"],
            *partitions(flags, ASSISTANT), *partitions(flags, NODE),
        ])

    def test_a_partition_is_described_by_its_package_and_size_not_its_names(self):
        self.assertEqual(gtp.describe(["go", "test", "-run", "^(TestA|TestB|TestC)$", NODE]),
                         "./internal/node partition (3 tests)")
        self.assertEqual(gtp.describe(["go", "test", "-run", "^(TestX)$", ASSISTANT]),
                         "./internal/assistant partition (1 tests)")
        self.assertEqual(gtp.describe(["go", "test", "-count=1", "./internal/hub", "./internal/kb"]), "2 package(s)")


class CoverageIntegrationTests(unittest.TestCase):
    """A real go test comparison in a temporary module outside the repository."""

    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="go-test-partitioned-cover-")
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.env = os.environ.copy()
        self.env.pop("GOFLAGS", None)
        self.env.update(GOTOOLCHAIN="local", GOMAXPROCS="2")
        (self.root / "go.mod").write_text("module example.com/coverfixture\n\ngo 1.27.1\n")
        self.write("internal/node/node.go", """package node

func Alpha() int {
	return 1
}

func Beta() int {
	return 2
}
""")
        self.write("internal/node/node_test.go", """package node

import "testing"

func TestAlpha(t *testing.T) {
	if Alpha() != 1 {
		t.Fatal("Alpha")
	}
}

func TestBeta(t *testing.T) {
	if Beta() != 2 {
		t.Fatal("Beta")
	}
}
""")
        self.write("internal/other/other.go", """package other

func Value() int {
	return 3
}
""")
        self.write("internal/other/other_test.go", """package other

import "testing"

func TestValue(t *testing.T) {
	if Value() != 3 {
		t.Fatal("Value")
	}
}
""")

    def write(self, relative, contents):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(contents)

    def run_command(self, command, env=None):
        return subprocess.run(command, cwd=self.root, env=env or self.env, stdout=subprocess.PIPE,
                              stderr=subprocess.STDOUT, text=True, timeout=120)

    def check_profile(self, path, mode, packages):
        lines = path.read_text().splitlines()
        self.assertTrue(lines)
        self.assertEqual(lines[0], "mode: " + mode)
        self.assertEqual(sum(line.startswith("mode:") for line in lines), 1)
        covered = {line.split(":", 1)[0].rsplit("/", 1)[0] for line in lines[1:] if line}
        self.assertEqual(covered, set(packages))
        result = self.run_command(["go", "tool", "cover", "-func=" + str(path)])
        self.assertEqual(result.returncode, 0, result.stdout)

    def compare_direct_and_wrapper(self, form, mode, package, passing=True, alias="-coverprofile", goflags=False):
        direct_profile = self.root / "direct.out"
        wrapper_profile = self.root / "wrapper.out"
        for profile in (direct_profile, wrapper_profile):
            profile.unlink(missing_ok=True)
        direct_flag = ([] if goflags else
                       [alias, str(direct_profile)] if form == "separate" else [alias + "=" + str(direct_profile)])
        wrapper_flag = ([] if goflags else
                        [alias, str(wrapper_profile)] if form == "separate" else [alias + "=" + str(wrapper_profile)])
        direct_env = self.env.copy()
        wrapper_env = self.env.copy()
        if goflags:
            direct_env["GOFLAGS"] = alias + "=" + str(direct_profile)
            wrapper_env["GOFLAGS"] = alias + "=" + str(wrapper_profile)
        base = ["go", "test", "-count=1", "-p", "1", "-covermode=" + mode]
        direct = self.run_command([*base, *direct_flag, package], env=direct_env)
        wrapper = self.run_command([sys.executable, "-I", str(Path(gtp.__file__).resolve()),
                                    *base, *wrapper_flag, package], env=wrapper_env)
        if passing:
            self.assertEqual(direct.returncode, 0, direct.stdout)
        else:
            self.assertNotEqual(direct.returncode, 0, direct.stdout)
        self.assertEqual(wrapper.returncode, direct.returncode, direct.stdout + wrapper.stdout)
        self.assertEqual(wrapper_profile.read_bytes(), direct_profile.read_bytes())
        packages = (["example.com/coverfixture/internal/node"] if package == NODE else
                    ["example.com/coverfixture/internal/node", "example.com/coverfixture/internal/other"])
        for profile in (direct_profile, wrapper_profile):
            self.check_profile(profile, mode, packages)

    def test_profiles_match_direct_go_for_every_flag_form_mode_and_package_selection(self):
        for form in ("separate", "equals"):
            for mode in ("set", "count", "atomic"):
                for package in ("./...", NODE):
                    with self.subTest(form=form, mode=mode, package=package):
                        self.compare_direct_and_wrapper(form, mode, package)

    def test_all_coverprofile_aliases_and_forms_match_direct_go(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for form in ("separate", "equals"):
                with self.subTest(alias=alias, form=form):
                    self.compare_direct_and_wrapper(form, "atomic", NODE, alias=alias)

    def test_goflags_coverprofile_aliases_match_direct_go(self):
        for alias in ("-coverprofile", "--coverprofile", "-test.coverprofile", "--test.coverprofile"):
            for mode in ("set", "count", "atomic"):
                with self.subTest(alias=alias, mode=mode):
                    self.compare_direct_and_wrapper("equals", mode, NODE, alias=alias, goflags=True)

    def test_empty_goflags_profile_can_be_overridden_without_creating_a_file(self):
        self.env["GOFLAGS"] = "-coverprofile=" + str(self.root / "unused.out")
        direct = self.run_command(["go", "test", "-count=1", "-p", "1", "-test.coverprofile=", NODE])
        wrapper = self.run_command([sys.executable, "-I", str(Path(gtp.__file__).resolve()),
                                    "go", "test", "-count=1", "-p", "1", "-test.coverprofile=", NODE])
        self.assertEqual(direct.returncode, 0, direct.stdout)
        self.assertEqual(wrapper.returncode, direct.returncode, direct.stdout + wrapper.stdout)
        self.assertFalse((self.root / "unused.out").exists())

    def test_a_failing_test_preserves_the_exit_status_and_profile(self):
        self.write("internal/node/failure_test.go", """package node

import "testing"

func TestFailure(t *testing.T) {
	t.Fatal("boom")
}
""")
        self.compare_direct_and_wrapper("equals", "atomic", NODE, passing=False)
        self.compare_direct_and_wrapper("separate", "count", NODE, passing=False, alias="--test.coverprofile")
        self.compare_direct_and_wrapper("equals", "set", NODE, passing=False, alias="--coverprofile", goflags=True)

    def test_args_coverprofile_matches_direct_go(self):
        for terminator in ("-args", "--args"):
            for form in ("separate", "equals"):
                with self.subTest(terminator=terminator, form=form):
                    direct_profile = self.root / "direct.out"
                    wrapper_profile = self.root / "wrapper.out"
                    for profile in (direct_profile, wrapper_profile):
                        profile.unlink(missing_ok=True)
                    direct_trailing = ([terminator, "-test.coverprofile", str(direct_profile)] if form == "separate"
                                       else [terminator, "-test.coverprofile=" + str(direct_profile)])
                    wrapper_trailing = ([terminator, "-test.coverprofile", str(wrapper_profile)] if form == "separate"
                                        else [terminator, "-test.coverprofile=" + str(wrapper_profile)])
                    base = ["go", "test", "-count=1", "-p", "1", "-cover", NODE]
                    direct = self.run_command([*base, *direct_trailing])
                    wrapper = self.run_command([sys.executable, "-I", str(Path(gtp.__file__).resolve()),
                                                *base, *wrapper_trailing])
                    self.assertEqual(direct.returncode, 0, direct.stdout)
                    self.assertEqual(wrapper.returncode, direct.returncode, direct.stdout + wrapper.stdout)
                    direct_lines = direct_profile.read_text().splitlines()
                    self.assertEqual(direct_lines[0], "mode: set")
                    self.assertEqual(len(direct_lines[1:]), 2, direct_profile.read_text())
                    self.assertTrue(all(line.endswith(" 1") for line in direct_lines[1:]), direct_profile.read_text())
                    self.assertEqual(wrapper_profile.read_text(), direct_profile.read_text())

    def test_args_test_run_matches_direct_go(self):
        for terminator in ("-args", "--args"):
            for form in ("separate", "equals"):
                with self.subTest(terminator=terminator, form=form):
                    trailing = ([terminator, "-test.run", "TestAlpha"] if form == "separate"
                                else [terminator, "-test.run=TestAlpha"])
                    base = ["go", "test", "-count=1", "-p", "1", "-v", NODE]
                    direct = self.run_command([*base, *trailing])
                    wrapper = self.run_command([sys.executable, "-I", str(Path(gtp.__file__).resolve()),
                                                *base, *trailing])
                    self.assertEqual(direct.returncode, 0, direct.stdout)
                    self.assertEqual(wrapper.returncode, direct.returncode, direct.stdout + wrapper.stdout)
                    self.assertEqual(direct.stdout.count("=== RUN   TestAlpha"), 1, direct.stdout)
                    self.assertNotIn("=== RUN   TestBeta", direct.stdout)
                    self.assertEqual(wrapper.stdout.count("=== RUN   TestAlpha"), 1, wrapper.stdout)
                    self.assertNotIn("=== RUN   TestBeta", wrapper.stdout)

    def test_noncoverage_coverprofile_tokens_keep_real_partitioning(self):
        for args in (["-skip", "-coverprofile", "./..."], ["./...", "--", "-coverprofile"]):
            with self.subTest(args=args):
                result = self.run_command([sys.executable, "-I", str(Path(gtp.__file__).resolve()),
                                           "go", "test", "-count=1", "-p", "1", *args])
                self.assertEqual(result.returncode, 0, result.stdout)
                self.assertIn("== go test:", result.stdout)
                self.assertFalse(list(self.root.glob("*.out")))


class RunTests(unittest.TestCase):
    def run_commands(self, commands):
        out = io.StringIO()
        with mock.patch.object(sys, "stdout", out):
            status = gtp.run(commands)
        return status, out.getvalue()

    def test_outputs_never_interleave_and_any_failure_fails(self):
        py = sys.executable
        status, text = self.run_commands([
            [py, "-c", "import time\nfor i in range(3): print('rest', i, flush=True); time.sleep(0.05)"],
            [py, "-c", "print('FAIL\\tpartition one'); raise SystemExit(1)"],
            [py, "-c", "print('ok\\tpartition two')"],
        ])
        self.assertEqual(status, 1)
        lines = text.splitlines()
        for line in ("rest 0", "rest 1", "rest 2", "FAIL\tpartition one", "ok\tpartition two"):
            self.assertIn(line, lines)
        self.assertEqual(lines.index("FAIL\tpartition one") - 1, next(
            i for i, line in enumerate(lines) if line.endswith("exit 1")))

    def test_all_passing_passes(self):
        py = sys.executable
        self.assertEqual(self.run_commands([[py, "-c", "pass"], [py, "-c", "pass"]])[0], 0)


if __name__ == "__main__":
    unittest.main()
