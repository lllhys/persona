import SwiftUI

// The Persona page's manager switcher (docs/assistant/managers.md "Clients"): with more than one installed manager, a
// chip per manager under the page's top bar, the default first and then the roster's order. The page shows the chosen
// one's assistant (`AppModel.selectAssistantManager`); it starts on the default and isn't kept across launches. An
// offline one is greyed: its page shows what was read of it in this run, with its composer off.

extension EnvironmentValues {
    /// The switcher's height under the Persona page's top bar, 0 without one: what the page leaves free at its top
    /// besides the bar's room (`AssistantTopBar.room`).
    @Entry var managerSwitcherRoom: CGFloat = 0
}

/// One chip per installed manager; nothing with one manager.
struct ManagerSwitcher: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        if let roster = model.managerRoster, roster.isMulti {
            let shown = model.assistantManager?.id
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Theme.Space.s) {
                    ForEach(roster.installed) { m in
                        FilterChip(title: m.name, symbol: m.online ? nil : Theme.Icon.offline, on: m.id == shown) {
                            model.selectAssistantManager(m.id)
                        }
                        .opacity(m.online ? 1 : 0.55)
                        .accessibilityLabel(m.online ? m.name : String(localized: "\(m.name), offline"))
                        .accessibilityIdentifier("manager-switch-\(m.id)")
                    }
                }
                .padding(.horizontal, Theme.Space.page)
            }
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
            .padding(.vertical, Theme.Space.xs)
            .accessibilityElement(children: .contain)
            .accessibilityLabel(String(localized: "Managers"))
            .accessibilityIdentifier("manager-switcher")
        }
    }
}

/// Over the composer of an offline manager's conversation, or of its page's new conversation: nothing goes to it until it
/// is back, and since when it last synced.
struct ManagerOfflineNote: View {
    var manager: HubManager

    /// "<name> · last synced <time>", and that messages can go once it is back.
    static func text(_ m: HubManager) -> String {
        guard let seen = m.lastSeen else { return String(localized: "\(m.name) is offline. Messages can be sent once it's back.") }
        let when = seen.formatted(.relative(presentation: .named, unitsStyle: .wide))
        return String(localized: "\(m.name) · last synced \(when). Messages can be sent once it's back.")
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
            ThemedIcon(Theme.Icon.offline).font(Theme.Font.labelStrong).foregroundStyle(.secondary)
            Text(Self.text(manager)).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(3)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.s)
        .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("manager-offline-note")
    }
}
