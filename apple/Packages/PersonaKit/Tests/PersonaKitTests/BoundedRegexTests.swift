import Foundation
import Synchronization
import Testing
@testable import PersonaKit

/// Matching that stops at a deadline (#9108): a pattern that backtracks for ever on some text no longer keeps a thread at
/// full CPU, and with it a recording's app in the background, until iOS kills it.
struct BoundedRegexTests {
    /// `PathRefs`'s text pattern as it was before #8911: two alternatives of `seg` both take ASCII letters and digits, so
    /// a long extensionless last part is tried every way. It is what spun in the recording's app (#9108).
    private static let backtracking: NSRegularExpression = {
        let seg = #"(?:[A-Za-z0-9_.@+\-]|(?<!\.[A-Za-z][A-Za-z0-9]{0,7})[\p{L}\p{M}\p{N}])+"#
        let line = #"(?::(\d{1,7})(?::(\d{1,5}))?(?:[-–]\d{1,7})?|#L(\d{1,7})(?:-L?\d{1,7})?)?"#
        return try! NSRegularExpression(pattern:
            #"(?<![A-Za-z0-9_/:.\-~])(~(?:/"# + seg + #")+/?|/"# + seg + "(?:/" + seg + #")+/?|[A-Za-z0-9_.@\-]+(?:/"# + seg
            + #")+\.[A-Za-z][A-Za-z0-9]{0,7})"# + line + #"(?![A-Za-z0-9_/\-])"#)
    }()

    /// A user message of a chat, as the one that hung: a relative path whose last part is a commit hash.
    private static let hung = "[0:15] 我们看一下 bucket/2024/1/1/0123456789abcdef0123456789abcdef01234567 这个对象"

    /// `work` on a thread of its own, or nil when it is still running after `seconds`: a deadline that doesn't hold fails
    /// the test instead of hanging the run (the thread is left to spin until the process ends).
    private static func within<T: Sendable>(_ seconds: Double, _ work: @escaping @Sendable () -> T) -> T? {
        let done = DispatchSemaphore(value: 0)
        let result = Mutex<T?>(nil)
        Thread.detachNewThread {
            let value = work()
            result.withLock { $0 = value }
            done.signal()
        }
        guard done.wait(timeout: .now() + seconds) == .success else { return nil }
        return result.withLock { $0 }
    }

    @Test func stopsAPatternThatBacktracksForeverAtItsDeadline() {
        // Unbounded, 22 characters of hash took 403 s (#8911); this one has 40.
        let bounded = Self.within(5) {
            let started = ContinuousClock.now
            let all = BoundedRegex.matches(Self.backtracking, in: Self.hung, until: BoundedRegex.deadline(after: .milliseconds(100)))
            let first = BoundedRegex.firstMatch(Self.backtracking, in: Self.hung, until: BoundedRegex.deadline(after: .milliseconds(100)))
            return (finished: all.finished, found: all.matches.count, first: first != nil, took: ContinuousClock.now - started)
        }
        guard let bounded else {
            Issue.record("the deadline did not stop the match")
            return
        }
        #expect(!bounded.finished)
        #expect(bounded.found == 0)
        #expect(!bounded.first)
        #expect(bounded.took < .seconds(1))
    }

    @Test func findsWhatUnboundedMatchingFinds() throws {
        let re = try NSRegularExpression(pattern: #"[a-z]+/[a-z]+\.md"#)
        let text = "读 notes/a.md 和 docs/b.md，再看 x/y.md"
        let all = re.matches(in: text, range: NSRange(location: 0, length: (text as NSString).length)).map(\.range)
        let bounded = BoundedRegex.matches(re, in: text, until: BoundedRegex.deadline())
        #expect(bounded.finished)
        #expect(bounded.matches.map(\.range) == all)
        #expect(all.count == 3)
        #expect(BoundedRegex.firstMatch(re, in: text, until: BoundedRegex.deadline())?.range == all.first)
        // A deadline already past matches nothing and says so.
        let late = BoundedRegex.matches(re, in: text, until: .now - .milliseconds(1))
        #expect(!late.finished && late.matches.isEmpty)
    }

    /// The paths of a long meeting batch (the shape the hub feeds a meeting's room) with hashes, ids and paths in it come
    /// at once, and the paths are still found.
    @Test func aLongMeetingRowWithHashesFindsItsPathsAtOnce() {
        var lines: [String] = []
        for i in 0..<120 {
            lines.append("[\(i / 60):\(String(format: "%02d", i % 60))] 先看 bucket/2024/1/\(i)/0123456789abcdef0123456789abcdef01234567，"
                + "还有 /0f1e2d3c-4b5a-4978-8796-a5b4c3d2e1f0 和 owner/repo@0123456789abcdef0123456789abcdef")
        }
        lines.append("改动在 apple/Persona/Recording/MeetingRecorder.swift:139 和 ~/报告/第四季度.docx")
        let text = "<persona-context kind=\"context\" title=\"Weekly · live\" url=\"\">\n" + lines.joined(separator: "\n") + "\n</persona-context>"
        let started = ContinuousClock.now
        let found = PathRefs.find(in: text, limit: 400).map(\.path)
        let took = ContinuousClock.now - started
        #expect(took < .milliseconds(500))
        #expect(found.contains("apple/Persona/Recording/MeetingRecorder.swift"))
        #expect(found.contains("~/报告/第四季度.docx"))
        #expect(!found.contains { $0.hasSuffix("0123456789abcdef0123456789abcdef01234567") })
        let markdown = PathMarkdown(text)
        #expect(markdown.paths.contains("apple/Persona/Recording/MeetingRecorder.swift"))
        #expect(ContinuousClock.now - started < .seconds(1))
    }
}
