#!/usr/bin/env python3
"""Sign darwin command-line release artifacts with a stable Developer ID.

Requires an existing signing certificate in the runner keychain. Never prints
its private key, identity, or any notarization credential. Fixed identifiers
keep macOS privacy grants across updates; hashes are generated afterward.
"""
import argparse
import os
from pathlib import Path
import re
import subprocess
import sys

PROGRAMS = ("personad", "persona-node", "persona-link", "persona-hub", "persona-manager")
NAME = re.compile(r"^(personad|persona-node|persona-link|persona-hub|persona-manager)-darwin-(?:amd64|arm64)$")


def run(args, secret=False):
    result = subprocess.run(args, capture_output=True, text=True)
    if result.returncode:
        detail = result.stdout + result.stderr
        if secret:
            detail = "the signing command failed (its certificate or private-key output is not logged)"
        raise RuntimeError(detail.strip())
    return result.stdout + result.stderr


def sign_one(path, identity, verify=True):
    identifier = "com.nwverse." + path.name.rsplit("-darwin-", 1)[0]
    signing = ["codesign", "--force", "--sign", identity, "--identifier", identifier,
               "--timestamp=none", "--options", "runtime"]
    if keychain := os.environ.get("PERSONA_SIGNING_KEYCHAIN"):
        signing += ["--keychain", keychain]
    run(signing + [str(path)], secret=True)
    if verify:
        run(["codesign", "--verify", "--strict", str(path)])
        requirement = run(["codesign", "-d", "-r-", str(path)])
        if "designated => identifier " not in requirement or "cdhash" in requirement:
            raise RuntimeError(f"{path.name}: signature is still an ad-hoc cdhash")
        if f'identifier "{identifier}"' not in requirement:
            raise RuntimeError(f"{path.name}: unexpected designated requirement")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    parser.add_argument("--identity", required=True, help="certificate name or SHA-1, supplied via the environment")
    args = parser.parse_args()
    if not args.directory.is_dir() or args.directory.is_symlink():
        raise RuntimeError("release binary directory must be a real directory")
    files = sorted(path for path in args.directory.iterdir() if path.is_file() and not path.is_symlink() and NAME.fullmatch(path.name))
    if not files:
        raise RuntimeError("no darwin command-line binaries to sign")
    for path in files:
        sign_one(path, args.identity)
    print(f"Signed and verified {len(files)} darwin command-line binaries with fixed identifiers")


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, ValueError) as error:
        print(f"Darwin release signing failed: {error}", file=sys.stderr)
        raise SystemExit(1)
