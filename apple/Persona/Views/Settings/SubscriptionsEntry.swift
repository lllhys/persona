import SwiftUI

// Source subscriptions' entry rows (#2041, docs/extensions/subscriptions.md "Pages"): on an extension's details and a connector's
// page, shown once the hub reports `source_subscriptions` and only for a connector that can be subscribed (or already is).
// The pages themselves are the manager's, drawn by ExtensionPageView under `SubscriptionsUI.id`.

/// One connector's line of `GET /api/v1/subscriptions/summary`: `subscribable` is nil until its catalog was read.
nonisolated struct SubscriptionSummaryLine: Decodable, Equatable, Sendable {
    var connector: String
    var extensionID: String
    var subscribable: Bool?
    var on: Int
    var failing: Int

    init(connector: String, extensionID: String = "", subscribable: Bool? = nil, on: Int = 0, failing: Int = 0) {
        self.connector = connector
        self.extensionID = extensionID
        self.subscribable = subscribable
        self.on = on
        self.failing = failing
    }

    private enum CodingKeys: String, CodingKey { case connector, extensionID = "extension", subscribable, on, failing }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        connector = try c.decode(String.self, forKey: .connector)
        extensionID = try c.decodeIfPresent(String.self, forKey: .extensionID) ?? ""
        subscribable = try c.decodeIfPresent(Bool.self, forKey: .subscribable)
        on = try c.decodeIfPresent(Int.self, forKey: .on) ?? 0
        failing = try c.decodeIfPresent(Int.self, forKey: .failing) ?? 0
    }
}

/// What an entry row says: how many subscriptions are on and how many need attention.
nonisolated struct SubscriptionCounts: Equatable, Sendable {
    var on: Int
    var failing: Int

    /// The counts for the lines one row stands for; nil when none of them can be subscribed (the row is hidden).
    static func of(_ lines: [SubscriptionSummaryLine], where pick: (SubscriptionSummaryLine) -> Bool) -> SubscriptionCounts? {
        let mine = lines.filter(pick)
        guard mine.contains(where: { $0.subscribable == true || $0.on > 0 }) else { return nil }
        return SubscriptionCounts(on: mine.reduce(0) { $0 + $1.on }, failing: mine.reduce(0) { $0 + $1.failing })
    }

    var text: String {
        if failing > 0 { return String(localized: "\(on) on · \(failing) failing [Source Subscriptions]") }
        return on > 0 ? String(localized: "\(on) on [Source Subscriptions]") : String(localized: "Available [Source Subscriptions]")
    }
}

extension APIClient {
    /// `GET /api/v1/subscriptions/summary`: the entry rows' counts, from what the manager has cached.
    func subscriptionSummary() async throws -> [SubscriptionSummaryLine] {
        struct Summary: Decodable { var connectors: [SubscriptionSummaryLine]? }
        let summary: Summary = try await get("/api/v1/subscriptions/summary")
        return summary.connectors ?? []
    }
}

extension SubscriptionCounts {
    /// `manager`'s subscription summary, read through `api` naming it (nil: the default's, naming none); nil when it can't
    /// be read now (the row keeps what it had).
    static func summary(_ api: APIClient, manager: String?) async -> [SubscriptionSummaryLine]? {
        try? await api.manager(manager).subscriptionSummary()
    }
}

/// Bumped by each `subscription` event (AppModel), so the entry rows read the summary again.
@MainActor @Observable final class SubscriptionsRefresh {
    static let shared = SubscriptionsRefresh()
    private(set) var revision = 0
    func changed() { revision += 1 }
}

/// A Subscriptions row in its own section; nothing while `counts` is nil.
struct SubscriptionsEntrySection: View {
    var counts: SubscriptionCounts?
    /// The pages' first state (`SubscriptionsUI.state`).
    var state: String?
    /// Whose subscriptions the pages are: the page's own manager, as its requests name it (nil: the current assistant's).
    var manager: String? = nil

    var body: some View {
        if let counts {
            Section {
                NavigationLink(value: SettingsPage.subscriptions(state: state, manager: manager)) {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("Subscriptions [Source Subscriptions Entry]").lineLimit(1)
                            Text(counts.text).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                    } icon: {
                        ThemedIcon(Theme.Icon.subscriptions)
                    }
                }
                .accessibilityIdentifier("subscriptions-entry")
            }
        }
    }
}

extension View {
    /// Reads the counts for an entry row on this page: once the hub shows subscriptions, and again (a second after the last of a
    /// burst) whenever one changes. `manager`: whose page it is, as its requests name it (nil: the default's); each
    /// assistant's manager keeps its own subscriptions, and the hub sends the summary to the one named.
    func subscriptionCounts(_ counts: Binding<SubscriptionCounts?>, manager: String?,
                            where pick: @escaping (SubscriptionSummaryLine) -> Bool) -> some View {
        modifier(SubscriptionCountsLoader(counts: counts, manager: manager, pick: pick))
    }
}

private struct SubscriptionCountsLoader: ViewModifier {
    @Environment(AppModel.self) private var app
    @Binding var counts: SubscriptionCounts?
    var manager: String?
    var pick: (SubscriptionSummaryLine) -> Bool

    private struct Key: Equatable { var shown: Bool; var manager: String?; var revision: Int }

    func body(content: Content) -> some View {
        let shown = app.hubFeatures.contains(SubscriptionsUI.feature)
        content.task(id: Key(shown: shown, manager: manager, revision: SubscriptionsRefresh.shared.revision)) {
            guard shown else { counts = nil; return }
            if SubscriptionsRefresh.shared.revision > 0 { try? await Task.sleep(for: .seconds(1)) }
            // The lines are read off the main actor; the row's pick runs here, on them.
            guard !Task.isCancelled, let api = app.api, let lines = await SubscriptionCounts.summary(api, manager: manager) else { return }
            counts = SubscriptionCounts.of(lines, where: pick) // a failed read keeps what the row had
        }
    }
}

/// The pages from the Inbox's menu, every subscription (every source of the Inbox with `inbox_sources`, #5278): a sheet, as
/// the Inbox's Muted and Rules are. It closes when the pages hand something to the assistant (Set Up with …).
struct SubscriptionsSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(AppModel.self) private var app

    var body: some View {
        NavigationStack {
            ExtensionPageView(plugin: SubscriptionsUI.id, ui: SubscriptionsUI.page,
                              title: app.hubFeatures.contains(SubscriptionsUI.sourcesFeature) ? String(localized: "Inbox Sources")
                                  : String(localized: "Subscriptions [Source Subscriptions]"))
                .toolbar {
                    SheetClose { dismiss() }
                }
        }
        .onChange(of: app.assistantReveal) { dismiss() }
    }
}
