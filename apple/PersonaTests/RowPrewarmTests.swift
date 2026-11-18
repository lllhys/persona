import Foundation
import PersonaKit
import SwiftUI
import Testing
@testable import Persona

/// A chat's row prewarming can't take a background recording down (#9108). iOS killed a recording's app for 100 % CPU
/// spent in `ChatView.prewarmRows` → `RowMemo.prewarm` → `UserRowText` → `FilePaths.find` → `PathRefs.find`, on a user
/// message with a relative path whose last part was a hash (the pattern before #8911 tried it every way).
struct RowPrewarmTests {
    /// The shape of the message that spun: a relative path ending in a commit hash, no extension.
    private static let hung = "看一下 bucket/2024/1/1/0123456789abcdef0123456789abcdef01234567 这个对象，"
        + "还有 owner/repo@0123456789abcdef0123456789abcdef01234567"

    /// The row's parse, the chain the crash's heaviest stack went through, ends at once, and still finds real paths.
    @Test func aUserRowWithAHashAfterAFolderIsParsedAtOnce() {
        let text = (0..<60).map { "[0:\(String(format: "%02d", $0))] " + Self.hung }.joined(separator: "\n")
            + "\n改动在 apple/Persona/Recording/MeetingRecorder.swift:139"
        let started = ContinuousClock.now
        let row = UserRowText(text)
        let found = FilePaths.find(in: text)
        #expect(ContinuousClock.now - started < .milliseconds(500))
        #expect(row.pathRefs.contains { $0.path == "apple/Persona/Recording/MeetingRecorder.swift" })
        #expect(found == ["apple/Persona/Recording/MeetingRecorder.swift"])
    }

    /// Rows are prewarmed in front and while the scene is inactive (a banner, Control Center), never in the background,
    /// where a recording may be what keeps the app running.
    @Test func noPrewarmingInTheBackground() {
        #expect(RowMemo.prewarms(in: .active))
        #expect(RowMemo.prewarms(in: .inactive))
        #expect(!RowMemo.prewarms(in: .background))
    }

    /// Prewarming a chat with that message in it ends at once too (it went on for minutes before).
    @Test func prewarmingAChatWithTheMessageEnds() async {
        let items = (0..<40).map { n in
            Item(id: "\(n)#0", seq: n, ts: nil, role: "user", kind: .text,
                 text: n % 4 == 0 ? Self.hung : "第 \(n) 条：看 docs/apple/recording.md", toolUseID: nil, toolName: nil,
                 toolInput: nil, isError: nil, image: nil)
        }
        let started = ContinuousClock.now
        await Task.detached(priority: .utility) { RowMemo.prewarm(items, scope: "test/rows-\(UUID().uuidString)") }.value
        #expect(ContinuousClock.now - started < .seconds(1))
    }
}
