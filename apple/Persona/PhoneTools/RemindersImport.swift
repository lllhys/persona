import EventKit
import Foundation
import PersonaWire

/// Reminders imported as to-dos, one way (#4009, docs/assistant/todos.md "Imported from Reminders"): the incomplete
/// reminders of the lists the user picked go to the hub, which makes a to-do of each new one and follows changes of
/// their title, notes and due date. Nothing is written back. Opt-in (Settings › Phone Data), off by default.
///
/// It runs only while the app does: on launch and return to the foreground, and when the Reminders database changes
/// while the app is in front. Reminders the hub already has (same hash, same hub) are not sent again; more than
/// `batchMax` go in several requests.
nonisolated enum RemindersImport {
    /// Settings › Phone Data › Import into To-dos.
    static let enabledKey = "reminders.importTodos"
    /// The lists to import (`EKCalendar.calendarIdentifier`); none stored: every list. Written when the user picks.
    static let listsKey = "reminders.importLists"
    /// The hash of the last batch the hub took, by hub group (`StateCache`).
    static let sentCache = "reminders-import-sent"

    /// One reminder as `POST /api/v1/todos/import/reminders` takes it.
    struct Item: Encodable, Equatable, Sendable {
        var externalID: String
        var title: String
        var notes: String?
        var dueAt: String?
        var dueDate: String?

        enum CodingKeys: String, CodingKey {
            case externalID = "external_id", title, notes, dueAt = "due_at", dueDate = "due_date"
        }
    }

    struct Batch: Encodable, Sendable { var items: [Item] }

    /// A reminder's due date the way the hub keeps one: a moment (RFC 3339, UTC) when it has a time, else the day.
    static func due(_ components: DateComponents?, calendar: Calendar = .current) -> (at: String?, date: String?) {
        guard let c = components, let year = c.year, let month = c.month, let day = c.day else { return (nil, nil) }
        if c.hour == nil {
            return (nil, String(format: "%04d-%02d-%02d", year, month, day))
        }
        var cal = calendar
        if let zone = c.timeZone { cal.timeZone = zone }
        guard let date = cal.date(from: c) else { return (nil, nil) }
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return (f.string(from: date), nil)
    }

    /// What the hub takes in one request (`importBatchMax`, server/internal/assistant/todo_imports.go).
    static let batchMax = 1000

    /// The batches for these reminders: in a stable order (so the hash only changes when a reminder does), split
    /// into requests the hub takes; every reminder is in one of them, however many there are.
    static func batches(_ items: [Item]) -> [Batch] {
        let sorted = items.sorted { $0.externalID < $1.externalID }
        return stride(from: 0, to: sorted.count, by: batchMax).map {
            Batch(items: Array(sorted[$0..<min($0 + batchMax, sorted.count)]))
        }
    }

    /// A stable hash of the batches (FNV-1a over their JSON), to tell whether they changed since they were last sent.
    static func hash(_ batches: [Batch]) -> String {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        guard let data = try? encoder.encode(batches) else { return "" }
        var h: UInt64 = 0xcbf2_9ce4_8422_2325
        for byte in data {
            h ^= UInt64(byte)
            h = h &* 0x100_0000_01b3
        }
        return String(h, radix: 16)
    }

    static var isEnabled: Bool { UserDefaults.standard.bool(forKey: enabledKey) }

    /// The lists picked; nil: all of them.
    static var lists: Set<String>? {
        (UserDefaults.standard.array(forKey: listsKey) as? [String]).map(Set.init)
    }

    /// A Reminders list the user can pick.
    struct ReminderList: Identifiable, Sendable {
        var id: String
        var title: String
    }

    /// The Reminders lists, by name; read off the main thread (EventKit's calls are synchronous, #1337).
    static func availableLists() async -> [ReminderList] {
        await Task.detached {
            EKEventStore().calendars(for: .reminder)
                .map { ReminderList(id: $0.calendarIdentifier, title: $0.title) }
                .sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
        }.value
    }

    /// An event store made off the main thread (P1: making one is a call to EventKit's daemon). EKEventStore is safe to
    /// use from any thread but not marked Sendable.
    struct Store: @unchecked Sendable { let store: EKEventStore }

    @concurrent static func makeStore() async -> Store { Store(store: EKEventStore()) }

    /// Whether Persona may read Reminders now, asked off the main thread (P1, #1337).
    static func mayRead() async -> Bool {
        await CalendarAccess.statusesOffMain().reminders.isUsable
    }

    /// The hashes of what each hub took, read from disk off the main thread (P1).
    @concurrent static func loadSent() async -> [String: String] {
        StateCache.load(sentCache, as: [String: String].self) ?? [:]
    }

    @concurrent static func saveSent(_ sent: [String: String]) async {
        StateCache.save(sentCache, sent)
    }

    /// The incomplete reminders of the picked lists, as items, read off the main thread (P1). EventKit calls back on its
    /// own queue, so the block is `@Sendable` and turns its objects into values there.
    @concurrent static func read(lists picked: Set<String>?) async -> [Item] {
        let store = EKEventStore()
        let all = store.calendars(for: .reminder)
        let chosen = picked.map { ids in all.filter { ids.contains($0.calendarIdentifier) } } ?? all
        if chosen.isEmpty { return [] }
        let predicate = store.predicateForIncompleteReminders(withDueDateStarting: nil, ending: nil, calendars: chosen)
        return await withCheckedContinuation { (continuation: CheckedContinuation<[Item], Never>) in
            store.fetchReminders(matching: predicate) { @Sendable reminders in
                let items = (reminders ?? []).compactMap { r -> Item? in
                    guard let id = r.calendarItemExternalIdentifier, !id.isEmpty else { return nil }
                    let (at, day) = due(r.dueDateComponents)
                    return Item(externalID: id, title: r.title ?? "", notes: r.notes, dueAt: at, dueDate: day)
                }
                continuation.resume(returning: items)
            }
        }
    }
}

