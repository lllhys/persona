"""Closures that system APIs run on a queue of their own are not main-actor code (docs/lessons/ios.md).

The Apple targets default to main-actor isolation, so a plain closure written in app code is main-actor code, and Swift
checks that at run time: when Photos, Speech, EventKit or WatchConnectivity call it on their queue, the app stops
(`dispatch_assert_queue_fail`). Save Image crashed that way (2026-10-31), and turning on urgent calls did (#9256). The
SDK does not mark these blocks `Sendable`, so the compiler says nothing. Each closure passed to them is written
`{ @Sendable … in` (a Sendable closure is not isolated to the main actor), and a new thread's body is a typed
`@Sendable` value (#4703).

A source scan, so it runs without a build in every Apple PR's guard pass (#9258). It used to be a PersonaTests class,
which only `xcodebuild test` runs, and #9092 merged without running it.
"""

from functools import cache
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "scripts" / "i18n"))
import apple_comments  # noqa: E402

APPLE = ROOT / "apple"

# Calls whose block runs off the main thread and is not `Sendable` in the SDK (checked in the headers). A call whose
# block the SDK marks `Sendable` (HealthKit's queries, URLSession, Network) needs no entry: its closure is not the main
# actor's, and assigning main-actor state in it does not compile.
API = re.compile(
    r"\.(performChanges|requestImage\w*|fetchReminders|sendMessage|getNotificationSettings|getDeliveredNotifications"
    r"|getPendingNotificationRequests|evaluatePolicy|loadObject|loadDataRepresentation|loadFileRepresentation"
    r"|prepareForDisplay|prepareThumbnail)\b"
    r"|SFSpeechRecognizer\.requestAuthorization\b"
    r"|\b(replyHandler|errorHandler): \{"
)

# A closure literal handed to a new thread (`Thread { … }`, `Thread(block: { … })`, `detachNewThread { … }`). Some
# batch builds import `Thread(block:)`'s block as `@MainActor @Sendable`, and a literal takes that isolation (#4703).
THREAD_LITERAL = re.compile(r"(?<![\w.])(?<!-> )Thread\s*(\(\s*block:\s*)?\{|\bdetachNewThread\s*(\(\s*block:\s*)?\{")


def without_comments(src: str) -> str:
    """The source with its comments blanked out, lines kept."""
    out = list(src)
    for start, end in apple_comments.c_comments(src):
        for i in range(start, end):
            if out[i] != "\n":
                out[i] = " "
    return "".join(out)


def unsendable_callbacks(src: str) -> list[int]:
    """The lines where a closure literal goes to a system API that runs it on its own queue without `@Sendable`."""
    found = []
    for n, line in enumerate(code(src).split("\n"), 1):
        m = API.search(line)
        if not m:
            continue
        # A closure literal after the call (a function passed by name is that function's business).
        rest = line[m.start():]
        brace = rest.find("{")
        if brace >= 0 and not rest[brace + 1:].lstrip().startswith("@Sendable"):
            found.append(n)
    return found


def thread_literals(src: str) -> list[int]:
    """The lines where a closure literal is handed to a new thread."""
    return [n for n, line in enumerate(code(src).split("\n"), 1) if THREAD_LITERAL.search(line)]


def app_sources() -> list[Path]:
    """The Swift files of the app targets (every directory under apple/ but tests and the local packages, which set
    their own isolation)."""
    dirs = [d for d in sorted(APPLE.iterdir()) if d.is_dir() and not d.name.endswith("Tests") and d.name != "Packages"]
    return [f for d in dirs for f in sorted(d.rglob("*.swift"))
            if not apple_comments.SKIP_DIRS & set(f.relative_to(APPLE).parts)]


@cache
def read(path: Path) -> str:
    return path.read_text(errors="replace")


@cache
def code(src: str) -> str:
    return without_comments(src)


def scan(rule) -> list[str]:
    """`file:line: code` for each line `rule` finds in the app's sources."""
    found = []
    for path in app_sources():
        src = read(path)
        lines = src.split("\n")
        found += [f"{path.relative_to(ROOT)}:{n}: {lines[n - 1].strip()}" for n in rule(src)]
    return found


class AppleBackgroundCallbackTests(unittest.TestCase):
    def test_closures_run_on_a_system_queue_are_sendable(self):
        found = scan(unsendable_callbacks)
        self.assertEqual(found, [], "write these closures `{ @Sendable … in` (docs/lessons/ios.md):\n" + "\n".join(found))

    def test_thread_bodies_are_typed_values(self):
        found = scan(thread_literals)
        self.assertEqual(found, [], "give the thread a `let body: @Sendable () -> Void = { … }`, then "
                         "`Thread(block: body)` (#4703):\n" + "\n".join(found))

    def test_the_scan_reaches_the_app_targets(self):
        files = {p.relative_to(APPLE).parts[0] for p in app_sources()}
        self.assertTrue({"Persona", "Shared", "PersonaWatch"} <= files, files)
        self.assertFalse({"PersonaTests", "PersonaUITests", "Packages"} & files, files)

    def test_what_is_found_and_what_is_not(self):
        # #9256: a plain closure from a main-actor type, answered on Speech's queue.
        self.assertEqual(unsendable_callbacks(
            "let speech = await withCheckedContinuation { (done: CheckedContinuation<Bool, Never>) in\n"
            "    SFSpeechRecognizer.requestAuthorization { done.resume(returning: $0 == .authorized) }\n}"), [2])
        self.assertEqual(unsendable_callbacks("PHPhotoLibrary.shared().performChanges({ request() }) { ok, _ in done(ok) }"), [1])
        self.assertEqual(unsendable_callbacks("session.sendMessage(m, replyHandler: { r in reply(r) }, errorHandler: nil)"), [1])
        self.assertEqual(unsendable_callbacks("store.fetchReminders(matching: p) { found in c.resume(returning: found) }"), [1])
        # Sendable closures, a function passed by name, a call with no closure on its line, and comments.
        self.assertEqual(unsendable_callbacks("SFSpeechRecognizer.requestAuthorization { @Sendable in c.resume(returning: $0) }"), [])
        self.assertEqual(unsendable_callbacks("SFSpeechRecognizer.requestAuthorization { @Sendable status in c.resume(returning: status) }"), [])
        self.assertEqual(unsendable_callbacks("SFSpeechRecognizer.requestAuthorization(answer)"), [])
        self.assertEqual(unsendable_callbacks("let status = SFSpeechRecognizer.authorizationStatus()"), [])
        self.assertEqual(unsendable_callbacks("// SFSpeechRecognizer.requestAuthorization { done() }\nlet a = 1"), [])
        self.assertEqual(unsendable_callbacks("/* .performChanges { x }\n   .sendMessage { y } */\nlet a = 1"), [])

    def test_thread_literals(self):
        self.assertEqual(thread_literals("let t = Thread { loop() }"), [1])
        self.assertEqual(thread_literals("let t = Thread(block: { loop() })"), [1])
        self.assertEqual(thread_literals("Thread.detachNewThread { loop() }"), [1])
        self.assertEqual(thread_literals("let body: @Sendable () -> Void = { loop() }\nlet t = Thread(block: body)"), [])
        self.assertEqual(thread_literals("func make() -> Thread { Thread(block: body) }"), [])
        self.assertEqual(thread_literals("if Thread.isMainThread { work() }"), [])
        self.assertEqual(thread_literals("// Thread { loop() }"), [])


if __name__ == "__main__":
    unittest.main()
