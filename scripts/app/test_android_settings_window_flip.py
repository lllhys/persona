#!/usr/bin/env python3
"""Unit tests for the checked device commands and bounded cleanup in the Settings window regression."""
import io
import subprocess
import unittest
from contextlib import redirect_stdout
from unittest import mock

import android_settings_window_flip as flip


class DeviceCommandTest(unittest.TestCase):
    @mock.patch("android_settings_window_flip.subprocess.run")
    def test_a_nonzero_adb_status_is_a_failure(self, run):
        run.return_value = subprocess.CompletedProcess(["adb", "shell", "wm", "size", "reset"], 1, "", "denied")
        with self.assertRaises(RuntimeError) as caught:
            flip.Device("emulator-5604").run("shell", "wm", "size", "reset")
        self.assertIn("denied", str(caught.exception))

    @mock.patch("android_settings_window_flip.subprocess.run")
    def test_an_adb_timeout_has_a_timeout_verdict_type(self, run):
        run.side_effect = subprocess.TimeoutExpired(["adb", "shell", "wm", "size"], 30)
        with self.assertRaises(Exception) as caught:
            flip.Device("emulator-5604").run("shell", "wm", "size")
        self.assertIsInstance(caught.exception, RuntimeError)
        self.assertIn("timed out", str(caught.exception))


class DisplayReadbackTest(unittest.TestCase):
    def setUp(self):
        self.verify = getattr(flip, "verify_display", None)
        self.assertTrue(callable(self.verify), "the display readback check is missing")
        self.device = mock.Mock()
        self.device.shell.side_effect = [
            "Physical size: 2560x1600\nOverride size: 1200x900\n",
            "Physical density: 320\nOverride density: 160\n",
        ]

    def test_the_actual_override_size_and_density_can_match(self):
        self.verify(self.device, "1200x900", 160)

    def test_a_size_mismatch_is_a_failure(self):
        with self.assertRaises(RuntimeError):
            self.verify(self.device, "900x1200", 160)


class CleanupTest(unittest.TestCase):
    def test_an_unresponsive_mock_is_killed_and_the_timeout_is_reported(self):
        stop = getattr(flip, "stop_mock", None)
        self.assertTrue(callable(stop), "the bounded mock stop is missing")
        process = mock.Mock()
        process.poll.return_value = None
        process.wait.side_effect = [subprocess.TimeoutExpired("mock", 10), 0]
        with self.assertRaises(RuntimeError) as caught:
            stop(process)
        process.kill.assert_called_once_with()
        self.assertIn("mock hub", str(caught.exception))


class VerdictTest(unittest.TestCase):
    def setUp(self):
        self.execute = getattr(flip, "execute", None)
        self.assertTrue(callable(self.execute), "the testable execution entry is missing")
        self.args = ["-s", "emulator-5604", "--mock-port", "19001"]

    def verdict(self, effect):
        output = io.StringIO()
        behavior = {"side_effect": effect} if isinstance(effect, Exception) else {"return_value": effect}
        with mock.patch("android_settings_window_flip.execute", **behavior):
            with redirect_stdout(output):
                status = flip.main(self.args)
        return status, output.getvalue()

    def test_success_has_one_pass_line_and_a_zero_status(self):
        status, output = self.verdict("0 of 1 rounds crashed")
        self.assertEqual(0, status)
        self.assertEqual("PASS: 0 of 1 rounds crashed\n", output)

    def test_a_command_failure_has_one_fail_line_and_a_nonzero_status(self):
        status, output = self.verdict(flip.FlipFailure("wm failed"))
        self.assertNotEqual(0, status)
        self.assertEqual("FAIL: wm failed\n", output)

    def test_a_timeout_has_one_timeout_line_and_a_nonzero_status(self):
        status, output = self.verdict(flip.FlipTimeout("adb timed out"))
        self.assertNotEqual(0, status)
        self.assertEqual("TIMEOUT: adb timed out\n", output)


if __name__ == "__main__":
    unittest.main()
