"""check_no_callkit.py: every Mach-O in a built app, every architecture, no CallKit dependency (#9290).

otool is faked, so these run on Linux too.

    python3 scripts/release/test_check_no_callkit.py
"""
import contextlib
import io
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import check_no_callkit as cnc  # noqa: E402

THIN64 = b"\xcf\xfa\xed\xfe" + b"\0" * 28  # MH_MAGIC_64, little-endian (arm64, x86_64)
FAT2 = b"\xca\xfe\xba\xbe\0\0\0\x02" + b"\0" * 24  # a universal binary with two architectures
JAVA = b"\xca\xfe\xba\xbe\0\0\0\x34"  # a class file: CAFEBABE, then version 52
SYSTEM = "/usr/lib/libSystem.B.dylib (compatibility version 1.0.0, current version 1359.0.0)"
FOUNDATION = ("/System/Library/Frameworks/Foundation.framework/Foundation (compatibility version 300.0.0, "
              "current version 5027.0.69)")
LCK = ("/System/Library/Frameworks/LiveCommunicationKit.framework/LiveCommunicationKit (compatibility version 1.0.0, "
       "current version 1.0.0)")
PUSHKIT = "/System/Library/Frameworks/PushKit.framework/PushKit (compatibility version 1.0.0, current version 1.0.0)"
INTENTS = "/usr/lib/swift/libswiftIntents.dylib (compatibility version 1.0.0, current version 12.0.0, weak)"
CALLKIT = "/System/Library/Frameworks/CallKit.framework/CallKit (compatibility version 1.0.0, current version 1.0.0)"
CALLKIT_WEAK = CALLKIT[:-1] + ", weak)"
CALLKIT_REEXPORT = CALLKIT[:-1] + ", reexport)"
SWIFT_CALLKIT = "/usr/lib/swift/libswiftCallKit.dylib (compatibility version 1.0.0, current version 5.0.0, weak)"


def otool_output(path, archs):
    """What `otool -arch all -L path` prints: archs maps an architecture ("" for a thin file) to its load commands."""
    out = []
    for arch, deps in archs.items():
        out.append(path + (" (architecture %s):" % arch if arch else ":"))
        out += ["\t" + d for d in deps]
    return "\n".join(out) + "\n"


class FakeOtool:
    """Answers otool for each file by its name; records the commands."""

    def __init__(self, by_name, default=None):
        self.by_name, self.default, self.calls = by_name, default if default is not None else {"": [SYSTEM]}, []

    def __call__(self, cmd, **kwargs):
        self.calls.append(cmd)
        assert cmd[:4] == ["otool", "-arch", "all", "-L"], cmd
        assert kwargs.get("timeout"), kwargs
        answer = self.by_name.get(os.path.basename(cmd[-1]), self.default)
        if isinstance(answer, subprocess.CompletedProcess):
            return answer
        if isinstance(answer, Exception):
            raise answer
        return subprocess.CompletedProcess(cmd, 0, otool_output(cmd[-1], answer), "")


class AppTree(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.app = self.root / "Persona.app"

    def write(self, rel, data):
        path = self.app / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
        return path

    def full_app(self):
        """An app laid out like the iOS build: main stub and debug dylib, frameworks, extensions, the Watch app."""
        for rel in ("Persona", "Persona.debug.dylib", "__preview.dylib", "Frameworks/PersonaCore.framework/PersonaCore",
                    "PlugIns/PersonaNotify.appex/PersonaNotify", "PlugIns/PersonaWidgets.appex/PersonaWidgets",
                    "Watch/Persona.app/Persona",
                    "Watch/Persona.app/PlugIns/PersonaWatchWidgets.appex/PersonaWatchWidgets"):
            self.write(rel, THIN64)
        self.write("Watch/Persona.app/Frameworks/PersonaWire.framework/PersonaWire", FAT2)
        # Resources, a file named like a library that is not one, and a class file are not Mach-O.
        self.write("Info.plist", b"<?xml version")
        self.write("Assets.car", b"BOMStore")
        self.write("Fake.dylib", b"#!/bin/sh\n")
        self.write("Tool.class", JAVA)
        self.write("empty", b"")


class MachOFiles(AppTree):
    def test_magic_numbers_not_names(self):
        for head in (THIN64, b"\xfe\xed\xfa\xcf", b"\xce\xfa\xed\xfe" + b"\0" * 4, b"\xfe\xed\xfa\xce", FAT2,
                     b"\xca\xfe\xba\xbf\0\0\0\x01"):
            self.assertTrue(cnc.is_macho(head), head)
        for head in (JAVA, b"\xca\xfe\xba\xbe\0\0\0\0", b"\xca\xfe\xba\xbe", b"", b"\x89PNG\r\n\x1a\n", b"!<arch>\n"):
            self.assertFalse(cnc.is_macho(head), head)

    def test_every_binary_in_every_bundle(self):
        self.full_app()
        found = [os.path.relpath(p, self.app) for p in cnc.macho_files(self.app)]
        self.assertEqual(sorted(found), sorted([
            "Persona", "Persona.debug.dylib", "__preview.dylib", "Frameworks/PersonaCore.framework/PersonaCore",
            "PlugIns/PersonaNotify.appex/PersonaNotify", "PlugIns/PersonaWidgets.appex/PersonaWidgets",
            "Watch/Persona.app/Persona", "Watch/Persona.app/PlugIns/PersonaWatchWidgets.appex/PersonaWatchWidgets",
            "Watch/Persona.app/Frameworks/PersonaWire.framework/PersonaWire"]))

    def test_links_are_not_followed_and_a_file_counts_once(self):
        main = self.write("Persona", THIN64)
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "Elsewhere").write_bytes(THIN64)
        (self.app / "Frameworks").mkdir()
        (self.app / "Frameworks" / "Current").symlink_to(main)
        (self.app / "Frameworks" / "Out").symlink_to(outside, target_is_directory=True)
        os.link(main, self.app / "Frameworks" / "HardLink")
        self.assertEqual(len(cnc.macho_files(self.app)), 1)


