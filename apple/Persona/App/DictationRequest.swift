import Foundation

/// A Dictate to Persona control's request, held only until its new conversation's composer takes it.
nonisolated struct DictationRequest: Hashable, Sendable {
    let id: UUID

    init(id: UUID = UUID()) {
        self.id = id
    }
}