#if os(iOS)
/// Sends the import when the app comes to the front (`PersonaApp`'s scene phase), when the Reminders database changes
/// while it is there, and when the user turns it on or picks lists.
@MainActor final class RemindersImporter {
    static let shared = RemindersImporter()
    private var running: Task<Void, Never>?
    private var again = false
    private weak var app: AppModel?
    /// Kept while the import is on: EventKit posts `EKEventStoreChanged` only to a store that is alive.
    private var store: EKEventStore?
    private var observer: NSObjectProtocol?
    private var watching = false
    /// What each hub took (hash by hub group); nil until read from disk, on the first import.
    private var sent: [String: String]?

    private init() {}

    /// Back in front (or turned on): follows changes of the Reminders database while the import is on, and imports now.
    func activate(_ app: AppModel, force: Bool = false) {
        self.app = app
        if RemindersImport.isEnabled {
            if observer == nil, !watching {
                watching = true
                Task { await watch() }
            }
        } else if let observer {
            NotificationCenter.default.removeObserver(observer)
            self.observer = nil
            store = nil
        }
        sync(app, force: force)
    }

    private func watch() async {
        let made = await RemindersImport.makeStore()
        watching = false
        guard RemindersImport.isEnabled, observer == nil else { return } // turned off meanwhile
        store = made.store
        observer = NotificationCenter.default.addObserver(forName: .EKEventStoreChanged, object: made.store, queue: .main) { @Sendable _ in
            Task { @MainActor in RemindersImporter.shared.changed() }
        }
    }

    private func changed() {
        if let app { sync(app) }
    }

    /// Imports now; `force` sends even a batch the hub already took (the user changed the lists or turned it on).
    func sync(_ app: AppModel, force: Bool = false) {
        if force { sent = [:] }
        if running != nil {
            again = true // the change lands after the batch on its way: one more
            return
        }
        running = Task {
            defer {
                running = nil
                if again {
                    again = false
                    sync(app)
                }
            }
            await importOnce(app)
        }
    }

    private func importOnce(_ app: AppModel) async {
        guard RemindersImport.isEnabled, PhoneDataSource.reminders.isEnabled,
              let link = app.hub, !link.isDemo, let api = app.api,
              await RemindersImport.mayRead() else { return }
        let batches = await RemindersImport.batches(RemindersImport.read(lists: RemindersImport.lists))
        let hash = RemindersImport.hash(batches)
        if sent == nil { sent = await RemindersImport.loadSent() }
        guard sent?[link.group] != hash else { return }
        do {
            // One request per batch; the hash counts as sent only once all of them were taken, so a batch that
            // failed is sent again next time (the ones before it change nothing then).
            for batch in batches {
                _ = try await api.post("/api/v1/todos/import/reminders", body: batch)
            }
            sent?[link.group] = hash
            await RemindersImport.saveSent(sent ?? [:])
        } catch {
            // An older hub (404) or no connection: the next foreground or change tries again.
        }
    }
}
#endif