class Dependencies(unittest.TestCase):
    def run_otool(self, stdout, returncode=0, stderr="", path="/x/Persona.app/Persona"):
        fake = lambda cmd, **kw: subprocess.CompletedProcess(cmd, returncode, stdout, stderr)  # noqa: E731
        return cnc.dependencies(path, fake)

    def test_thin_and_universal(self):
        path = "/x/Persona.app/Persona"
        self.assertEqual(self.run_otool(otool_output(path, {"": [SYSTEM, CALLKIT_WEAK]})), [
            ("", "/usr/lib/libSystem.B.dylib", ""),
            ("", "/System/Library/Frameworks/CallKit.framework/CallKit", "weak")])
        self.assertEqual(self.run_otool(otool_output(path, {"x86_64": [SYSTEM], "arm64": [SYSTEM, CALLKIT_REEXPORT]})), [
            ("x86_64", "/usr/lib/libSystem.B.dylib", ""), ("arm64", "/usr/lib/libSystem.B.dylib", ""),
            ("arm64", "/System/Library/Frameworks/CallKit.framework/CallKit", "reexport")])

    def test_a_load_command_in_an_unknown_format_still_counts(self):
        path = "/x/Persona.app/Persona"
        found = self.run_otool(path + ":\n\t/System/Library/Frameworks/CallKit.framework/CallKit (new format)\n")
        self.assertEqual(found, [("", "/System/Library/Frameworks/CallKit.framework/CallKit (new format)", "")])

    def test_tool_errors_are_failures_not_passes(self):
        path = "/x/Persona.app/Persona"
        for stdout, code, stderr in ((path + ": is not an object file\n", 0, ""),  # otool exits 0 for this
                                     ("", 1, "can't open file"),
                                     (otool_output(path, {"": [SYSTEM]}), 0, "warning: truncated"),
                                     ("", 0, ""),  # no header at all
                                     ("\t" + SYSTEM + "\n", 0, ""),  # a load command before any header
                                     (otool_output("/other/file", {"": [SYSTEM]}), 0, "")):
            with self.subTest(stdout=stdout, code=code, stderr=stderr), self.assertRaises(cnc.CheckError):
                self.run_otool(stdout, code, stderr, path)
        for error in (FileNotFoundError("otool"), subprocess.TimeoutExpired(["otool"], 120)):
            def fail(cmd, **kwargs):
                raise error
            with self.subTest(error=error), self.assertRaises(cnc.CheckError):
                cnc.dependencies(path, fail)


class Scan(AppTree):
    def test_a_clean_app_passes(self):
        self.full_app()
        fake = FakeOtool({"Persona.debug.dylib": {"": [SYSTEM, FOUNDATION, LCK, PUSHKIT, INTENTS]}})
        self.assertEqual(cnc.scan(self.app, fake), (9, []))
        self.assertEqual(len(fake.calls), 9)

    def test_callkit_anywhere_is_reported_binary_arch_dependency(self):
        self.full_app()
        fake = FakeOtool({
            "Persona.debug.dylib": {"": [SYSTEM, LCK, CALLKIT]},
            "PersonaCore": {"": ["@rpath/PersonaCore.framework/PersonaCore (compatibility version 0.0.0, "
                                 "current version 0.0.0)", SYSTEM, SWIFT_CALLKIT]},
            "PersonaWatchWidgets": {"": [SYSTEM, CALLKIT_REEXPORT]},
            "PersonaWire": {"armv7k": [SYSTEM], "arm64_32": [SYSTEM, CALLKIT_WEAK]},
        })
        checked, linked = cnc.scan(self.app, fake)
        self.assertEqual(checked, 9)
        self.assertEqual(sorted(linked), sorted([
            "Persona.app/Persona.debug.dylib → /System/Library/Frameworks/CallKit.framework/CallKit",
            "Persona.app/Frameworks/PersonaCore.framework/PersonaCore → /usr/lib/swift/libswiftCallKit.dylib (weak)",
            "Persona.app/Watch/Persona.app/PlugIns/PersonaWatchWidgets.appex/PersonaWatchWidgets → "
            "/System/Library/Frameworks/CallKit.framework/CallKit (reexport)",
            "Persona.app/Watch/Persona.app/Frameworks/PersonaWire.framework/PersonaWire [arm64_32] → "
            "/System/Library/Frameworks/CallKit.framework/CallKit (weak)"]))

    def test_no_binary_or_no_bundle_is_a_failure(self):
        self.write("Info.plist", b"<?xml version")
        with self.assertRaises(cnc.CheckError):
            cnc.scan(self.app, FakeOtool({}))
        with self.assertRaises(cnc.CheckError):
            cnc.scan(self.root / "Missing.app", FakeOtool({}))

    def test_one_failing_binary_fails_the_scan(self):
        self.full_app()
        broken = subprocess.CompletedProcess([], 1, "", "truncated or malformed object")
        with self.assertRaises(cnc.CheckError):
            cnc.scan(self.app, FakeOtool({"PersonaNotify": broken}))


