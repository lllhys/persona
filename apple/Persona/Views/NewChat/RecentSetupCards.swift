import PersonaWire
import SwiftUI

/// A recent setup as New Agent's card shows it, and what tapping it puts in the form (#2134).
nonisolated struct RecentSetupCard: Equatable {
    var setup: RecentSetup
    /// Its machine is online: only then can the card be picked.
    var online: Bool

    /// What a card puts in New Agent's fields. It only fills them: Start stays the user's.
    struct Fill: Equatable {
        var machine: String
        var cwd: String
        var launcher: String
        var model: String
        /// Claude Code's mode and effort; nil for another agent's card, which leaves the machine's own in place.
        var mode: String?
        var effort: String?
    }

    /// Nil when its machine is offline: the card can't be picked.
    var fill: Fill? {
        guard online else { return nil }
        let claude = setup.agentType == "claude"
        return Fill(machine: setup.machine, cwd: setup.cwd, launcher: setup.launcher, model: setup.model,
                    mode: claude ? setup.mode : nil, effort: claude ? setup.effort : nil)
    }

    /// Line 1: the folder's last path component, "~" for the home folder; none: a temporary session (a start in Claude's
    /// desktop app with no folder, #5480).
    static func folder(_ cwd: String, home: String?) -> String {
        if cwd.isEmpty { return String(localized: "Temporary Session") }
        let trimmed = cwd.count > 1 && cwd.hasSuffix("/") ? String(cwd.dropLast()) : cwd
        if trimmed == "~" || (home.map { !$0.isEmpty && trimmed == $0 } ?? false) { return "~" }
        let last = (trimmed as NSString).lastPathComponent
        return last.isEmpty ? trimmed : last
    }

    /// Line 2: the launcher (or the follow-configuration wording the form uses), the model when it is not the default,
    /// then, for Claude Code only, the permission mode and the thinking effort when they are not the launcher's own.
    var launcherText: String { setup.launcher.isEmpty ? String(localized: "Use Model Configuration") : setup.launcher }
    var modelText: String? { setup.model.isEmpty ? nil : setup.model }
    var modeText: String? {
        guard setup.agentType == "claude", !setup.mode.isEmpty else { return nil }
        return SessionModes.modeLabel(setup.mode)
    }
    var effortText: String? {
        guard setup.agentType == "claude", !setup.effort.isEmpty else { return nil }
        return SessionModes.effortLabel(setup.effort)
    }
    /// Allow Everything: said in the caution color with its mark.
    var bypass: Bool { setup.agentType == "claude" && setup.mode == "bypassPermissions" }
}

/// One card of New Agent's Recent section: folder · machine, then what it starts with; a context menu removes it or
/// clears them all.
struct RecentSetupRow: View {
    var card: RecentSetupCard
    var machine: Machine
    var picked: Bool
    var pick: () -> Void
    var remove: () -> Void
    var clear: () -> Void

    var body: some View {
        Button(action: pick) {
            HStack(spacing: Theme.Space.m) {
                ThemedIcon(Theme.Icon.folderFilled).foregroundStyle(card.online ? Theme.accent : Color.secondary)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    title
                        .foregroundStyle(card.online ? Color.primary : Color.secondary)
                        .lineLimit(1)
                    detail
                        .font(Theme.Font.label)
                        // Gray as a color: a button's label draws the hierarchical styles in its tint.
                        .foregroundStyle(card.online ? AnyShapeStyle(Color.secondary) : AnyShapeStyle(.tertiary))
                        .lineLimit(1)
                }
                Spacer(minLength: Theme.Space.s)
                if !card.online {
                    Chip(text: String(localized: "Offline [Machine Entity be1b4f]"))
                } else if picked {
                    ThemedIcon(Theme.Icon.successFilled).foregroundStyle(Theme.accent)
                }
            }
        }
        .disabled(!card.online)
        .accessibilityHint(card.online ? "" : String(localized: "This computer is offline"))
        .accessibilityAddTraits(picked ? .isSelected : [])
        .contextMenu {
            Button(action: remove) { Label(String(localized: "Remove from Recent"), systemImage: Theme.Icon.clear) }
            Button(role: .destructive, action: clear) { Label(String(localized: "Clear Recent Setups"), systemImage: Theme.Icon.delete) }
        }
    }

    /// folder · machine. Texts are joined by interpolation, not `+` (deprecated in iOS 26): "%@%@" and "%@ · %@" are
    /// the catalog's format-only keys.
    private var title: Text {
        let folder = Text(RecentSetupCard.folder(card.setup.cwd, home: machine.home)).font(Theme.Font.heading)
        let place = Text(" · " + machine.title).foregroundStyle(Color.secondary)
        return Text("\(folder)\(place)")
    }

    /// launcher · model · mode · effort; Allow Everything in the caution color with its mark (gray while offline).
    private var detail: Text {
        var parts: [Text] = []
        if let m = card.modelText { parts.append(Text(m)) }
        if let mode = card.modeText {
            if card.bypass {
                let warning = Text("\(Theme.Icon.image(Theme.Icon.allowEverything)) \(mode)")
                parts.append(card.online ? warning.foregroundStyle(Theme.cautionText) : warning)
            } else {
                parts.append(Text(mode))
            }
        }
        if let e = card.effortText { parts.append(Text(e)) }
        return parts.reduce(Text(card.launcherText)) { line, part in Text("\(line) · \(part)") }
    }
}
