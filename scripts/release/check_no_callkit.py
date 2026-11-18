#!/usr/bin/env python3
"""No binary the app ships links CallKit (#9290; docs/operations/release.md "No CallKit").

App Review rejects a build offered in China when any of its binaries links CallKit.framework, even weakly and even
when the only use is an SDK's CXCallObserver. Calls go through LiveCommunicationKit instead. That system framework
links CallKit itself, which is fine: only the Mach-O files inside the app count.

  scripts/release/check_no_callkit.py <.app | .ipa | .xcarchive> ...

Every file under the bundle that starts with a Mach-O magic number is checked, whatever its name: the main
executable, *.debug.dylib, Frameworks/*, PlugIns/*.appex, the Watch app and its extensions. Symbolic links are not
followed, and a file reached twice is checked once. `otool -arch all -L` lists each architecture's dylib load commands
(weak, re-exported, upward and lazy ones too); an install name that mentions CallKit (CallKit.framework,
libswiftCallKit.dylib) fails the check, printed as `binary [arch] → dependency`. An otool failure, output it cannot
read, or no Mach-O at all fails it too. On success it prints one line: `no CallKit: N Mach-O files checked`.
"""
import os
import re
import subprocess
import sys
import tempfile
import zipfile

THIN = {b"\xfe\xed\xfa\xce", b"\xfe\xed\xfa\xcf", b"\xce\xfa\xed\xfe", b"\xcf\xfa\xed\xfe"}
FAT = {b"\xca\xfe\xba\xbe", b"\xca\xfe\xba\xbf"}
CALLKIT = re.compile(r"callkit", re.I)
# "\t<install name> (compatibility version 1.0.0, current version 1.0.0[, weak | reexport | upward | lazy])"
DEPENDENCY = re.compile(r"\t(.+?) \(compatibility version [^,()]*, current version [^,()]*(?:, ([^()]*))?\)$")
TIMEOUT = 120


class CheckError(Exception):
    """The check could not run to the end: no verdict, so it must not pass."""


def is_macho(head):
    """Whether a file's first eight bytes start a Mach-O: thin, either byte order, or universal. A Java class file
    starts with CAFEBABE too; its next word is the class version (45 or more), a universal binary's its architecture
    count."""
    if head[:4] in THIN:
        return True
    return head[:4] in FAT and len(head) >= 8 and 0 < int.from_bytes(head[4:8], "big") < 45


def macho_files(root):
    """The Mach-O files under root, sorted; symbolic links are not followed and hard links count once."""
    def unreadable(error):
        raise error

    found, seen = [], set()
    for directory, dirs, files in os.walk(root, onerror=unreadable):
        dirs.sort()
        for name in sorted(files):
            path = os.path.join(directory, name)
            if os.path.islink(path):
                continue
            st = os.stat(path)
            if (st.st_dev, st.st_ino) in seen:
                continue
            seen.add((st.st_dev, st.st_ino))
            with open(path, "rb") as f:
                if is_macho(f.read(8)):
                    found.append(path)
    return found


def dependencies(path, run=None):
    """[(architecture, install name, attribute)] of one Mach-O: every architecture's dylib load commands, the
    library's own install name first. The architecture is empty for a thin file, the attribute for a plain load."""
    try:
        out = (run or subprocess.run)(["otool", "-arch", "all", "-L", path], capture_output=True, text=True,
                                      timeout=TIMEOUT)
    except (OSError, subprocess.SubprocessError) as error:
        raise CheckError("otool -L %s: %s" % (path, error))
    if out.returncode != 0 or out.stderr.strip():
        raise CheckError("otool -L %s exited %d: %s" % (path, out.returncode, out.stderr.strip()[:500]))
    found, arch = [], None
    for line in out.stdout.splitlines():
        if not line.strip():
            continue
        if line == path + ":":
            arch = ""
        elif line.startswith(path + " (architecture ") and line.endswith("):"):
            arch = line[len(path) + len(" (architecture "):-2]
        elif arch is not None and line.startswith("\t"):
            # A load command in a format DEPENDENCY does not know still counts, whole, rather than passing unread.
            m = DEPENDENCY.match(line)
            found.append((arch, m.group(1), m.group(2) or "") if m else (arch, line.strip(), ""))
        else:
            # "is not an object file", "truncated or malformed", a format this parser does not know: no verdict.
            raise CheckError("otool -L %s: unexpected line %r" % (path, line[:200]))
    if arch is None:
        raise CheckError("otool -L %s printed no architecture" % path)
    return found


def scan(root, run=None):
    """(Mach-O files checked, `binary [arch] → dependency` for each CallKit dependency) under root; names are
    relative to root's parent, so they start with the bundle's own name."""
    if not os.path.isdir(root):
        raise CheckError("%s is not a directory" % root)
    try:
        files = macho_files(root)
    except OSError as error:
        raise CheckError("reading %s: %s" % (root, error))
    if not files:
        raise CheckError("no Mach-O file under %s" % root)
    base = os.path.dirname(os.path.abspath(root))
    linked = []
    for path in files:
        for arch, name, attribute in dependencies(path, run):
            if CALLKIT.search(name):
                linked.append("%s%s → %s%s" % (os.path.relpath(path, base), " [%s]" % arch if arch else "", name,
                                               " (%s)" % attribute if attribute else ""))
    return len(files), linked


def extract_macho(ipa, into):
    """Write the .ipa's Mach-O members under into (the rest is never unpacked); returns its Payload directory."""
    with zipfile.ZipFile(ipa) as z:
        for member in z.infolist():
            if member.is_dir() or (member.external_attr >> 16) & 0o170000 == 0o120000:  # a directory, a symlink
                continue
            with z.open(member) as f:
                if is_macho(f.read(8)):
                    z.extract(member, into)
    return os.path.join(into, "Payload")


def check(path, run=None):
    """scan() of an .app, an .ipa's Payload or an .xcarchive's applications."""
    path = path.rstrip("/")
    if path.endswith(".ipa"):
        with tempfile.TemporaryDirectory() as tmp:
            try:
                payload = extract_macho(path, tmp)
            except (OSError, zipfile.BadZipFile) as error:
                raise CheckError("%s: %s" % (path, error))
            return scan(payload, run)
    if path.endswith(".xcarchive"):
        return scan(os.path.join(path, "Products", "Applications"), run)
    return scan(path, run)


def main(argv):
    if len(argv) < 2 or any(a.startswith("-") for a in argv[1:]):
        sys.exit(__doc__)
    failed = False
    for path in argv[1:]:
        try:
            checked, linked = check(path)
        except CheckError as error:
            sys.exit("check_no_callkit: FAIL %s" % error)
        for line in linked:
            print(line, flush=True)
        if linked:
            print("check_no_callkit: FAIL %s links CallKit (%d dependencies); App Review rejects it for China "
                  "(docs/operations/release.md \"No CallKit\")" % (os.path.basename(path.rstrip("/")), len(linked)),
                  file=sys.stderr)
            failed = True
        else:
            print("no CallKit: %d Mach-O files checked" % checked)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
