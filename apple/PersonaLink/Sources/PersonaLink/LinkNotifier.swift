import AppKit
import UserNotifications

/// Posts and takes back the "Needs you" notifications (`AttentionNotifications` decides which), asks for permission
/// once the hub sends a summary, hands a clicked notification's route to `open`, and confirms each card whose
/// notification it handed to Notification Center (`confirm`). The running app's goes to the system's notification
/// center, which needs a bundled app: outside one (snapshots, `swift run`) it does nothing; tests give their own.
@MainActor final class LinkNotifier: NSObject, UNUserNotificationCenterDelegate {
    static let shared = LinkNotifier(center: Bundle.main.bundleURL.pathExtension == "app" ? SystemNotifications() : nil)

    private var diff = AttentionNotifications()
    private var asked = false
    private var allowed = true
    /// The work of each change, in order: each waits for the one before, so a post never lands after a later take-back.
    private var last: Task<Void, Never>?
    private var open: (_ route: String, _ webURL: String?) -> Void = { _, _ in }
    private var report: (_ access: NotificationAccess) -> Void = { _ in }
    private var confirm: (_ card: String, _ conn: UInt64) -> Void = { _, _ in }
    private let center: NotificationCenterLike?

    init(center: NotificationCenterLike?) {
        self.center = center
    }

    /// Called at launch, before any summary. `open`: a clicked notification's route and the hub's web address when it
    /// was posted. `access`: what notifications may do, whenever that is checked. `confirm`: a card whose notification
    /// went to Notification Center, and the helper's link whose summary listed it (`LinkStatus.conn`).
    func start(open: @escaping (_ route: String, _ webURL: String?) -> Void,
               access: @escaping (_ access: NotificationAccess) -> Void,
               confirm: @escaping (_ card: String, _ conn: UInt64) -> Void) {
        guard let center else { return }
        self.open = open
        report = access
        self.confirm = confirm
        center.attach(self)
        // Notifications an earlier run left shown go when the summary no longer has their session.
        run {
            self.withdraw(self.diff.adopt(await center.delivered()))
            await self.refreshAuthorization()
        }
    }

    /// Waits until the work of every change so far is done.
    func settled() async {
        await last?.value
    }

    /// A status line's summary changed (nil: it went away). `conn`: the helper's link that sent it; a helper that does
    /// not say gets no confirmations.
    func apply(_ attention: LinkAttention?, conn: UInt64?) {
        guard center != nil else { return }
        let changes = diff.apply(attention)
        let notes = changes.post
        let connection = changes.connection
        let webURL = attention?.webURL
        // Asked once the hub sends summaries at all, not at launch: an older hub never sends one.
        let ask = attention != nil && !asked
        if ask { asked = true }
        run {
            self.withdraw(changes.withdrawNow)
            if ask {
                await self.requestAuthorization()
            } else if !notes.isEmpty {
                await self.refreshAuthorization() // System Settings may have turned them on or off since the last check
            }
            // While the permission prompt waited, a later summary may have answered or replaced the card or stopped it
            // notifying (muted), or the connection may have dropped.
            for item in notes {
                if self.allowed, self.diff.isCurrent(item, connection: connection),
                   await self.post(AttentionNote(item, webURL: webURL)) {
                    // Shown only now; the summary may have moved on while it went out.
                    let gone = self.diff.adopt([(item.id, item.card)])
                    self.withdraw(gone)
                    // Handed to Notification Center and left there: the hub may leave this card to this Mac, unless the
                    // connection it was for went meanwhile (a new one starts with no confirmations). The confirmation
                    // names the helper's link the summary came on: the helper passes it on to that link only.
                    if let conn, !gone.contains(item.id) && !item.card.isEmpty && self.diff.connection == connection {
                        self.confirm(item.card, conn)
                    }
                } else if changes.withdraw.contains(item.id) {
                    self.withdraw([item.id]) // the older notification it was to replace
                }
            }
        }
    }

