import AppKit
import CoreGraphics

/// What the app tells persona-link about the user at this Mac (docs/operations/link.md "Presence"): `active` while
/// someone is using this Mac and its notifications show, `notifies` while they show at all. The hub counts an active
/// Link Mac as a desktop, so a phone with "Phone Muted" on leaves the alerts to this Mac.
struct Presence: Equatable {
    var active: Bool
    var notifies: Bool

    /// One line for the helper's stdin, newline included: `{"presence":{"active":true,"notifies":true}}`.
    var line: Data { Data(#"{"presence":{"active":\#(active),"notifies":\#(notifies)}}"#.utf8) + Data([0x0A]) }

    /// The line that confirms card's notification went to Notification Center, newline included:
    /// `{"rang":{"conn":2,"interaction_id":"…"}}`, `conn` the helper's link whose summary listed the card; persona-link
    /// passes it on to that link only, and the hub may then leave that card to this Mac.
    static func rangLine(_ card: String, conn: UInt64) -> Data? {
        struct Line: Encodable {
            struct Card: Encodable {
                var interactionID: String
                var conn: UInt64
                enum CodingKeys: String, CodingKey { case interactionID = "interaction_id", conn }
            }
            var rang: Card
        }
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.withoutEscapingSlashes, .sortedKeys]
        do {
            return try encoder.encode(Line(rang: .init(interactionID: card, conn: conn))) + Data([0x0A])
        } catch {
            NSLog("Persona Link: confirmation not encoded: %@", String(describing: error))
            return nil
        }
    }
}

/// Decides presence from the system's signals and when to write it to the helper. Pure: the caller samples the idle
/// time and the clock, so tests drive it without a screen or a notification permission.
struct PresenceTracker {
    /// Input within the last 5 minutes counts as someone at this Mac. Reading, a call or a long build without touching
    /// the keyboard keeps the phone quiet; a Mac left unlocked hands the alerts back to the phone 5 minutes later.
    static let idleLimit: TimeInterval = 5 * 60
    /// While active, the same line again this often: the helper's presence on the hub lives 120 s, so one lost line
    /// does not let it lapse. Also how often to look again while notifications do not show.
    static let repeatEvery: TimeInterval = 60
    /// While only input is missing, how often to look for it: nothing announces the user's return, so it is noticed at
    /// most this late.
    static let pollEvery: TimeInterval = 10
    /// The shortest wait between checks, so a check that lands a moment early does not spin.
    static let minimumWait: TimeInterval = 1

    enum Signal: Sendable {
        case locked(Bool), screensAsleep(Bool), sessionActive(Bool), notifies(Bool)
    }

    private var locked = false
    private var screensAsleep = false
    /// False while another user's session has the screen (fast user switching).
    private var sessionActive = true
    /// This app's notifications show (`NotificationAccess.shows`); false until LinkNotifier says.
    private var notifies = false
    /// The last presence written to this helper, and when; nil: none yet.
    private var sent: (presence: Presence, at: TimeInterval)?

    /// Whether it changed anything.
    @discardableResult mutating func apply(_ signal: Signal) -> Bool {
        let before = (locked, screensAsleep, sessionActive, notifies)
        switch signal {
        case .locked(let on): locked = on
        case .screensAsleep(let on): screensAsleep = on
        case .sessionActive(let on): sessionActive = on
        case .notifies(let on): notifies = on
        }
        return before != (locked, screensAsleep, sessionActive, notifies)
    }

    /// A signal says the user cannot be at this Mac, until another signal says otherwise.
    private var away: Bool { locked || screensAsleep || !sessionActive }

    /// `idle`: seconds since the last input.
    func presence(idle: TimeInterval) -> Presence {
        Presence(active: notifies && !away && idle < Self.idleLimit, notifies: notifies)
    }

    /// The presence to write now (nil: none), and how long until the next check (nil: not before the next signal).
    /// A change is written at once, an active state again every `repeatEvery`. `now`: seconds on a monotonic clock.
    mutating func check(idle: TimeInterval, now: TimeInterval) -> (write: Presence?, next: TimeInterval?) {
        let presence = presence(idle: idle)
        let due = sent.map { $0.presence != presence || presence.active && now - $0.at >= Self.repeatEvery } ?? true
        if due { sent = (presence, now) }
        let write = due ? presence : nil
        guard presence.active, let sent else {
            return (write, away ? nil : notifies ? Self.pollEvery : Self.repeatEvery)
        }
        return (write, max(Self.minimumWait, min(Self.idleLimit - idle, Self.repeatEvery - (now - sent.at))))
    }

