import Foundation

/// The cards being answered right now (`AppModel.resolve`), and which of them the hub ended while the answer was on
/// its way (#3922). A screen card whose dialog changed (409 `prompt_changed`) or whose keys did not reach the computer
/// is over on the hub: it says `interaction_resolved` before its error comes back, and a card for what is on the screen
/// now follows. Put back after that event, the old card stayed for good. A Codex card the node could not answer stays
/// pending on the hub, without that event, so it goes back.
nonisolated struct AnsweringInteractions {
    private var inFlight: Set<String> = []
    private var endedByHub: Set<String> = []

    mutating func begin(_ uid: String) {
        inFlight.insert(uid)
        endedByHub.remove(uid)
    }

    /// The hub said this card is over.
    mutating func hubEnded(_ uid: String) {
        if inFlight.contains(uid) { endedByHub.insert(uid) }
    }

    /// This card's answer is on its way.
    func contains(_ uid: String) -> Bool { inFlight.contains(uid) }

    /// Whether a failed answer puts the card back: not when the hub ended it meanwhile.
    func putsBack(_ uid: String) -> Bool { !endedByHub.contains(uid) }

    mutating func finish(_ uid: String) {
        inFlight.remove(uid)
        endedByHub.remove(uid)
    }
}

/// What a question card shows while its answer is on its way (#6279): the card stays where it is, greyed, until the hub
/// has the answer; a failed answer leaves it with its picks and the error. One op id per answer: the same answer sent
/// again (Retry) goes with the id it had, so the hub does it once; a different answer is a new one.
nonisolated struct AnswerAttempts {
    enum Progress: Equatable {
        case sending
        case failed(String)
    }

    private struct Attempt {
        var op: UUID
        var resolution: Resolution
        var progress: Progress
    }

    private var attempts: [String: Attempt] = [:]

    /// Starts sending `resolution` for the card; nil when its answer is already on its way (a second tap sends nothing).
    mutating func begin(_ uid: String, _ resolution: Resolution, newOp: () -> UUID = UUID.init) -> UUID? {
        if let a = attempts[uid] {
            if a.progress == .sending { return nil }
            if a.resolution == resolution {
                attempts[uid]?.progress = .sending
                return a.op
            }
        }
        let op = newOp()
        attempts[uid] = Attempt(op: op, resolution: resolution, progress: .sending)
        return op
    }

    /// The answer failed: the card keeps the error until the next try.
    mutating func fail(_ uid: String, _ message: String) {
        guard attempts[uid] != nil else { return }
        attempts[uid]?.progress = .failed(message)
    }

    /// The card is over (answered, ended by the hub or gone): nothing more to remember.
    mutating func finish(_ uid: String) { attempts[uid] = nil }

    func progress(_ uid: String) -> Progress? { attempts[uid]?.progress }

    /// The answer a failed card sends again.
    func retryable(_ uid: String) -> Resolution? {
        guard let a = attempts[uid], case .failed = a.progress else { return nil }
        return a.resolution
    }
}
