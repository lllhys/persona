#if os(iOS)
import SwiftUI

// Select Messages (#3838, #9468, docs/apple/ios.md "Forwarding a message"): the long-press bar's Select Messages row, or
// Forward To's Select Messages…, puts the chat into selecting, the pressed message already picked. Each message gets a
// round mark on its leading edge and a tap anywhere on it picks or unpicks it; the composer gives way to a bar with
// Cancel, the count, Include Questions when it means something, and Forward · Copy · Export Image. Cancel leaves the chat
// as it was. The marks read the picked set themselves, so a tap redraws its mark and the bar, never the chat.

/// The chat's selecting state: `on` while selecting (the chat reads only this), `ids` the picked messages' item ids,
/// `include` whether picked replies bring their questions (#9468: on by default, remembered).
@Observable final class MessagePick {
    var on = false
    var ids: Set<String> = []
    /// Read as selecting starts, not as the chat is made (docs/apple/performance.md P3); written when it is changed.
    var include = true
    /// Each reply's questions (`PickQuestions.map`), read from the chat as selecting starts and at each pick.
    var questions: [String: [String]] = [:]
    @ObservationIgnored var readQuestions: () -> [String: [String]] = { [:] }

    static let includeKey = "pickIncludeQuestions"

    /// Starts selecting with `id` picked.
    func start(_ id: String) {
        include = UserDefaults.standard.object(forKey: Self.includeKey) as? Bool ?? true
        questions = readQuestions()
        ids = [id]
        on = true
    }

    func end() {
        on = false
        ids = []
    }

    /// The questions that go with the picked replies, not picked themselves; none with Include Questions off.
    var brought: Set<String> { include ? PickQuestions.brought(ids, questions: questions) : [] }

    /// Include Questions means something now: a picked reply's question is not picked.
    var offersQuestions: Bool { PickQuestions.offered(ids, questions: questions) }

    /// The messages that go: the picked ones and the questions they bring.
    var going: Set<String> { ids.union(brought) }

    /// Picks or unpicks `id`; false when more would go than one forward takes.
    func toggle(_ id: String) -> Bool {
        questions = readQuestions()
        if ids.remove(id) != nil { return true }
        var next = ids
        next.insert(id)
        let count = include ? next.union(PickQuestions.brought(next, questions: questions)).count : next.count
        guard count <= ForwardMessage.most else { return false }
        ids = next
        return true
    }

    /// Include Questions on or off; false when turning it on would take more than one forward takes.
    func setInclude(_ on: Bool) -> Bool {
        if on, ids.union(PickQuestions.brought(ids, questions: questions)).count > ForwardMessage.most { return false }
        include = on
        UserDefaults.standard.set(on, forKey: Self.includeKey)
        return true
    }
}

extension ChatRow {
    /// The message a row stands for while selecting, as its long-press bar has it; nil for rows that are not messages
    /// (an injected line, a /loop tick, thinking, steps, notices, pictures alone).
    var pickBar: MessageBarInfo? {
        switch self {
        case .user(let i):
            if let source = MessageSource.of(i) {
                return MessageBarInfo(id: i.id, text: i.text ?? "", isUser: false, date: i.ts, pending: false, from: source.mention)
            }
            if Injected.of(i) != nil || (i.text ?? "").hasPrefix("/loop") { return nil }
            if let from = i.from { return MessageBarInfo(id: i.id, text: i.text ?? "", isUser: false, date: i.ts, pending: false, from: from) }
            return MessageBarInfo(id: i.id, text: i.text ?? "", isUser: true, date: i.ts, pending: false)
        case .assistant(let i):
            return MessageBarInfo(id: i.id, text: i.text ?? "", isUser: false, date: i.ts, pending: false)
        default:
            return nil
        }
    }
}

extension MessageBarInfo {
    /// What VoiceOver reads for the message's row while selecting: its words on one line, the first 200 characters.
    var pickLabel: String {
        let words = text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        return words.count > 200 ? String(words.prefix(200)) + "…" : words
    }
}

/// A message's mark while selecting: filled and in the accent when picked, an empty ring when not (the Inbox's Select);
/// a question a picked reply brings along (#9468) is filled in a lighter accent.
struct PickMark: View {
    let pick: MessagePick
    let id: String

    var body: some View {
        let on = pick.ids.contains(id)
        let brought = !on && pick.brought.contains(id)
        ThemedIcon(on || brought ? Theme.Icon.successFilled : Theme.Icon.unchecked)
            .font(Theme.Font.heading)
            .foregroundStyle(on ? AnyShapeStyle(Theme.accent) : brought ? AnyShapeStyle(Theme.accent.tertiary) : AnyShapeStyle(Color.secondary))
            .scaledFrame(height: Theme.Size.avatar)
            .accessibilityHidden(true)
    }
}

