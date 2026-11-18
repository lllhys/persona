#!/usr/bin/env python3
"""Cross the regular-window boundary and back in quick succession while the Settings sheet is open, in one process.

The Settings sheet's pages swap between the list-detail scene and the single-page scene when a window narrows below the
regular-window width. Done twice within a fraction of a second (a free window's edge dragged across the boundary, or a
display's size and density changed one after the other), that swap crashed Compose's runtime with a movable-content
exception while the sheet's dialog window was open (docs/lessons/android.md). Each round launches the debug build
against a mock hub, opens Settings (and a page when --page), changes the display size to a compact window and its density
to a different regular one right after, and passes only when the same process is still there with no fatal exception.

    scripts/app/android_settings_window_flip.py -s emulator-5604 --mock-port 19001 --rounds 8 --page --out /tmp/flip

Needs a tablet or open-foldable display (at least 600dp wide) and a free local port for the mock hub. Restores the
physical display and stops its mock hub. The samples are few: report the round count with the result, never a rate.
Every display change is read back; an ADB failure, a display mismatch or a cleanup timeout cannot produce a PASS.
"""
import argparse
import os
import re
import signal
import subprocess
import sys
import time
import xml.etree.ElementTree as ET
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
APP = "com.example.persona.shots"
ENTRY = APP + "/persona.android.debug.DebugEntryActivity"
SESSION = "server/b2e4d6f8-0000-4000-8000-000000000001"


class FlipFailure(RuntimeError):
    """A command, display assertion or round failed."""


class FlipTimeout(RuntimeError):
    """A device command or process cleanup exceeded its bound."""


