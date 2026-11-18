import SwiftUI

/// A quiet round keeps its full transcript, but occupies only one muted line until requested. With a `source` it is a
/// background round of the main conversation (docs/apple/ios.md "Background rounds"): "@<source> · Reported from the
/// background". The words say plainly what happened in the background (#6230), the same copy on every client.
struct SupervisionRecordLine: View {
    var live: Bool
    var open: Bool
    var source: MessageSource? = nil
    var toggle: () -> Void

    private var title: String {
        guard let source else {
            return live ? String(localized: "Supervision record · running") : String(localized: "Supervision record")
        }
        let state = live ? String(localized: "Running in the background") : String(localized: "Background round")
        return source.mention + " · " + state
    }

    var body: some View {
        Button(action: toggle) {
            HStack(spacing: Theme.Space.s) {
                ThemedIcon(Theme.Icon.disclosure)
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .accessibilityHidden(true)
                Text(verbatim: title)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
            .font(Theme.Font.caption)
            .foregroundStyle(.secondary)
            .padding(.vertical, Theme.Space.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier(source == nil ? "supervision-record" : "background-round")
        .accessibilityValue(open ? String(localized: "Expanded") : String(localized: "Collapsed"))
    }
}

/// Unlike an injected hint, the opened audit trail exposes the original model input verbatim.
struct SupervisionInputLine: View {
    var item: Item
    @Environment(\.loadFullText) private var loadFullText
    @State private var full: String?
    @State private var showFull = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text("Supervision input")
                .font(Theme.Font.caption)
                .foregroundStyle(.secondary)
            Text(full ?? item.cutText)
                .font(Theme.Font.secondary)
                .foregroundStyle(.primary)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            if item.fullSize != nil, full == nil {
                Button("Show Full Text") { showFull = true }
                    .font(Theme.Font.caption)
                    .lineLimit(1)
                    .sheet(isPresented: $showFull) { FullOutputSheet(itemID: item.id, load: loadFullText) }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .chatBoxed(Theme.ChatBox.persona)
        .accessibilityIdentifier("supervision-input")
        .task(id: item.id) {
            if item.fullSize != nil, let loadFullText { full = try? await loadFullText(item.id) }
        }
    }
}
