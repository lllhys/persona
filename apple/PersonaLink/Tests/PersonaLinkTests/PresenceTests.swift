import Foundation
import UserNotifications
import XCTest
@testable import PersonaLink

/// Presence for the helper's stdin: when the user counts as at this Mac, which lines go out and when, and that a helper
/// that stopped reading or went away never blocks or kills the app.
final class PresenceTests: XCTestCase {
    private let limit = PresenceTracker.idleLimit
    private let every = PresenceTracker.repeatEvery

    /// Unlocked, screens awake, this session on screen, notifications shown.
    private func here() -> PresenceTracker {
        var t = PresenceTracker()
        t.apply(.notifies(true))
        return t
    }

    private let active = Presence(active: true, notifies: true)

    // MARK: who counts as here

    func testEverySignalTurnsItOffAndBackOn() {
        XCTAssertEqual(PresenceTracker().presence(idle: 0), Presence(active: false, notifies: false),
                       "notifications count as not shown until LinkNotifier says they are")
        var t = here()
        XCTAssertEqual(t.check(idle: 0, now: 0).write, active)
        let pairs: [(PresenceTracker.Signal, PresenceTracker.Signal)] = [
            (.locked(true), .locked(false)), (.screensAsleep(true), .screensAsleep(false)),
            (.sessionActive(false), .sessionActive(true)),
        ]
        var now: TimeInterval = 0
        for (off, on) in pairs {
            now += 1
            t.apply(off)
            XCTAssertEqual(t.check(idle: 0, now: now).write, Presence(active: false, notifies: true), "\(off)")
            now += 1
            t.apply(on)
            XCTAssertEqual(t.check(idle: 0, now: now).write, active, "\(on)")
        }
        t.apply(.notifies(false))
        XCTAssertEqual(t.check(idle: 0, now: 10).write, Presence(active: false, notifies: false))
        t.apply(.notifies(true))
        XCTAssertEqual(t.check(idle: 0, now: 11).write, active)
        XCTAssertNil(t.check(idle: limit - 1, now: 12).write, "still active just under the idle limit")
        XCTAssertEqual(t.check(idle: limit, now: 13).write, Presence(active: false, notifies: true), "idle")
        XCTAssertEqual(t.check(idle: 2, now: 14).write, active, "input again")
    }

    func testApplySaysWhetherAnythingChanged() {
        var t = here()
        XCTAssertFalse(t.apply(.notifies(true)))
        XCTAssertTrue(t.apply(.locked(true)))
        XCTAssertFalse(t.apply(.locked(true)))
    }

    // MARK: which lines go out, and when

    func testWritesChangesAtOnceAndRepeatsOnlyWhileActive() {
        var t = here()
        XCTAssertEqual(t.check(idle: 0, now: 100).write, active)
        XCTAssertNil(t.check(idle: 30, now: 130).write, "unchanged, not yet due")
        XCTAssertNil(t.check(idle: 59, now: 100 + every - 1).write)
        XCTAssertEqual(t.check(idle: 0, now: 100 + every).write, active, "repeated while active")
        t.apply(.locked(true))
        XCTAssertEqual(t.check(idle: 0, now: 170).write, Presence(active: false, notifies: true),
                       "a change goes out before the repeat is due")
        XCTAssertNil(t.check(idle: 0, now: 170 + every * 10).write, "inactive is said once")
        t.apply(.notifies(false))
        XCTAssertEqual(t.check(idle: 0, now: 171).write, Presence(active: false, notifies: false),
                       "notifies changing alone is a change")
    }

