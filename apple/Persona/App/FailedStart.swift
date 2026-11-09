import Foundation
import Observation

/// A new conversation's first message that did not start one (#1368): it stays on the new-conversation page, drawn as
/// the chat draws a message that did not go (`PendingBubble`, failed), with the reason, Resend and Delete, and the
/// composer stays empty. Kept across launches, like the outbox (`StateCache`); one per page it was sent from (Health's,
/// each manager's Persona page), since a page takes no other first message while its own is there.
@MainActor @Observable
final class FailedStart {
    static let shared = FailedStart()

    nonisolated struct Message: Codable, Equatable, Sendable {
        var text: String
        var images: [ImagePayload]
        /// The space it was started in (Health's), nil for Persona's own.
        var space: String?
        /// The op id it went with (`AssistantStart`).
        var op: UUID
        /// It is unclear whether the conversation started (cut off on the way, `202 unknown`, no answer at all):
        /// Resend asks the hub about `op` before choosing an op id.
        var mayHaveStarted: Bool
        /// Why, for the line over the composer; nil when the readiness card says it (`CodedFailure`).
        var reason: String?
        var at: Date
        /// The manager whose Persona page it was sent from (`AppModel.assistantManagerID`), nil for the default's: that
        /// page alone shows it and sends it again.
        var manager: String? = nil

        func samePage(_ o: Message) -> Bool { space == o.space && (space != nil || manager == o.manager) }
    }

    private static let name = "assistant-failed-start"
    /// Oldest first.
    private(set) var messages: [Message]

    private init() { messages = StateCache.load(Self.name, as: [Message].self) ?? [] }

    /// Holds `m` in place of the one from its page.
    func fail(_ m: Message) { set(messages.filter { !$0.samePage(m) } + [m]) }
    func clear(where gone: (Message) -> Bool) { if messages.contains(where: gone) { set(messages.filter { !gone($0) }) } }

    private func set(_ ms: [Message]) {
        messages = ms
        StateCache.save(Self.name, ms) // saved, not removed: two writes stay in order of arrival
    }

    /// Whether `error` leaves the start possibly done: the hub may have started it though the app was told otherwise.
    nonisolated static func mayHaveStarted(_ error: Error) -> Bool {
        if let e = error as? APIError {
            if e.fields["delivered"] == "unknown" || e.code == OpReceipt.unknownCode || e.code == OpReceipt.pendingCode { return true }
            if e.code != nil || (400..<500).contains(e.status) { return false }
        }
        return error.isGap || error.isOffline || error.isTimeout
    }

    /// The op id Resend goes with, as a chat's Resend chooses (`AppModel.retry`): when it may have started, the hub is
    /// asked about the old id first; done (its answer is the conversation) or still running keeps it, anything else is
    /// a new one, since the old id would be answered with its first outcome.
    static func resendOp(_ m: Message, api: APIClient, receipts: Bool) async -> UUID {
        guard receipts, m.mayHaveStarted, let state = try? await api.opState(m.op) else { return UUID() }
        return state == "done" || state == "pending" ? m.op : UUID()
    }
}
