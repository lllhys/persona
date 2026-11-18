#!/usr/bin/env python3
"""Code that moved out of apple/Shared into a Swift package does not flow back (#7891, stage 1; #7895).

Each group of files that moves adds its names here: the file may not reappear in apple/Shared, and no Swift file
outside the package may define the moved types again. There is no count of the folder's files or lines.

  python3 scripts/test/test_apple_shared_loose.py
"""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[2]
APPLE = ROOT / "apple"

# package -> (files that left apple/Shared, directories that left it, types the package now owns)
MOVED = {
    "PersonaWire": (
        ["JSONValue.swift", "AppClock.swift", "AssistantCardText.swift", "AssistantViewBlocks.swift", "BrowserCookies.swift", "ChatUserInputIndex.swift", "CloudflareAccess.swift", "ExtensionActionValues.swift", "ExtensionOperation.swift", "RetryIDs.swift", "SecretReference.swift", "TaskBrief.swift", "TextMemo.swift", "UsageWindows.swift", "TokenUsage.swift", "ModelID.swift", "Closeout.swift", "ExtensionAuthState.swift", "ExtensionStreamJSON.swift", "HubTransport.swift", "Keychain.swift", "MachineMetrics.swift", "StateCache.swift", "HubFanOut.swift"],
        ["Generated"],
        ["JSONValue", "JSON", "AppClock", "AssistantCardText", "AssistantViewBlocks", "BrowserCookie", "BrowserCookies", "BrowserCookieSite", "BrowserSiteList", "ChatUserInputIndex", "CloudflareAccess", "CloudflareAccessError", "CookieFile", "CookieFileSite", "ExtensionActionInputError", "ExtensionActionValues", "ExtensionOperation", "ExtensionOperationStep", "HubAccess", "HubRedirects", "MachineUsageSeries", "MemoScope", "ModelID", "RetryIDs", "SecretReference", "TaskBrief", "TextMemo", "TokenFormat", "UsageAccount", "UsageCard", "UsageKind", "UsagePoint", "UsageRange", "UsageShare", "UsageTokens", "UsageWindow", "UsageWindows", "ClaimState", "Closeout", "Debouncer", "ExtensionAuth", "ExtensionStreamSchema", "HubFanOut", "HubTransport", "InAppTunnelRoute", "Keychain", "KeychainAccess", "LocalProxyRoute", "MachineMetrics", "MachineSystem", "MetricAlert", "MetricFormat", "MetricLevel", "MetricPoint", "MetricRange", "MetricSample", "OSFamily", "SSHRoute", "StateCache", "Throttle", "WorkArtifact"],
    ),
}


def swift_files():
    for f in APPLE.rglob("*.swift"):
        parts = f.relative_to(APPLE).parts
        if parts[0] != "Packages" and "build" not in parts:  # other packages are separate modules
            yield f


class MovedCodeStaysMoved(unittest.TestCase):
    def test_files_do_not_reappear_in_shared(self):
        for package, (files, dirs, _) in MOVED.items():
            for name in files:
                self.assertFalse((APPLE / "Shared" / name).exists(), f"{name} lives in {package}")
            for name in dirs:
                self.assertFalse((APPLE / "Shared" / name).exists(), f"{name}/ lives in {package}")

    def test_types_are_not_defined_again(self):
        for package, (_, _, types) in MOVED.items():
            for f in swift_files():
                text = f.read_text(encoding="utf-8")
                for t in types:
                    found = re.search(rf"^\s*(?:\w+\s+)*(?:enum|struct|class|actor)\s+{t}\b", text, re.M)
                    self.assertIsNone(found, f"{f.relative_to(ROOT)} defines {t}, which {package} owns")


if __name__ == "__main__":
    unittest.main()
