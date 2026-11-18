import Foundation

/// Where a message sent from this phone is drawn until the transcript has it (#1461): one written while the agent
/// worked (the composer said "queued") goes into the queue strip at once, as sending, waiting or failed, so it does not
/// show in the chat first and jump to the strip once Claude reports it queued. One written while the agent was idle
/// stays a bubble in the chat.
nonisolated enum QueuePlacement {
    /// Whether the message is drawn in the strip: it was written in queue mode, and the agent is still working or the
    /// send failed (a failed one stays where it was drawn, with its Resend). Once the agent is idle again a message
    /// that has gone out is an ordinary one: no turn will queue it, so it is a bubble until the transcript has it.
    static func inStrip(_ message: PendingMessage, working: Bool) -> Bool {
        guard message.queuedMode == true else { return false }
        return working || message.state == .failed
    }

    /// The messages the strip draws (after the ones Claude reports queued) and the ones the chat draws. A message whose
    /// words Claude already holds in its queue is in neither: the strip's confirmed rows are it.
    static func split(_ pending: [PendingMessage], working: Bool, queued: [String], stripShown: Bool)
        -> (strip: [PendingMessage], chat: [PendingMessage]) {
        var strip: [PendingMessage] = [], chat: [PendingMessage] = []
        for p in pending where !queued.contains(where: { PendingMessage.same($0, p.text) }) {
            if stripShown, inStrip(p, working: working) { strip.append(p) } else { chat.append(p) }
        }
        return (strip, chat)
    }
}
