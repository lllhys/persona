import SwiftUI
import PersonaWire

// MARK: - A subagent's report, folded to one line

// What a subagent handed back reads as one line until it is opened: in its card's details, in its background notice,
// and in a message another session sent when that message is a subagent's handback. Open or not is kept by the card's
// id (`OpenReports`), so a row the lazy list builds again, or the chat opened again, stays as it was left.

/// The one line a folded report shows.
nonisolated enum ReportFold {
    /// How much of a report without a heading its line shows.
    static let limit = 80

    /// Its first line when that is a Markdown heading, else its first 80 characters with the line breaks as spaces; nil
    /// when the report is that line already (one line of 80 at most: nothing to fold).
    static func line(_ text: String) -> String? {
        let t = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !t.isEmpty else { return nil }
        let lines = t.split(separator: "\n", omittingEmptySubsequences: true).map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
        if let first = lines.first, first.hasPrefix("#") {
            let heading = plain(first.drop { $0 == "#" }.trimmingCharacters(in: .whitespaces))
            if !heading.isEmpty { return lines.count > 1 ? cut(heading) : nil }
        }
        let flat = lines.map(plain).joined(separator: " ")
        return lines.count > 1 || flat.count > limit ? cut(flat) : nil
    }

    private static func cut(_ s: String) -> String { s.count > limit ? String(s.prefix(limit)) + "…" : s }

    /// Without the Markdown marks a line starts with (a quote, a list item) and its bold.
    private static func plain(_ s: String) -> String {
        var t = Substring(s)
        while let c = t.first, c == ">" || c == "-" || c == "*" || c == "+" {
            let rest = t.dropFirst()
            // "**bold**" opening the line is bold, not a list item.
            if c == "*", rest.first == "*" { break }
            t = rest.drop { $0 == " " }
        }
        return t.replacingOccurrences(of: "**", with: "").replacingOccurrences(of: "__", with: "")
    }
}

/// Which reports are open, by their card's id (in the chat's scope: item ids repeat across chats); kept on disk with
/// the most recent few hundred, closed is the default.
@MainActor @Observable final class OpenReports {
    static let shared = OpenReports()
    private static let name = "open-reports"
    private static let kept = 400

    /// Oldest first.
    private var ids: [String]
    @ObservationIgnored private let persist: Bool

    init(persist: Bool = true) {
        self.persist = persist
        ids = persist ? StateCache.load(Self.name, as: [String].self) ?? [] : []
    }

    func isOpen(_ id: String) -> Bool { ids.contains(id) }

    func set(_ id: String, _ open: Bool) {
        ids.removeAll { $0 == id }
        if open { ids.append(id) }
        if ids.count > Self.kept { ids.removeFirst(ids.count - Self.kept) }
        if persist { StateCache.save(Self.name, ids) }
    }

    func closeAll() {
        ids = []
        if persist { StateCache.save(Self.name, ids) }
    }
}

/// A report folded to one line with "Expand", or whole (`full`) with "Collapse"; a short one is just whole. `id`: its
/// card's, unique in the chat, and the key the card is pinned by (`chatPinned`).
struct FoldedReport<Full: View>: View {
    var id: String
    var text: String
    var font = Theme.Font.caption
    var isError = false
    @ViewBuilder var full: () -> Full
    @Environment(\.rowMemoScope) private var scope
    @Environment(\.chatPin) private var pin

    private var key: String { scope + "/" + id }
    private var reports: OpenReports { OpenReports.shared }

    var body: some View {
        if let line = ReportFold.line(text) {
            let open = reports.isOpen(key)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                if open {
                    full()
                    toggle(open)
                } else {
                    Button { flip(open) } label: {
                        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.s) {
                            Text(line)
                                .font(font)
                                .foregroundStyle(isError ? AnyShapeStyle(Theme.danger) : AnyShapeStyle(.secondary))
                                .lineLimit(1)
                                .truncationMode(.tail)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            label(open)
                        }
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityElement(children: .combine)
                    .accessibilityHint(String(localized: "Shows the whole report [Folded Report]"))
                    .accessibilityIdentifier("report.fold")
                }
            }
        } else {
            full()
        }
    }

    private func toggle(_ open: Bool) -> some View {
        Button { flip(open) } label: { label(open) }
            .buttonStyle(.plain)
            .accessibilityIdentifier("report.collapse")
    }

    private func label(_ open: Bool) -> some View {
        Text(open ? String(localized: "Collapse [Folded Report]") : String(localized: "Expand [Folded Report]"))
            .font(Theme.Font.captionMedium)
            .foregroundStyle(Theme.accent)
            .lineLimit(1)
            .fixedSize()
    }

    private func flip(_ open: Bool) {
        Haptics.tap()
        // The card's top stays where it is in the chat while it grows or shrinks under it.
        toggleInPlace(pin, id, animation: Theme.Motion.state, closing: open) { reports.set(key, !open) }
    }
}

extension PeerMessage {
    /// A subagent of this session handing its report back, shown as another session's message (by the name it came
    /// with: the subagent's id, type or description): its words fold like a report.
    nonisolated static func isHandback(from: String, agents: some Collection<Subagent>) -> Bool {
        let f = from.trimmingCharacters(in: .whitespaces)
        guard !f.isEmpty else { return false }
        return agents.contains { a in a.id == f || a.type == f || a.description == f }
    }
}
