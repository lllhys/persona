import Foundation

/// What a first-message completion may change after awaiting the hub. Accepted results still count as begun; only the
/// visible page, feedback and failure UI become stale when a newer New Chat has taken over.
nonisolated struct AssistantStartDecision: Equatable, Sendable {
    enum Haptic: Equatable, Sendable { case success, error }
    /// `record` keeps another manager's failure for its own page; `none` drops a stale completion's failure UI.
    enum FailureAction: Equatable, Sendable { case none, clear, show, record }

    let began: Bool
    let open: Bool
    let haptic: Haptic?
    let clearPending: Bool
    let failure: FailureAction

    init(startGeneration: Int, currentGeneration: Int, pendingOp: UUID?, op: UUID,
         succeeded: Bool, space: String?, showsPage: Bool) {
        let current = startGeneration == currentGeneration
        let here = space != nil || showsPage
        began = succeeded
        open = succeeded && current && here
        haptic = current ? (succeeded ? .success : .error) : nil
        clearPending = pendingOp == op
        if succeeded {
            failure = current ? .clear : .none
        } else if !current {
            failure = .none
        } else {
            failure = here ? .show : .record
        }
    }
}
