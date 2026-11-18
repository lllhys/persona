import Foundation

/// The `client_msg_id` of a message sent again after a failure (#1185): the same as the failed try's, so the hub types
/// it at most once (a try that reached the computer before the answer was lost is not sent twice). Once it went
/// through, the same text again is a new message with a new id.
public nonisolated struct RetryIDs<What: Hashable> {
    public init() {}

    private var pending: [What: String] = [:]

    /// The id to send `what` with: its last failed try's, else a new one (kept until `sent`).
    public mutating func id(for what: What) -> String {
        if let id = pending[what] { return id }
        let id = UUID().uuidString
        pending[what] = id
        return id
    }

    /// It went through: the next one is a new message.
    public mutating func sent(_ what: What) { pending[what] = nil }
}