class Device:
    def __init__(self, serial):
        self.serial = serial
        self.adb = os.path.join(os.environ.get("ANDROID_HOME", os.path.expanduser("~/Library/Android/sdk")), "platform-tools", "adb")

    def run(self, *args, timeout=30, allowed=(0,)):
        command = [self.adb, "-s", self.serial, *args]
        try:
            done = subprocess.run(command, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired as error:
            raise FlipTimeout("adb timed out after %ss: %s" % (timeout, " ".join(args))) from error
        if done.returncode not in allowed:
            detail = (done.stderr or done.stdout).strip()
            raise FlipFailure("adb returned %d for %s%s" % (done.returncode, " ".join(args), ": " + detail if detail else ""))
        return done.stdout

    def shell(self, *args, timeout=30):
        return self.run("shell", *args, timeout=timeout)

    def pid(self):
        # pidof uses status 1 specifically for a missing process; the round turns that into a crash verdict.
        return self.run("shell", "pidof", APP, allowed=(0, 1)).strip()

    def fatal_count(self):
        return self.run("logcat", "-d").count("FATAL EXCEPTION")

    def find(self, **match):
        """The center of the first node whose attributes equal [match], from a fresh uiautomator dump."""
        for _ in range(5):
            self.shell("uiautomator", "dump", "/sdcard/flip-window.xml")
            xml = self.run("exec-out", "cat", "/sdcard/flip-window.xml")
            try:
                tree = ET.fromstring(xml)
            except ET.ParseError:
                time.sleep(1)
                continue
            for node in tree.iter("node"):
                if all(node.get(key.replace("_", "-")) == value for key, value in match.items()):
                    left, top, right, bottom = map(int, re.findall(r"\d+", node.get("bounds")))
                    return (left + right) // 2, (top + bottom) // 2
            time.sleep(1)
        raise FlipFailure("no node %r on screen" % (match,))

    def tap(self, **match):
        x, y = self.find(**match)
        self.shell("input", "tap", str(x), str(y))


def parse_display(size_output, density_output, physical=False):
    """Read physical values, or the active override when one is present."""
    physical_size = re.search(r"Physical size: (\d+)x(\d+)", size_output)
    physical_density = re.search(r"Physical density: (\d+)", density_output)
    if physical_size is None or physical_density is None:
        raise FlipFailure("cannot read the physical display size and density")
    override_size = None if physical else re.search(r"Override size: (\d+)x(\d+)", size_output)
    override_density = None if physical else re.search(r"Override density: (\d+)", density_output)
    size = override_size or physical_size
    density = override_density or physical_density
    return int(size.group(1)), int(size.group(2)), int(density.group(1))


def display(device):
    return parse_display(device.shell("wm", "size"), device.shell("wm", "density"), physical=True)


def verify_display(device, size, density):
    expected = re.fullmatch(r"(\d+)x(\d+)", size)
    if expected is None:
        raise FlipFailure("invalid expected display size %r" % size)
    actual = parse_display(device.shell("wm", "size"), device.shell("wm", "density"))
    wanted = (int(expected.group(1)), int(expected.group(2)), density)
    if actual != wanted:
        raise FlipFailure("display readback is %dx%d at %ddpi, expected %dx%d at %ddpi" % (*actual, *wanted))


def set_display(device, size, density):
    # One device shell keeps the compact-to-regular sequence tight; && cannot hide an earlier wm failure.
    device.shell("wm size %s && wm density %d" % (size, density))
    verify_display(device, size, density)


def reset_display(device, size, density):
    device.shell("wm size reset && wm density reset")
    verify_display(device, size, density)


def stop_mock(mock):
    """Stop only the child started by this run, with a second bound after an ignored SIGTERM."""
    if mock is None or mock.poll() is not None:
        return
    mock.send_signal(signal.SIGTERM)
    try:
        mock.wait(timeout=10)
        return
    except subprocess.TimeoutExpired:
        pass
    try:
        mock.kill()
        mock.wait(timeout=5)
    except subprocess.TimeoutExpired as error:
        raise FlipTimeout("mock hub did not stop after SIGTERM and kill") from error
    raise FlipTimeout("mock hub ignored SIGTERM and was killed")


def cleanup(device, mock, mock_log, port, reverse_added, size, density):
    """Attempt every owned cleanup even when an earlier one fails, then report all failures together."""
    errors = []
    timed_out = False

    def attempt(action):
        nonlocal timed_out
        try:
            action()
        except Exception as error:
            timed_out = timed_out or isinstance(error, FlipTimeout)
            errors.append(str(error))

    attempt(lambda: reset_display(device, size, density))
    if reverse_added:
        attempt(lambda: device.run("reverse", "--remove", "tcp:%d" % port))
    if mock is not None:
        attempt(lambda: stop_mock(mock))
    if mock_log is not None:
        attempt(mock_log.close)
    if errors:
        message = "; ".join(errors)
        if timed_out:
            raise FlipTimeout(message)
        raise FlipFailure(message)


def execute(args):
    if not args.serial.startswith("emulator-"):
        raise FlipFailure("use a dedicated emulator (-s emulator-NNNN)")
    if args.rounds < 1 or args.flips < 1:
        raise FlipFailure("rounds and flips must both be at least one")
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    device = Device(args.serial)
    width, height, density = display(device)
    if density <= 0 or width * 160 / density < 600:
        raise FlipFailure("this display is narrower than 600dp: nothing to cross")
    # A display that halves its pixels and then its density: 1280x800dp -> 600x450dp (compact) -> 1200x900dp, as `wm size`
    # then `wm density` do on a 2560x1600 tablet at 320dpi.
    narrow = "%dx%d" % (width * 15 // 32, height * 9 // 16)
    original = "%dx%d" % (width, height)
    wide_density = density // 2
    if wide_density < 1:
        raise FlipFailure("the display density is too low for the size-and-density sequence")
    mock = None
    mock_log = None
    reverse_added = False
    failures = 0
    primary = None
    cleanup_failure = None
    page = "Models page open" if args.page else "Settings list"

    def summary():
        return "%d of %d rounds crashed (%d size and density changes each, %s)" % (failures, args.rounds, args.flips, page)

    try:
        mock_log = (out / "mock-hub.log").open("w")
        mock = subprocess.Popen([sys.executable, "-u", str(ROOT / "scripts/mock/mock_hub.py")], env={**os.environ, "PORT": str(args.mock_port)},
                                stdout=mock_log, stderr=subprocess.STDOUT)
        # Attempt removal even if the command times out after the device accepted the rule.
        reverse_added = True
        device.run("reverse", "tcp:%d" % args.mock_port, "tcp:%d" % args.mock_port)
        time.sleep(3)
        for number in range(1, args.rounds + 1):
            reset_display(device, original, density)
            device.shell("am", "force-stop", APP)
            device.shell("pm", "clear", APP)
            device.run("logcat", "-c")
            device.shell("am", "start", "-W", "-n", ENTRY, "--es", "debug.mockAddress", "http://127.0.0.1:%d" % args.mock_port,
                         "--es", "debug.openSession", SESSION, "--es", "debug.conversationKind", "assistant",
                         "--ez", "debug.automation", "true", "--es", "debug.style", "glass", "--es", "debug.theme", "light")
            device.tap(content_desc="Settings")
            time.sleep(3)
            if args.page:
                device.tap(text="Models")
                time.sleep(3)
            before = device.pid()
            for _ in range(args.flips):
                set_display(device, narrow, wide_density)
                time.sleep(1)
                reset_display(device, original, density)
                time.sleep(1)
            time.sleep(4)
            after = device.pid()
            fatal = device.fatal_count()
            verdict = "ok" if before and before == after and fatal == 0 else "CRASH"
            if verdict != "ok":
                failures += 1
                (out / ("round-%d.log" % number)).write_text(device.run("logcat", "-d"))
            print("round %d: pid %s -> %s fatal=%d %s" % (number, before, after, fatal, verdict), flush=True)
        if failures:
            primary = FlipFailure(summary())
    except Exception as error:
        primary = error
    finally:
        try:
            cleanup(device, mock, mock_log, args.mock_port, reverse_added, original, density)
        except Exception as error:
            cleanup_failure = error

    if primary is not None and cleanup_failure is not None:
        message = "%s; cleanup also failed: %s" % (primary, cleanup_failure)
        if isinstance(primary, FlipTimeout) or isinstance(cleanup_failure, FlipTimeout):
            raise FlipTimeout(message)
        raise FlipFailure(message)
    if cleanup_failure is not None:
        raise cleanup_failure
    if primary is not None:
        raise primary
    return summary()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("-s", "--serial", required=True, help="the emulator to use (emulator-NNNN)")
    parser.add_argument("--mock-port", type=int, required=True, help="a free local port for the mock hub")
    parser.add_argument("--rounds", type=int, default=8)
    parser.add_argument("--page", action="store_true", help="open the Models page inside Settings before the flips")
    parser.add_argument("--flips", type=int, default=1, help="how many size-and-density changes per round")
    parser.add_argument("--out", default="/tmp/settings-window-flip")
    args = parser.parse_args(argv)
    try:
        result = execute(args)
    except FlipTimeout as error:
        print("TIMEOUT: %s" % " ".join(str(error).splitlines()), flush=True)
        return 1
    except Exception as error:
        print("FAIL: %s" % " ".join(str(error).splitlines()), flush=True)
        return 1
    print("PASS: %s" % result, flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())
