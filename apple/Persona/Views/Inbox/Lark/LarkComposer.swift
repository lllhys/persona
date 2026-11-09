import SwiftUI

extension EnvironmentValues {
    /// Reply in a message's long-press menu; set by the chat and the thread sheet. Nil: no Reply item.
    @Entry var larkReply: LarkReplyHandler? = nil
    /// A reply's quote was tapped: the chat goes to the message it answers and says whether it could.
    @Entry var larkJump: ((String) -> Bool)? = nil
}

/// What Reply does where the message is shown: the page says whether it is a topic group or a thread sheet, and takes
/// the message to answer (the chat puts up its quote bar, the thread sheet focuses its box).
struct LarkReplyHandler {
    var inTopicGroup = false
    var inThread = false
    var answer: (LarkMessage) -> Void
}

/// The box under a Feishu chat or thread: the text field and the send button, with the quote bar of the message
/// being answered above it. Like Feishu, the send button is only there once there is something to send; until then
/// the field takes the whole row. In a group, typing @ lists the members to mention (LarkMentionPicker).
struct LarkComposer: View {
    @Environment(AppModel.self) private var model
    @Environment(\.larkAccount) private var larkAccount
    @Environment(\.larkManager) private var larkManager
    var placeholder: String
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    var replyTo: LarkReplyTo? = nil
    var cancelReply: () -> Void = {}
    /// Another quote over the field, where there is no message being answered (a document's passage being commented on):
    /// its heading, what it quotes, and what × does.
    var quote: (heading: String, summary: String, removeLabel: String, remove: () -> Void)? = nil
    /// Tell the chat's box from a thread sheet's (UI tests; the chat stays behind the sheet). The field's placeholder
    /// goes once it holds text, so tests find it by its id.
    var boxID = "lark.box"
    var sendID = "lark.send"
    var attachmentPicker: ((Bool) -> Void)? = nil
    var canAddAttachments = true
    var hasAttachments = false
    var preparingAttachment = false
    /// The group whose members @ offers; nil in a 1:1 chat (no picker).
    var mentionFeed: String? = nil
    /// Where @ finds people when there is no group to take them from (a document's comment): by what follows the @.
    var findPeople: ((String) async throws -> [LarkMember])? = nil
    /// My user id there (I am not offered).
    var me: String? = nil
    /// Those picked so far; what is sent keeps the ones whose `@name` is still in the text (LarkMentions).
    var mentions: Binding<[LarkMention]> = .constant([])
    var send: () -> Void
    /// The members matching the @ being typed, and whether the hub answered for it (an older hub without the route
    /// does not: no picker, the box works as before).
    @State private var members: [LarkMember] = []
    @State private var membersAnswered = false
    /// Where the cursor is: an @ is looked up, and a picked name goes in, there (not only at the end).
    @State private var selection: TextSelection?

    /// The @ being typed before the cursor, in a group. Before the field reports a cursor it is the end of the text;
    /// while a range is selected no @ is being typed.
    private var typing: LarkMentions.Typing? {
        guard mentionFeed != nil || findPeople != nil else { return nil }
        guard let selection else { return LarkMentions.typing(in: text) }
        guard case .selection(let r) = selection.indices, r.isEmpty else { return nil }
        let at = min(r.lowerBound, text.endIndex) // the text may have changed since (a send, a draft put back)
        return LarkMentions.typing(in: text, cursor: text.distance(from: text.startIndex, to: at))
    }

    private var mentionQuery: String? { typing?.query }