extension View {
    /// A chat row while selecting: a message gets its mark on the leading edge and picks or unpicks with a tap anywhere
    /// on it; nothing inside it (links, cards, long-press) answers. Not selecting, the row as it is. `mark`: false for a
    /// long answer's later parts (#4970), which keep the mark's room and pick the answer, and are one element with it.
    @ViewBuilder func pickable(_ bar: MessageBarInfo?, mark: Bool = true, pick: MessagePick, selecting: Bool, refused: @escaping () -> Void) -> some View {
        if !selecting {
            self
        } else if let bar {
            PickRow(pick: pick, id: bar.id, label: bar.pickLabel, mark: mark, refused: refused) { self }
        } else {
            allowsHitTesting(false)
        }
    }
}

private struct PickRow<Content: View>: View {
    let pick: MessagePick
    let id: String
    /// What VoiceOver reads (`MessageBarInfo.pickLabel`). Set here, since a bubble's text can be a UIKit view that
    /// `.combine` leaves as a separate element, which would leave the row unnamed.
    let label: String
    let mark: Bool
    var refused: () -> Void
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.s) {
            PickMark(pick: pick, id: id).opacity(mark ? 1 : 0)
            content.allowsHitTesting(false)
        }
        .padding(.leading, Theme.Space.page)
        .contentShape(.rect)
        .onTapGesture {
            if pick.toggle(id) { Haptics.tap() } else { refused() }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityValue(!pick.ids.contains(id) && pick.brought.contains(id) ? String(localized: "Goes with its answer") : "")
        .accessibilityAddTraits(pick.ids.contains(id) ? [.isButton, .isSelected] : .isButton)
        .accessibilityIdentifier("chat.pick." + id)
        .accessibilityHidden(!mark)
    }
}

/// In the composer's place while selecting (#9468): Cancel, how many are picked and Include Questions when it means
/// something; under them Forward · Copy · Export Image, drawn as the long-press bar's actions, off with none picked.
struct PickBar: View {
    let pick: MessagePick
    var cancel: () -> Void
    var forward: () -> Void
    var copy: () -> Void
    var export: () -> Void
    /// Include Questions would take more messages than one forward takes.
    var refused: () -> Void

    var body: some View {
        let n = pick.ids.count
        VStack(spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.m) {
                Button("Cancel", action: cancel)
                    .themedButton()
                    .lineLimit(1)
                    .accessibilityIdentifier("chat.pickCancel")
                    .frame(maxWidth: .infinity, alignment: .leading)
                Text("\(n) Selected [Selected Messages]").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    .accessibilityIdentifier("chat.pickCount")
                Group {
                    if pick.offersQuestions { questions }
                }
                .frame(maxWidth: .infinity, alignment: .trailing)
            }
            HStack(spacing: Theme.Space.flush) {
                action(String(localized: "Forward [Message Menu]"), Theme.Icon.forwardMessage, "chat.pickForward", forward)
                action(String(localized: "Copy [Message Menu 63d90d]"), Theme.Icon.copy, "chat.pickCopy", copy)
                action(String(localized: "Export Image"), Theme.Icon.photo, "chat.pickExport", export)
            }
            .disabled(n == 0)
            .dynamicTypeSize(...MessageActionBar.barTypeSize)
            .surface(in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.s)
        .transition(.move(edge: .bottom).combined(with: .opacity))
    }

    /// Include Questions: a chip that turns it on and off, ticked while on.
    private var questions: some View {
        Button {
            if pick.setInclude(!pick.include) { Haptics.tap() } else { refused() }
        } label: {
            Chip(text: String(localized: "Include Questions"), symbol: pick.include ? Theme.Icon.successFilled : Theme.Icon.unchecked,
                 look: pick.include ? .tint(Theme.accent) : .outline)
        }
        .buttonStyle(.plain)
        .accessibilityValue(pick.include ? String(localized: "On") : String(localized: "Off"))
        .accessibilityAddTraits(pick.include ? [.isButton, .isSelected] : .isButton)
        .accessibilityIdentifier("chat.pickQuestions")
    }

    /// One action as the long-press bar draws it: its icon over its word, in the accent.
    private func action(_ title: String, _ symbol: String, _ id: String, _ perform: @escaping () -> Void) -> some View {
        Button(action: perform) { PickBarAction(title: title, symbol: symbol) }
            .buttonStyle(.plain)
            .accessibilityLabel(title)
            .accessibilityShowsLargeContentViewer { ThemedIcon(symbol); Text(title) }
            .accessibilityIdentifier(id)
    }
}

/// A selecting bar action's face: the accent, or the quiet ink while it is off.
private struct PickBarAction: View {
    @Environment(\.isEnabled) private var enabled
    let title: String
    let symbol: String

    var body: some View {
        VStack(spacing: Theme.Space.xs) {
            ThemedIcon(symbol).font(Theme.Font.heading).scaledFrame(height: Theme.Size.iconS)
            Text(title).font(Theme.Font.label).lineLimit(1).minimumScaleFactor(0.8)
        }
        .frame(maxWidth: .infinity)
        .frame(height: MessageActionBar.height)
        .foregroundStyle(enabled ? AnyShapeStyle(Theme.accent) : AnyShapeStyle(HierarchicalShapeStyle.tertiary))
        .contentShape(.rect)
    }
}
#endif
