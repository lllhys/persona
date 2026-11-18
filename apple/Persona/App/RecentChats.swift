import Foundation
import PersonaWire

/// The conversations opened on this device, newest first, by when they were opened rather than by their last activity
/// (#2191): the home screen icon's long-press menu has the last one (`QuickAction`). The rules are the web's ⌘K's
/// (spec/fixtures/recents/README.md, checked against recent_conversations.json in `RecentChatsTests`).
nonisolated enum RecentChats {
    static let maxEntries = 10
    /// How many the home screen menu lists: one, with Record, New Chat and Inbox (the system shows four).
    static let homeScreen = 1

    /// The conversation shown goes first; one already there moves (no second entry); the ten newest stay.
    static func open(_ before: [String], _ key: String) -> [String] {
        guard !key.isEmpty else { return before }
        return Array(([key] + before.filter { $0 != key }).prefix(maxEntries))
    }

    /// The entries to offer: those that still exist, without the one on screen, in order, at most `limit`.
    static func shown(_ entries: [String], current: String, exists: (String) -> Bool, limit: Int) -> [String] {
        Array(entries.filter { $0 != current && exists($0) }.prefix(limit))
    }
}

/// This device's list, and the conversation on screen. Only keys are kept (`SessionKey.path`, with its hub group), never a
/// title or a message; in `StateCache`, not UserDefaults, since it changes on every open; never sent to a hub.
@MainActor final class RecentChatsStore {
    static let shared = RecentChatsStore()
    private static let cacheName = "recent-chats"

    private(set) var entries: [String]
    /// The conversation on screen ("" for none).
    private(set) var current = ""
    private let saver = Debouncer(.seconds(1))

    init(entries: [String]? = nil) {
        self.entries = entries ?? StateCache.load(Self.cacheName, as: [String].self) ?? []
    }

    /// A chat is on screen (`AppModel.visibleSession`), or none: one opened goes first and becomes the current one.
    func shown(_ key: SessionKey?) {
        guard let key else {
            current = ""
            return
        }
        current = key.path
        guard entries.first != current else { return }
        entries = RecentChats.open(entries, current)
        let snapshot = entries
        saver.run { StateCache.save(Self.cacheName, snapshot) }
        #if os(iOS)
        QuickAction.publish()
        #endif
    }

    /// The newest `limit` the app still has (`exists`), the one on screen included: for the home screen, which shows
    /// when the app is not.
    func latest(_ limit: Int, exists: (SessionKey) -> Bool) -> [SessionKey] {
        RecentChats.shown(entries, current: "", exists: { SessionKey(path: $0).map(exists) ?? false }, limit: limit).compactMap(SessionKey.init(path:))
    }
}
