import SwiftUI

// What Persona sent to its tasks' agents (`routed` items it wrote, #1542, docs/assistant/unified-entry.md "Messages to agents"):
// never the user's bubble. A message on its own is one folded line on the left (`AgentSentLine`); the hand-off sent
// together with a task's card is drawn like any other (nothing applies `SentToAgents.takenIn` now, docs/design/task-cards.md §5.1).

/// One of Persona's messages to a task's agent in the conversation: a capsule like the answered and review lines, with
/// its mark (a stop when Persona cut in first), where it went and the first line. A tap opens the whole text with Copy,
/// and again folds it; the task's name opens the task.
struct AgentSentLine: View {
    var item: FeedItem
    var taskTitle: String
    @State private var open = false
    @State private var unfold = Unfolding()
    /// Where the text ends on screen, to bring it into view once open (`ChatPin.show`).
    @State private var bottom: CGFloat = 0
    @Environment(\.chatPin) private var pin
    private var store: TasksStore { .shared }
    private var pinKey: String { "sent-" + item.id }

    var body: some View {
        let text = item.text ?? ""
        VStack(alignment: .leading, spacing: 0) {
            header(first: ExchangeText.plain(SentToAgents.firstLine(text)))
            if open {
                AgentSentText(text: text)
                    .padding(.horizontal, Theme.Space.m)
                    .padding(.bottom, Theme.Space.m)
                    .accessibilityIdentifier("sent-detail")
                    .unfolding(unfold)
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .frame(maxWidth: open ? .infinity : nil, alignment: .leading)
        .onGeometryChange(for: CGFloat.self, of: { $0.frame(in: .global).maxY }) { bottom = $0 }
        .background(Theme.Fill.chip, in: open ? AnyShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous)) : AnyShape(.capsule))
        .frame(maxWidth: .infinity, alignment: .leading)
        .chatPinned(pinKey)
    }

    /// "Sent to “task”", or "Stopped “task” and sent" when Persona cut in first.
    static func title(_ task: String, interrupted: Bool) -> String {
        interrupted ? String(localized: "Stopped “\(task)” and sent") : String(localized: "Sent to “\(task)”")
    }

    private func header(first: String) -> some View {
        HStack(spacing: Theme.Space.s) {
            ThemedIcon(item.interrupted ? Theme.Icon.interrupt : Theme.Icon.routeToTask).font(Theme.Font.labelStrong)
            HStack(spacing: Theme.Space.xs) {
                if let id = item.ref, store.todos[id] != nil {
                    Button { store.detailTask = id } label: { Text(Self.title(taskTitle, interrupted: item.interrupted)).lineLimit(1) }
                        .buttonStyle(.plain)
                } else {
                    Text(Self.title(taskTitle, interrupted: item.interrupted)).lineLimit(1)
                }
                if !first.isEmpty { Text("· \(first)").lineLimit(1) }
            }
            ThemedIcon(Theme.Icon.disclosure)
                .font(Theme.Font.labelStrong)
                .foregroundStyle(.tertiary)
                .rotationEffect(.degrees(open ? 90 : 0))
                .accessibilityHidden(true)
        }
        .font(Theme.Font.caption)
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .contentShape(.rect)
        .onTapGesture { toggle() }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("sent-line")
        .accessibilityValue(open ? String(localized: "Expanded") : String(localized: "Collapsed [Assistant Sidebar e64447]"))
        .accessibilityAction(named: open ? String(localized: "Collapse") : String(localized: "Expand")) { toggle() }
        .accessibilityAction(named: "Open Task") { if let id = item.ref, store.todos[id] != nil { store.detailTask = id } }
    }

    private func toggle() {
        Haptics.tap()
        let opening = !open
        toggleInPlace(pin, pinKey, animation: Theme.Motion.state, unfold: $unfold, closing: open) { open.toggle() }
        #if os(iOS) || os(visionOS)
        // The line as the chat's last row: the text opens under it, past the composer.
        guard opening, let pin else { return }
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(Theme.Motion.revealTime))
            if open { pin.show(pinKey, bottom: bottom) }
        }
        #endif
    }
}

/// The whole text Persona sent, newlines kept, with Copy under it. Not `.textSelection`: right under the line's header it
/// took the header's taps, so the line would not fold (docs/lessons/ios.md).
struct AgentSentText: View {
    var text: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text(ExchangeText.linked(text)) // Markdown rendered; Copy keeps the text as sent (#2363)
                .font(Theme.Font.secondary)
                .foregroundStyle(.primary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
            Button {
                UIPasteboard.general.string = text
                Haptics.success()
                copied = true
                AccessibilityNotification.Announcement(String(localized: "Copied [V N C Pi P Diagnostics 8f6f8d]")).post()
            } label: {
                if copied {
                    Label("Copied [V N C Pi P Diagnostics 8f6f8d]", systemImage: Theme.Icon.done).lineLimit(1)
                } else {
                    Label("Copy [Message Menu 63d90d]", systemImage: Theme.Icon.copy).lineLimit(1)
                }
            }
            .font(Theme.Font.captionStrong)
            .foregroundStyle(Theme.accent)
            .buttonStyle(.plain)
            .accessibilityIdentifier("sent-copy")
            .task(id: copied) {
                guard copied else { return }
                do { try await Task.sleep(for: .seconds(2)) } catch { return }
                copied = false
            }
        }
    }
}