    /// What notifications may do now, for the menu and presence (System Settings may have changed it meanwhile).
    func refreshAuthorization() async {
        guard let center else { return }
        let access = await center.access()
        allowed = access.allowed
        report(access)
    }

    private func run(_ work: @escaping @MainActor () async -> Void) {
        let previous = last
        last = Task {
            await previous?.value
            await work()
        }
    }

    private func withdraw(_ ids: [String]) {
        guard !ids.isEmpty else { return }
        center?.remove(ids)
    }

    /// Prompts the first time only; later calls return what the user chose.
    private func requestAuthorization() async {
        do {
            try await center?.requestAuthorization()
        } catch {
            NSLog("Persona Link: notification permission not asked: %@", error.localizedDescription)
        }
        await refreshAuthorization() // denied: the menu says so
    }

    /// Whether it went out: handed to Notification Center.
    private func post(_ note: AttentionNote) async -> Bool {
        guard let center else { return false }
        do {
            try await center.add(note)
            return true
        } catch {
            NSLog("Persona Link: notification not posted: %@", error.localizedDescription)
            return false
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let route = info[AttentionNote.routeKey] as? String ?? ""
        let webURL = info[AttentionNote.webURLKey] as? String
        Task { @MainActor in LinkNotifier.shared.open(route, webURL) }
        completionHandler()
    }

    /// The app counts as in front while its menu or a window is: show the banner anyway.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .list, .sound])
    }
}

/// What System Settings lets this app's notifications do.
struct NotificationAccess {
    /// Not turned off: posts go out, also before the user was asked (the menu says when they are off).
    var allowed: Bool
    /// Allowed and shown as banners or alerts, so someone at this Mac sees one as it comes: presence's `notifies`.
    /// Quiet (provisional) delivery and the None style reach only Notification Center.
    var shows: Bool

    init(status: UNAuthorizationStatus, style: UNAlertStyle) {
        allowed = status != .denied
        shows = status == .authorized && style != .none
    }
}

/// What LinkNotifier needs of a notification center: the system's (SystemNotifications), or a test's.
@MainActor protocol NotificationCenterLike: AnyObject {
    /// Clicks and banners in front go to delegate.
    func attach(_ delegate: UNUserNotificationCenterDelegate)
    /// Hands note to Notification Center; the same id replaces a notification still shown for its session.
    func add(_ note: AttentionNote) async throws
    func remove(_ ids: [String])
    /// The "Needs you" notifications shown now, with the card each is about.
    func delivered() async -> [(id: String, card: String?)]
    func access() async -> NotificationAccess
    func requestAuthorization() async throws
}

/// The system's notification center (`UNUserNotificationCenter.current()`, which raises outside an app bundle).
@MainActor final class SystemNotifications: NotificationCenterLike {
    private var center: UNUserNotificationCenter { .current() }

    func attach(_ delegate: UNUserNotificationCenterDelegate) { center.delegate = delegate }

    func add(_ note: AttentionNote) async throws {
        let content = UNMutableNotificationContent()
        content.title = note.title
        content.subtitle = note.subtitle
        content.body = note.body
        content.sound = .default
        content.userInfo = note.userInfo
        try await center.add(UNNotificationRequest(identifier: note.id, content: content, trigger: nil))
    }

    func remove(_ ids: [String]) {
        center.removeDeliveredNotifications(withIdentifiers: ids)
        center.removePendingNotificationRequests(withIdentifiers: ids)
    }

    func delivered() async -> [(id: String, card: String?)] {
        await center.deliveredNotifications().map(\.request)
            .filter { $0.identifier.hasSuffix("#needs") }
            .map { (id: $0.identifier, card: $0.content.userInfo[AttentionNote.cardKey] as? String) }
    }

    func access() async -> NotificationAccess {
        let settings = await center.notificationSettings()
        return NotificationAccess(status: settings.authorizationStatus, style: settings.alertStyle)
    }

    func requestAuthorization() async throws {
        _ = try await center.requestAuthorization(options: [.alert, .sound])
    }
}