    func testNextCheckLandsOnTheIdleLimitOrTheRepeat() {
        var t = here()
        XCTAssertEqual(t.check(idle: 0, now: 0).next, every, "the repeat comes first")
        XCTAssertEqual(t.check(idle: limit - 20, now: 10).next, 20, "the idle limit comes first")
        XCTAssertEqual(t.check(idle: 0, now: 45).next, every - 45, "the repeat counts from the last line")
        XCTAssertEqual(t.check(idle: limit - 0.2, now: 46).next, PresenceTracker.minimumWait, "never a busy loop")
        XCTAssertEqual(t.check(idle: limit, now: 47).next, PresenceTracker.pollEvery, "idle: look for input again")
        t.apply(.notifies(false))
        XCTAssertEqual(t.check(idle: 0, now: 48).next, every, "no notifications: input does not matter, the permission might")
        for signal: PresenceTracker.Signal in [.locked(true), .screensAsleep(true), .sessionActive(false)] {
            var away = here()
            away.apply(signal)
            XCTAssertNil(away.check(idle: 0, now: 0).next, "\(signal): the next signal says when it ends")
        }
    }

    func testARestartedHelperGetsTheStateAtOnce() {
        var t = here()
        _ = t.check(idle: 0, now: 0)
        t.helperStarted()
        XCTAssertEqual(t.check(idle: 0, now: 1).write, active)
        t.apply(.locked(true))
        _ = t.check(idle: 0, now: 2)
        t.helperStarted()
        XCTAssertEqual(t.check(idle: 0, now: 3).write, Presence(active: false, notifies: true),
                       "inactive too, once per helper")
        XCTAssertNil(t.check(idle: 0, now: 4).write)
    }

    func testLineIsTheDocumentedJSON() {
        XCTAssertEqual(String(decoding: Presence(active: true, notifies: true).line, as: UTF8.self),
                       #"{"presence":{"active":true,"notifies":true}}"# + "\n")
        XCTAssertEqual(String(decoding: Presence(active: false, notifies: false).line, as: UTF8.self),
                       #"{"presence":{"active":false,"notifies":false}}"# + "\n")
    }

    // MARK: notification permission

    func testNotificationsShowOnlyWhenAuthorizedAsBannersOrAlerts() {
        XCTAssertTrue(NotificationAccess(status: .authorized, style: .banner).shows)
        XCTAssertTrue(NotificationAccess(status: .authorized, style: .alert).shows)
        XCTAssertFalse(NotificationAccess(status: .authorized, style: .none).shows, "Notification Center only")
        XCTAssertFalse(NotificationAccess(status: .provisional, style: .banner).shows, "delivered quietly")
        XCTAssertFalse(NotificationAccess(status: .notDetermined, style: .none).shows, "not asked yet")
        XCTAssertTrue(NotificationAccess(status: .notDetermined, style: .none).allowed, "posts still go out")
        XCTAssertFalse(NotificationAccess(status: .denied, style: .banner).shows)
        XCTAssertFalse(NotificationAccess(status: .denied, style: .banner).allowed)
    }

    // MARK: writing to the helper's stdin

    func testAFullPipeFailsTheWriteInsteadOfBlocking() throws {
        let pipe = Pipe()
        LinkModel.dontBlock(pipe.fileHandleForWriting)
        let line = Presence(active: true, notifies: true).line
        var written = 0
        var failure: NSError?
        while failure == nil && written < 100_000 { // a pipe holds 64 KiB: about 1,500 lines
            do {
                try LinkModel.writeAll(pipe.fileHandleForWriting, line)
                written += 1
            } catch {
                failure = error as NSError
            }
        }
        XCTAssertGreaterThan(written, 0)
        XCTAssertEqual(failure?.domain, NSPOSIXErrorDomain)
        XCTAssertEqual(failure?.code, Int(EAGAIN))
        // Nothing half-written: whatever the pipe holds is whole lines.
        try pipe.fileHandleForWriting.close()
        let held = pipe.fileHandleForReading.readDataToEndOfFile()
        XCTAssertEqual(held.count, written * line.count)
    }

    func testWritingToAHelperThatWentAwayThrows() throws {
        let pipe = Pipe()
        LinkModel.dontBlock(pipe.fileHandleForWriting)
        try pipe.fileHandleForReading.close()
        XCTAssertThrowsError(try LinkModel.writeAll(pipe.fileHandleForWriting, Presence(active: true, notifies: true).line)) {
            XCTAssertEqual(($0 as NSError).code, Int(EPIPE), "a broken pipe is an error, not SIGPIPE")
        }
    }
}