    /// Attachments go with a quote too when the hub can carry it (#1035); when it cannot, sending says so and keeps the draft.
    private var showsSend: Bool { Self.canSend(text) || hasAttachments }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let q = mentionQuery, membersAnswered, !members.isEmpty || (mentionFeed != nil && LarkMentions.offersEveryone(q)) {
                LarkMentionPicker(query: q, members: members, everyone: mentionFeed != nil) { m in
                    let picked = LarkMentions.insert(m, into: text, cursor: typing?.end)
                    text = picked.text
                    selection = .cursor(utf16: text[..<text.index(text.startIndex, offsetBy: picked.cursor)].utf16.count)
                    mentions.wrappedValue.append(m)
                }
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                if let attachmentPicker {
                    Menu {
                        Button { attachmentPicker(true) } label: { Label("Photos", systemImage: Theme.Icon.photos).lineLimit(1) }
                        Button { attachmentPicker(false) } label: { Label("Files", systemImage: Theme.Icon.file).lineLimit(1) }
                    } label: {
                        Image(systemName: Theme.Icon.add).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                    }
                    .disabled(preparingAttachment || !canAddAttachments)
                    .accessibilityLabel("Add attachment")
                }
                // The message answered is quoted inside the box, over the field (Telegram's reply, #986).
                VStack(alignment: .leading, spacing: 0) {
                    if let replyTo {
                        LarkReplyBar(reply: replyTo, cancel: cancelReply)
                            .padding(.leading, Theme.Space.l)
                            .padding(.trailing, Theme.Space.xs)
                            .padding(.top, Theme.Space.s)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    } else if let quote {
                        QuoteBarRow(heading: quote.heading, summary: quote.summary, lines: 1, tint: LarkColor.blue,
                                    removeLabel: quote.removeLabel, remove: quote.remove)
                            .accessibilityIdentifier(boxID + ".quote")
                            .padding(.leading, Theme.Space.l)
                            .padding(.trailing, Theme.Space.xs)
                            .padding(.top, Theme.Space.s)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    TextField(placeholder, text: $text, selection: $selection, axis: .vertical)
                        .lineLimit(1...5)
                        .focused(focused)
                        .accessibilityIdentifier(boxID)
                        .padding(.horizontal, Theme.Space.l)
                        .padding(.vertical, Theme.Space.s)
                }
                .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                if showsSend {
                    Button(action: send) {
                        Image(systemName: Theme.Icon.send).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                    }
                    .themedButton(prominent: true)
                    .clipShape(.circle)
                    .disabled(preparingAttachment)
                    .accessibilityLabel("Send")
                    .accessibilityIdentifier(sendID)
                    .transition(.scale.combined(with: .opacity))
                }
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .animation(Theme.Motion.standard, value: replyTo)
        .animation(Theme.Motion.standard, value: showsSend)
        .animation(Theme.Motion.standard, value: mentionQuery == nil)
        .task(id: mentionQuery) { await lookUp(mentionQuery) }
    }

    /// Asks the hub for members a moment after the last keystroke after @.
    private func lookUp(_ query: String?) async {
        guard let query, mentionFeed != nil || findPeople != nil else {
            membersAnswered = false
            return
        }
        try? await Task.sleep(for: .milliseconds(200))
        guard !Task.isCancelled else { return }
        do {
            let found: [LarkMember]
            if let findPeople {
                found = try await findPeople(query)
            } else if let feed = mentionFeed, let api = model.api?.manager(larkManager).lark(larkAccount) {
                found = try await api.larkMembers(feed, query: query).members
            } else {
                return
            }
            members = found.filter { $0.userID != me }
            membersAnswered = true
        } catch {
            guard !error.isCancellation else { return }
            members = []
            membersAnswered = false
        }
    }

    /// Words to send: blank lines and spaces alone are not.
    static func canSend(_ text: String) -> Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

/// "Reply <name>" at the top of the box: who is answered, a line of what they wrote, and × to drop it (`QuoteBarRow`).
struct LarkReplyBar: View {
    var reply: LarkReplyTo
    var cancel: () -> Void

    var body: some View {
        QuoteBarRow(heading: String(localized: "Replying to \(reply.senderName)"), summary: Fmt.plain(reply.displayText), lines: 1, tint: LarkColor.blue,
                    removeLabel: String(localized: "Cancel Reply"), remove: cancel)
            .accessibilityIdentifier("lark.replyBar")
    }
}