class Containers(AppTree):
    def test_an_ipa_unpacks_only_its_binaries(self):
        self.full_app()
        ipa = self.root / "Persona.ipa"
        with zipfile.ZipFile(ipa, "w") as z:
            for path in sorted(self.app.rglob("*")):
                if path.is_file():
                    z.write(path, "Payload/" + path.relative_to(self.root).as_posix())
        seen = []

        def otool(cmd, **kwargs):
            seen.append(cmd[-1])
            self.assertFalse(os.path.exists(os.path.join(os.path.dirname(cmd[-1]), "Info.plist")))
            deps = [SYSTEM, CALLKIT] if cmd[-1].endswith("PersonaNotify") else [SYSTEM]
            return subprocess.CompletedProcess(cmd, 0, otool_output(cmd[-1], {"": deps}), "")
        checked, linked = cnc.check(str(ipa), otool)
        self.assertEqual(checked, 9)
        self.assertEqual(linked, ["Payload/Persona.app/PlugIns/PersonaNotify.appex/PersonaNotify → "
                                  "/System/Library/Frameworks/CallKit.framework/CallKit"])
        self.assertTrue(all(not os.path.exists(p) for p in seen))  # the unpacked copy is gone

    def test_a_broken_ipa_is_a_failure(self):
        bad = self.root / "Persona.ipa"
        bad.write_bytes(b"not a zip")
        with self.assertRaises(cnc.CheckError):
            cnc.check(str(bad), FakeOtool({}))

    def test_an_archive_checks_its_applications(self):
        archive = self.root / "Persona 1.xcarchive"
        self.app = archive / "Products" / "Applications" / "Persona.app"
        self.full_app()
        self.write("../../../dSYMs/Persona.app.dSYM/Contents/Resources/DWARF/Persona", THIN64)  # not shipped
        self.assertEqual(cnc.check(str(archive) + "/", FakeOtool({})), (9, []))


class Main(AppTree):
    def main(self, by_name):
        out, err = io.StringIO(), io.StringIO()
        with patch.object(cnc.subprocess, "run", FakeOtool(by_name)), contextlib.redirect_stdout(out), \
                contextlib.redirect_stderr(err):
            code = cnc.main(["check_no_callkit.py", str(self.app)])
        return code, out.getvalue(), err.getvalue()

    def test_success_is_one_line(self):
        self.full_app()
        self.assertEqual(self.main({}), (0, "no CallKit: 9 Mach-O files checked\n", ""))

    def test_failure_names_each_binary_and_exits_nonzero(self):
        self.full_app()
        code, out, err = self.main({"Persona.debug.dylib": {"": [SYSTEM, CALLKIT, SWIFT_CALLKIT]}})
        self.assertEqual(code, 1)
        self.assertEqual(out.splitlines(), [
            "Persona.app/Persona.debug.dylib → /System/Library/Frameworks/CallKit.framework/CallKit",
            "Persona.app/Persona.debug.dylib → /usr/lib/swift/libswiftCallKit.dylib (weak)"])
        self.assertIn("FAIL Persona.app links CallKit (2 dependencies)", err)

    def test_a_tool_error_exits_with_its_reason(self):
        self.full_app()
        with patch.object(cnc.subprocess, "run", FakeOtool({}, default=FileNotFoundError("otool"))), \
                self.assertRaises(SystemExit) as stop:
            cnc.main(["check_no_callkit.py", str(self.app)])
        self.assertIn("check_no_callkit: FAIL otool -L", str(stop.exception))

    def test_usage(self):
        for argv in (["check_no_callkit.py"], ["check_no_callkit.py", "--help"]):
            with self.assertRaises(SystemExit) as stop:
                cnc.main(argv)
            self.assertIn("check_no_callkit.py <.app | .ipa | .xcarchive>", str(stop.exception))


if __name__ == "__main__":
    unittest.main()
