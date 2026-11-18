#!/usr/bin/env python3
"""Verify Windows release ZIPs natively, then bind their check receipts to uploaded bytes.

Receipts are outputs of the trusted release process, not remote attestations. The
uploader checks identity and hashes so old results cannot verify a different ZIP.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import stat
import subprocess
import sys
import tempfile
import zipfile

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "test"))
from server_test_env import clean_environment

ARCHIVE = re.compile(r"Persona-([0-9A-Za-z][0-9A-Za-z._-]*)-windows-(amd64|arm64)\.zip\Z")
ARCHIVE_FAMILY = re.compile(r"Persona-.*-windows-.*\.zip\Z", re.IGNORECASE)
PROGRAMS = ("persona-link.exe", "personad.exe")
MAX_BINARY_SIZE = 512 * 1024 * 1024
COMMAND_TIMEOUT = 20


class WindowsReleaseError(ValueError):
    """A release-check failure whose message contains no machine paths or command output."""


def check_pe_header(header, target):
    if len(header) < 64 or header[:2] != b"MZ":
        raise WindowsReleaseError("Windows executable has no PE header")
    offset = int.from_bytes(header[60:64], "little")
    machine = {"windows-amd64": 0x8664, "windows-arm64": 0xAA64}[target]
    if (offset < 64 or offset + 6 > len(header) or header[offset:offset + 4] != b"PE\0\0"
            or int.from_bytes(header[offset + 4:offset + 6], "little") != machine):
        raise WindowsReleaseError("Windows executable PE architecture differs from the archive")


def report_name(name):
    return name.removesuffix(".zip") + ".verification.json"


def fingerprint(path):
    digest = hashlib.sha256()
    size = 0
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1 << 20), b""):
            size += len(chunk)
            digest.update(chunk)
    return {"size": size, "sha256": digest.hexdigest()}


def archive_identity(name, version):
    match = ARCHIVE.fullmatch(name)
    if match is None or match[1] != version:
        raise WindowsReleaseError("Windows archive name does not match the requested version")
    return "windows-" + match[2]


def inspect_archive(path, version, destination=None):
    """Hash the exact members; extract only the two allowlisted files, without extractall."""
    path = Path(path)
    target = archive_identity(path.name, version)
    before = fingerprint(path)
    members = {}
    with zipfile.ZipFile(path) as archive:
        entries = {}
        for entry in archive.infolist():
            # ZipInfo normalizes backslashes on Windows and truncates NULs; validate the original spelling.
            name = entry.orig_filename
            parts = name.rstrip("/").split("/")
            if (not name or "\0" in name or "\\" in name or PurePosixPath(name).is_absolute()
                    or any(part in ("", ".", "..") or ":" in part for part in parts)
                    or stat.S_ISLNK(entry.external_attr >> 16)):
                raise WindowsReleaseError("Unsafe path in Windows archive")
            if name.casefold() in entries:
                raise WindowsReleaseError("Duplicate path in Windows archive")
            entries[name.casefold()] = entry
        for program in PROGRAMS:
            member = path.stem + "/" + program
            entry = entries.get(member.casefold())
            if entry is None or entry.filename != member or entry.is_dir():
                raise WindowsReleaseError("Windows archive is missing " + program)
            if not 0 < entry.file_size <= MAX_BINARY_SIZE:
                raise WindowsReleaseError("Invalid Windows executable size")
            digest, size, header = hashlib.sha256(), 0, b""
            output = None
            try:
                if destination is not None:
                    output = (Path(destination) / program).open("wb")
                with archive.open(entry) as stream:
                    for chunk in iter(lambda: stream.read(1 << 20), b""):
                        if not header:
                            header = chunk
                        size += len(chunk)
                        if size > MAX_BINARY_SIZE:
                            raise WindowsReleaseError("Windows executable exceeds the size limit")
                        digest.update(chunk)
                        if output is not None:
                            output.write(chunk)
            finally:
                if output is not None:
                    output.close()
            if size != entry.file_size:
                raise WindowsReleaseError("Windows executable size differs from ZIP entry")
            check_pe_header(header, target)
            members[program] = {"size": size, "sha256": digest.hexdigest()}
    if fingerprint(path) != before:
        raise WindowsReleaseError("Windows archive changed during verification")
    return {"name": path.name, **before}, members


def expected_build(assets, version):
    """Either release index may provide the build; all supplied indexes must agree."""
    builds = []
    for name in ("latest.json", "BUILD.json"):
        if name not in assets:
            continue
        metadata = json.loads(assets[name].read_text(encoding="utf-8"))
        build = metadata.get("build") if isinstance(metadata, dict) else None
        if (not isinstance(metadata, dict) or metadata.get("version") != version
                or type(build) is not int or not re.fullmatch(r"[0-9]{12}", str(build))):
            raise WindowsReleaseError(name + " does not identify the requested Windows release")
        builds.append(build)
    if not builds:
        raise WindowsReleaseError("Windows archives require latest.json or BUILD.json in the receipt")
    if len(set(builds)) != 1:
        raise WindowsReleaseError("Release indexes disagree on the Windows build")
    return builds[0]


def check_outputs(outputs, version, build, platform_name):
    """Match actual command results, never a version-shaped string elsewhere in a binary."""
    if not isinstance(outputs, dict):
        raise WindowsReleaseError("Windows verification has no command outputs")
    expected = {
        "link_version": version,
        "personad_version": version,
        "personad_verbose": "%s\nchannel release, build %d" % (version, build),
        "node_check": {"version": version, "os": "windows",
                       "arch": platform_name.removeprefix("windows-"), "program": "personad"},
    }
    if outputs != expected:
        raise WindowsReleaseError("Windows executable command outputs differ from the requested release")


def check_windows_archives(assets, expected_version):
    """Reject absent, stale or mismatched native checks before any release asset upload."""
    archives = [asset for asset in assets if ARCHIVE_FAMILY.fullmatch(asset["name"])]
    if not archives:
        return
    by_name = {asset["name"]: Path(asset["path"]) for asset in assets}
    if len(by_name) != len(assets):
        raise WindowsReleaseError("Duplicate asset names in Windows release receipt")
    build = expected_build(by_name, expected_version)
    for asset in archives:
        name, path = asset["name"], Path(asset["path"])
        target = archive_identity(name, expected_version)
        if path.name != name:
            raise WindowsReleaseError("Windows archive path and asset name differ")
        report_path = by_name.get(report_name(name))
        if report_path is None:
            raise WindowsReleaseError("Windows archive requires its native verification report: " + report_name(name))
        report = json.loads(report_path.read_text(encoding="utf-8"))
        if (not isinstance(report, dict) or type(report.get("schema")) is not int or report["schema"] != 1
                or report.get("version") != expected_version or report.get("build") != build
                or report.get("platform") != target):
            raise WindowsReleaseError("Windows verification report has the wrong release identity")
        archive, members = inspect_archive(path, expected_version)
        if report.get("archive") != archive or report.get("binaries") != members:
            raise WindowsReleaseError("Windows verification report hashes do not match the archive")
        check_outputs(report.get("outputs"), expected_version, build, target)


def command_output(path, arguments, directory, environment):
    result = subprocess.run([str(path), *arguments], capture_output=True, text=True,
                            encoding="utf-8", timeout=COMMAND_TIMEOUT, check=True,
                            cwd=directory, env=environment)
    if len(result.stdout) > 16384:
        raise WindowsReleaseError("Windows version command returned too much output")
    return result.stdout.replace("\r\n", "\n").strip()


def verify_native(path, version, build):
    """Run only version/self-check commands on the archive's matching native Windows host."""
    path = Path(path)
    target = archive_identity(path.name, version)
    machine = platform.machine().lower()
    architecture = {"amd64": "amd64", "x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}.get(machine)
    if platform.system() != "Windows" or target != "windows-" + str(architecture):
        raise WindowsReleaseError("Windows archive must be verified on its matching native Windows architecture")
    if type(build) is not int or not re.fullmatch(r"[0-9]{12}", str(build)):
        raise WindowsReleaseError("Expected Windows build must be a twelve-digit number")
    with tempfile.TemporaryDirectory(prefix="persona-windows-release-check-") as directory:
        archive, members = inspect_archive(path, version, directory)
        link, node = (Path(directory) / name for name in PROGRAMS)
        environment = clean_environment(os.environ)
        home = Path(directory) / "home"
        home.mkdir()
        for name in ("AppData", "LocalAppData"):
            (home / name).mkdir()
        environment.update(HOME=str(home), USERPROFILE=str(home), APPDATA=str(home / "AppData"),
                           LOCALAPPDATA=str(home / "LocalAppData"), PERSONA_HOME=str(home / ".persona"))

        def output(program, arguments):
            return command_output(program, arguments, directory, environment)

        outputs = {"link_version": output(link, ["version"]),
                   "personad_version": output(node, ["version"]),
                   "personad_verbose": output(node, ["version", "-v"])}
        node_check = json.loads(output(node, ["node-update-check"]))
        if not isinstance(node_check, dict):
            raise WindowsReleaseError("Node self-check did not return an object")
        # Future node capabilities are not release identity and must not copy private data into the report.
        outputs["node_check"] = {key: node_check.get(key) for key in ("version", "os", "arch", "program")}
        check_outputs(outputs, version, build, target)
    if fingerprint(path) != {key: archive[key] for key in ("size", "sha256")}:
        raise WindowsReleaseError("Windows archive changed while its programs were checked")
    return {"schema": 1, "version": version, "build": build, "platform": target,
            "archive": archive, "binaries": members, "outputs": outputs}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--build", type=int, required=True)
    args = parser.parse_args(argv)
    output = args.archive.with_name(report_name(args.archive.name))
    temporary = None
    try:
        # A failed recheck must not leave an earlier successful report beside this ZIP.
        if output.exists() or output.is_symlink():
            output.unlink()
        report = verify_native(args.archive, args.version, args.build)
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=output.parent,
                                         prefix=".windows-verification-", delete=False) as stream:
            temporary = Path(stream.name)
            json.dump(report, stream, indent=2)
            stream.write("\n")
        temporary.replace(output)
    except (OSError, ValueError, zipfile.BadZipFile, subprocess.SubprocessError) as error:
        # Child stdout/stderr and operating-system errors may contain machine paths.
        reason = "invalid release data"
        if isinstance(error, WindowsReleaseError):
            reason = str(error)
        elif isinstance(error, subprocess.TimeoutExpired):
            reason = "version command timed out"
        elif isinstance(error, subprocess.CalledProcessError):
            reason = "version command failed"
        elif isinstance(error, zipfile.BadZipFile):
            reason = "invalid ZIP archive"
        elif isinstance(error, OSError):
            reason = "could not read archive or write report"
        parser.exit(1, "Windows release verification failed: %s; no report was written.\n" % reason)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
    print("Verified " + args.archive.name)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
