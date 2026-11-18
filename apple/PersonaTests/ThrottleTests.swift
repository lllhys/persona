import Foundation
import Testing
@testable import Persona
import PersonaWire

/// `Throttle` (#4684): the session snapshot is written once after a quiet spell, then at most once per gap, and at once
/// when the app leaves.
@MainActor struct ThrottleTests {
    @MainActor final class Log {
        var written: [Int] = []
    }

    @Test func aBurstWritesOnceWithTheLatestWork() async throws {
        let throttle = Throttle(first: .milliseconds(20), gap: .seconds(10))
        let log = Log()
        for i in 1...5 { throttle.run { log.written.append(i) } }
        try await Task.sleep(for: .milliseconds(200))
        #expect(log.written == [5])
    }

    @Test func aChangeAfterAWriteWaitsForTheGap() async throws {
        let throttle = Throttle(first: .milliseconds(20), gap: .milliseconds(600))
        let log = Log()
        throttle.run { log.written.append(1) }
        try await Task.sleep(for: .milliseconds(150))
        #expect(log.written == [1])
        throttle.run { log.written.append(2) }
        try await Task.sleep(for: .milliseconds(150))
        #expect(log.written == [1]) // still inside the gap
        try await Task.sleep(for: .milliseconds(700))
        #expect(log.written == [1, 2])
    }

    @Test func flushWritesWhatWaitsAtOnce() async throws {
        let throttle = Throttle(first: .seconds(10), gap: .seconds(10))
        let log = Log()
        throttle.flush() // nothing waits: nothing to write
        throttle.run { log.written.append(1) }
        throttle.flush()
        #expect(log.written == [1])
        try await Task.sleep(for: .milliseconds(50))
        #expect(log.written == [1]) // the cancelled wait writes nothing more
    }
}
