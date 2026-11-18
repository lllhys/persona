"""The Android build commit is passed explicitly by every script that builds an APK (About shows it)."""
from pathlib import Path
import re
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[2]
CI = (ROOT / "scripts/ci/android.sh").read_text()
LOCAL = (ROOT / "scripts/app/android.sh").read_text()
GRADLE = (ROOT / "android/app/build.gradle.kts").read_text()
SHA = "0123456789abcdef0123456789abcdef01234567"


def commit_block(gradle):
    """The Gradle statement that resolves the build commit: only this block is held to the no-git, explicit-input rules."""
    return gradle.split("val buildCommit", 1)[1].split("}.lowercase()", 1)[0]


def gradle_problems(gradle):
    """What is wrong with how a build.gradle.kts carries the build commit. Other features' blocks (a BuildConfig switch
    another change turns on, say) are none of its business: only the commit's own statements are checked."""
    problems = []
    block = commit_block(gradle)
    if block.index('gradleProperty("personaBuildCommit")') > block.index('environmentVariable("PERSONA_BUILD_COMMIT")'):
        problems.append("the property must come before the environment variable")
    if '?: "unknown"' not in block:
        problems.append("no commit must mean unknown")
    if 'Regex("[0-9a-fA-F]{7,64}")' not in block:
        problems.append("the value must be checked as a commit")
    if re.search(r"rev-parse|ProcessBuilder|exec\(|\.git\b|\"git\"", block):
        problems.append("the commit must not be read from git")
    if 'resValue("string", "persona_build_commit", buildCommit)' not in gradle:
        problems.append("the commit must be a generated string resource")
    # It is never a BuildConfig field: a line that mentions the commit and buildConfigField together.
    if any("buildConfigField" in line and ("persona_build_commit" in line or "buildCommit" in line) for line in gradle.splitlines()):
        problems.append("the commit must not be a BuildConfig field")
    return problems


def resolved(env):
    """Run the CI script's own assignment with only [env] set and return what it resolves to."""
    line = next(line for line in CI.splitlines() if line.startswith("BUILD_COMMIT="))
    done = subprocess.run(["bash", "-c", line + '\nprintf %s "$BUILD_COMMIT"'], env=env, capture_output=True, text=True, check=True)
    return done.stdout


class CiBuildCommitTest(unittest.TestCase):
    def test_explicit_value_wins_over_the_runner_sha(self):
        self.assertEqual(resolved({"PERSONA_BUILD_COMMIT": "abcdef1", "GITHUB_SHA": SHA}), "abcdef1")

    def test_runner_sha_is_the_fallback(self):
        self.assertEqual(resolved({"GITHUB_SHA": SHA}), SHA)

    def test_nothing_set_or_empty_means_unknown(self):
        self.assertEqual(resolved({}), "unknown")
        self.assertEqual(resolved({"PERSONA_BUILD_COMMIT": "", "GITHUB_SHA": ""}), "unknown")

    def test_gradle_gets_the_value_and_the_script_never_asks_git(self):
        self.assertIn('-PpersonaBuildCommit="$BUILD_COMMIT"', CI)
        # Only the commit's own assignment is held to it: another step of this script may use git for something else.
        assignment = next(line for line in CI.splitlines() if line.startswith("BUILD_COMMIT="))
        self.assertNotRegex(assignment, r"git\s|rev-parse")
        self.assertNotIn(".config/persona", assignment)


class LocalBuildCommitTest(unittest.TestCase):
    def test_only_the_release_command_injects_the_repository_head(self):
        release = LOCAL.split("\n  release)", 1)[1].split("\n  install)", 1)[0]
        self.assertIn("-PpersonaBuildCommit=", release)
        self.assertIn("rev-parse HEAD", release)
        # An unreadable repository says unknown; an earlier build's value is never reused.
        self.assertIn("BUILD_COMMIT=unknown", release)
        self.assertEqual(LOCAL.count("-PpersonaBuildCommit="), 1)
        for plain in ("test)", "lint)", "apk)"):
            line = next(line for line in LOCAL.splitlines() if line.strip().startswith(plain))
            self.assertNotIn("personaBuildCommit", line)


class GradleBuildCommitTest(unittest.TestCase):
    def test_gradle_reads_only_the_property_then_the_environment_and_never_git(self):
        self.assertEqual(gradle_problems(GRADLE), [])

    def test_the_commit_is_a_generated_resource_and_not_manifest_metadata(self):
        self.assertIn('resValue("string", "persona_build_commit", buildCommit)', GRADLE)
        self.assertNotIn("persona_build_commit", (ROOT / "android/app/src/main/AndroidManifest.xml").read_text())

    def test_a_buildconfig_switch_for_another_feature_can_live_beside_the_commit(self):
        # A debug-only Rust shadow change adds buildFeatures { buildConfig = true } under namespace; merging it must not fail here.
        merged = GRADLE.replace("android {\n", "android {\n    buildFeatures { buildConfig = true }\n", 1)
        self.assertNotEqual(merged, GRADLE)
        self.assertEqual(gradle_problems(merged), [])
        both = merged.replace("buildFeatures { resValues = true }", "buildFeatures { resValues = true; buildConfig = true }")
        self.assertEqual(gradle_problems(both), [])

    def test_the_commit_as_a_buildconfig_field_or_a_git_read_is_still_refused(self):
        field = GRADLE.replace("        resValue(", '        buildConfigField("String", "BUILD_COMMIT", "\\"" + buildCommit + "\\"")\n        resValue(', 1)
        self.assertIn("the commit must not be a BuildConfig field", gradle_problems(field))
        git = GRADLE.replace('?: "unknown"', '?: "git rev-parse HEAD".execute().text', 1)
        self.assertIn("the commit must not be read from git", gradle_problems(git))
        self.assertIn("the commit must be a generated string resource", gradle_problems(GRADLE.replace('resValue("string", "persona_build_commit", buildCommit)', "")))

    def test_the_settings_module_reads_the_same_resource_name(self):
        reader = (ROOT / "android/feature/settings/src/main/kotlin/persona/feature/settings/AboutBuildCommit.kt").read_text()
        self.assertIn('BUILD_COMMIT_RESOURCE = "persona_build_commit"', reader)

    def test_the_other_lanes_files_carry_no_build_commit(self):
        devices = (ROOT / "android/feature/settings/src/main/kotlin/persona/feature/settings/DeviceSettingsPages.kt").read_text()
        page = devices.split("internal fun DevicesPage(", 1)[1].split("internal fun AboutSettingsPage", 1)[0]
        self.assertNotIn("BuildCommit", page)


if __name__ == "__main__":
    unittest.main()