    /// A new helper has heard nothing: the next check writes the state again, active or not.
    mutating func helperStarted() { sent = nil }
}

/// Feeds `PresenceTracker` the system's signals (screen lock, display sleep, fast user switching, idle time) and hands
/// its lines to `write`. Whether notifications show comes from LinkNotifier through `receive(.notifies)`; this asks
/// for them again at most once a `repeatEvery`, since System Settings announces no change.
@MainActor final class PresenceMonitor: NSObject {
    private var tracker = PresenceTracker()
    private var write: (Data) -> Void = { _ in }
    private var started = false
    private var observers: [NSObjectProtocol] = []
    private var timer: DispatchWorkItem?
    /// When this last asked LinkNotifier for the notification settings (systemUptime).
    private var notifiesRead = -TimeInterval.infinity

    func start(write: @escaping (Data) -> Void) {
        guard !started else { return }
        started = true
        self.write = write
        notifiesRead = ProcessInfo.processInfo.systemUptime // LinkNotifier.start reads them at launch
        // The state at launch; the notifications below keep it from then on. The lock key is not documented.
        let session = CGSessionCopyCurrentDictionary() as? [String: Any] ?? [:]
        tracker.apply(.locked(session["CGSSessionScreenIsLocked"] as? Bool ?? false))
        tracker.apply(.sessionActive(session[kCGSessionOnConsoleKey] as? Bool ?? true))
        tracker.apply(.screensAsleep(CGDisplayIsAsleep(CGMainDisplayID()) != 0))
        let workspace = NSWorkspace.shared.notificationCenter
        observe(workspace, NSWorkspace.screensDidSleepNotification, .screensAsleep(true))
        observe(workspace, NSWorkspace.screensDidWakeNotification, .screensAsleep(false))
        observe(workspace, NSWorkspace.sessionDidResignActiveNotification, .sessionActive(false))
        observe(workspace, NSWorkspace.sessionDidBecomeActiveNotification, .sessionActive(true))
        // Not documented, but what macOS posts as the screen locks and unlocks. AppKit holds distributed notifications
        // back while the app is not active, which a menu bar app almost never is: these come at once regardless.
        let distributed = DistributedNotificationCenter.default()
        distributed.addObserver(self, selector: #selector(screenLocked), name: Notification.Name("com.apple.screenIsLocked"),
                                object: nil, suspensionBehavior: .deliverImmediately)
        distributed.addObserver(self, selector: #selector(screenUnlocked),
                                name: Notification.Name("com.apple.screenIsUnlocked"), object: nil,
                                suspensionBehavior: .deliverImmediately)
        check()
    }

    func receive(_ signal: PresenceTracker.Signal) {
        if tracker.apply(signal) && started { check() }
    }

    @objc private func screenLocked(_ note: Notification) { receive(.locked(true)) }
    @objc private func screenUnlocked(_ note: Notification) { receive(.locked(false)) }

    /// The helper (re)started: it gets the current state at once.
    func helperStarted() {
        tracker.helperStarted()
        if started { check() }
    }

    private func observe(_ center: NotificationCenter, _ name: Notification.Name, _ signal: PresenceTracker.Signal) {
        observers.append(center.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.receive(signal) }
        })
    }

    private func check() {
        let now = ProcessInfo.processInfo.systemUptime
        if now - notifiesRead >= PresenceTracker.repeatEvery {
            notifiesRead = now
            Task { await LinkNotifier.shared.refreshAuthorization() }
        }
        let result = tracker.check(idle: Self.idle(), now: now)
        if let presence = result.write { write(presence.line) }
        timer?.cancel()
        timer = nil
        guard let next = result.next else { return }
        let item = DispatchWorkItem { [weak self] in MainActor.assumeIsolated { self?.check() } }
        timer = item
        DispatchQueue.main.asyncAfter(deadline: .now() + next, execute: item)
    }

    /// Seconds since the last keyboard, mouse or trackpad input, as the HID system (the hardware) sees it.
    private static func idle() -> TimeInterval {
        CGEventSource.secondsSinceLastEventType(.hidSystemState, eventType: anyInput)
    }

    private static let anyInput = CGEventType(rawValue: ~0)! // kCGAnyInputEventType
}
