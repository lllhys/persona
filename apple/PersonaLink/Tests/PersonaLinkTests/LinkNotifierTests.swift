import Foundation
import UserNotifications
import XCTest
@testable import PersonaLink

/// A notification center that keeps what it was handed; `hold` makes the next permission checks wait for `release`,
/// `failAdd` refuses every notification, `access` is what System Settings allows.
@MainActor private final class FakeCenter: NotificationCenterLike {
    var added: [String] = []
    var failAdd = false
    var access = NotificationAccess(status: .authorized, style: .banner)
    var hold = false
    private(set) var waiting: [CheckedContinuation<Void, Never>] = []
    /// holdAdd: the next notifications wait in add until release (Notification Center slow to take them).
    var holdAdd = false
    private(set) var adding: [CheckedContinuation<Void, Never>] = []

    func attach(_ delegate: UNUserNotificationCenterDelegate) {}

    func add(_ note: AttentionNote) async throws {
        if holdAdd { await withCheckedContinuation { adding.append($0) } }
        if failAdd { throw CocoaError(.featureUnsupported) }
        added.append(note.id)
    }

    func remove(_ ids: [String]) {}
    func delivered() async -> [(id: String, card: String?)] { [] }

    func access() async -> NotificationAccess {
        if hold { await withCheckedContinuation { waiting.append($0) } }
        return access
    }

    func requestAuthorization() async throws {}

    func release() {
        hold = false
        holdAdd = false
        let w = waiting + adding
        waiting = []
        adding = []
        w.forEach { $0.resume() }
    }
}

/// The menu bar app confirms a card only once it handed the card's notification to Notification Center, on the
/// connection it was for: the hub then leaves that card to this Mac. A post dropped while it waited (the card left a
/// capped summary, the connection went), one taken back as soon as it went out (its session waits on a newer card),
/// one Notification Center refused, or one with notifications off confirms nothing, and the phone gets the card.
@MainActor
final class LinkNotifierTests: XCTestCase {
    private func item(_ session: String, card: String) -> LinkAttention.Item {
        LinkAttention.Item(machineID: "laptop", sessionID: session, kind: "permission", muted: false, notify: true,
                           interactionID: card, route: "/s/laptop/\(session)")
    }

    private func summary(_ items: LinkAttention.Item..., more: Int = 0) -> LinkAttention {
        LinkAttention(needsYou: items.count + more, more: more, items: items)
    }

    /// A notifier on center whose confirmations are kept as "card@conn", past its first summary (the baseline, which
    /// posts nothing) from the helper's link conn.
    private func started(_ center: FakeCenter, conn: UInt64? = 7) async -> (LinkNotifier, () -> [String]) {
        let notifier = LinkNotifier(center: center)
        var confirmed: [String] = []
        notifier.start(open: { _, _ in }, access: { _ in }, confirm: { confirmed.append("\($0)@\($1)") })
        notifier.apply(summary(), conn: conn)
        await notifier.settled()
        return (notifier, { confirmed })
    }

    /// Waits until the work queued holds on the permission check.
    private func untilHeld(_ center: FakeCenter) async {
        for _ in 0..<1000 where center.waiting.isEmpty { await Task.yield() }
        XCTAssertFalse(center.waiting.isEmpty, "the post never reached the permission check")
    }

    func testConfirmsACardOnceItsNotificationIsHandedOver() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        await notifier.settled()
        XCTAssertEqual(center.added, ["laptop/s1#needs"])
        XCTAssertEqual(confirmed(), ["ix-1@7"])
    }

    func testNoConfirmationWhenTheQueuedPostIsDropped() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.hold = true
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        await untilHeld(center)
        notifier.apply(summary(item("s2", card: "ix-2"), more: 1), conn: 7) // ix-1's session is now past the cap
        center.release()
        await notifier.settled()
        XCTAssertFalse(center.added.contains("laptop/s1#needs"))
        XCTAssertFalse(confirmed().contains("ix-1@7"), "a card whose notification was never handed over was confirmed")
    }

    func testNoConfirmationWhenTheAddFails() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.failAdd = true
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        await notifier.settled()
        XCTAssertEqual(confirmed(), [])
    }

    func testNoConfirmationWhenTheConnectionGoes() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.hold = true
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        await untilHeld(center)
        notifier.apply(nil, conn: nil) // the link went
        center.release()
        await notifier.settled()
        XCTAssertEqual(confirmed(), [])
    }

    func testNoConfirmationWhenTheConnectionGoesWhileHandingOver() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.holdAdd = true
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        for _ in 0..<1000 where center.adding.isEmpty { await Task.yield() }
        XCTAssertFalse(center.adding.isEmpty, "the notification never reached Notification Center")
        notifier.apply(nil, conn: nil) // the link went while Notification Center took it
        center.release()
        await notifier.settled()
        XCTAssertEqual(center.added, ["laptop/s1#needs"])
        XCTAssertEqual(confirmed(), [], "a confirmation for a connection that went")
    }

    func testNoConfirmationForANotificationTakenBackAtOnce() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.holdAdd = true
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        for _ in 0..<1000 where center.adding.isEmpty { await Task.yield() }
        XCTAssertFalse(center.adding.isEmpty, "the notification never reached Notification Center")
        notifier.apply(summary(item("s1", card: "ix-2")), conn: 7) // the session waits on a newer card while ix-1's goes out
        center.release()
        await notifier.settled()
        XCTAssertEqual(confirmed(), ["ix-2@7"], "a card whose notification was taken back at once was confirmed")
    }

    func testNoConfirmationWithNotificationsOff() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        center.access = NotificationAccess(status: .denied, style: .banner)
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 7)
        await notifier.settled()
        XCTAssertEqual(center.added, [])
        XCTAssertEqual(confirmed(), [])
    }

    /// The confirmation names the helper's link whose summary listed the card; with a helper that names none, the
    /// app confirms nothing (such a helper passes nothing on).
    func testConfirmationNamesTheLinkOfItsSummary() async {
        let center = FakeCenter()
        let (notifier, confirmed) = await started(center)
        notifier.apply(nil, conn: nil) // the link went
        notifier.apply(summary(), conn: 8) // the next one's baseline
        notifier.apply(summary(item("s1", card: "ix-1")), conn: 8)
        await notifier.settled()
        XCTAssertEqual(confirmed(), ["ix-1@8"])

        let older = FakeCenter()
        let (quiet, none) = await started(older, conn: nil)
        quiet.apply(summary(item("s1", card: "ix-1")), conn: nil)
        await quiet.settled()
        XCTAssertEqual(older.added, ["laptop/s1#needs"])
        XCTAssertEqual(none(), [], "a confirmation for a helper that names no link")
    }

    func testConfirmationLine() {
        XCTAssertEqual(Presence.rangLine("ix_1/a", conn: 3).map { String(decoding: $0, as: UTF8.self) },
                       #"{"rang":{"conn":3,"interaction_id":"ix_1/a"}}"# + "\n")
        XCTAssertEqual(Presence.rangLine(#"a"b"#, conn: 1).map { String(decoding: $0, as: UTF8.self) },
                       #"{"rang":{"conn":1,"interaction_id":"a\"b"}}"# + "\n", "escaped as JSON")
    }
}
