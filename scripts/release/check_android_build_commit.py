#!/usr/bin/env python3
"""Check that a release APK carries the commit it was built from (docs/android/android.md, "Build commit").

    check_android_build_commit.py <release apk> <full commit sha> [--build-tools <dir>]

About reads the `persona_build_commit` string resource. A build that was given no commit, the wrong one, or an APK an
earlier build left behind has a different value (or none), so reading it back with `aapt2 dump resources` stops a release
before the APK is copied for signing. Exits 1 with one line when the value is not exactly the commit.
"""
from __future__ import annotations

import argparse
from pathlib import Path
import re
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
from check_android_shrink import build_tools  # noqa: E402  (the same aapt2 lookup the shrink check uses)

RESOURCE = "persona_build_commit"
HEADER = re.compile(r"^\s*resource\s+0x[0-9a-fA-F]+\s+(\S+)")
VALUE = re.compile(r"^\s+\(([^)]*)\)\s+(.*)$")


def commit_values(dump):
    """The (config, value) pairs `aapt2 dump resources` lists under string/persona_build_commit.

    A value that is not a plain quoted string (a reference, a style) is kept as written, so it never equals a commit."""
    values, inside = [], False
    for line in dump.splitlines():
        header = HEADER.match(line)
        if header:
            inside = header.group(1) == "string/" + RESOURCE
            continue
        value = VALUE.match(line) if inside else None
        if value:
            text = value.group(2).strip()
            if len(text) >= 2 and text[0] == text[-1] == '"':
                text = text[1:-1]
            values.append((value.group(1), text))
    return values


def problem(dump, sha):
    """None when every value of the resource, in every configuration, is sha; otherwise what is wrong."""
    values = commit_values(dump)
    if not values:
        return f"{RESOURCE} is not in the APK's resources"
    if not any(config == "" for config, _ in values):
        return f"{RESOURCE} has no default value"
    wrong = sorted({value for _, value in values if value != sha})
    if wrong:
        # The injected value is a commit, never private, so printing both makes the failure readable.
        return f"{RESOURCE} is {', '.join(wrong)} but the build commit is {sha}"
    return None


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__.split("\n", 1)[0])
    parser.add_argument("apk", type=Path)
    parser.add_argument("sha", help="the commit the APK must carry, in full")
    parser.add_argument("--build-tools", help="Android build-tools directory holding aapt2")
    args = parser.parse_args(argv)
    if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", args.sha):
        print("check_android_build_commit: the commit must be a full lowercase sha", file=sys.stderr)
        return 1
    aapt2 = build_tools(args.build_tools) / "aapt2"
    dump = subprocess.run([str(aapt2), "dump", "resources", str(args.apk)],
                          check=True, capture_output=True, text=True).stdout
    found = problem(dump, args.sha)
    if found:
        print("check_android_build_commit: " + found, file=sys.stderr)
        print(f"check_android_build_commit: FAIL {args.apk.name}")
        return 1
    print(f"check_android_build_commit: PASS {args.apk.name}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
