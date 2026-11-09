import Foundation

/// New Chat takes the Persona page to its blank new-conversation page. On TestFlight 213 a user's tap in a long main
/// conversation left the page as it was, and no simulator showed it (#2189). This watches each New Chat taken from a
/// conversation: when no new-conversation page has appeared a moment later, it writes one line of labels (where the tap
/// came from, what the current conversation is, whether the page drew the new one) and hands it on. No content, no ids.
@MainActor final class NewChatCheck {
    static let shared = NewChatCheck()
    /// Where the latest miss is kept on the device.
    nonisolated static let defaultsKey = "diag.newChat"

    /// How long the page has to draw the new-conversation page, and how much longer it is given before the line is written.
    private let grace: Duration
    private let more: Duration
    /// Times a new-conversation page appeared (`ShellHome`, `AssistantHome`).
    private var homeAppearances = 0
    private var watching: Task<Void, Never>?

    init(grace: Duration = .milliseconds(1500), more: Duration = .milliseconds(3500)) {
        self.grace = grace
        self.more = more
    }

    /// A new-conversation page appeared.
    func homeAppeared() { homeAppearances += 1 }

    /// New Chat was taken from `from` (`main` / `other`). `current` names the current conversation now (`main` / `other` /
    /// `none`); `miss` gets the line when the new page did not come within `grace`.
    func watch(from: String, current: @escaping @MainActor () -> String, miss: @escaping @MainActor (String) -> Void) {
        let seen = homeAppearances
        watching?.cancel()
        watching = Task { @MainActor [grace, more] in
            try? await Task.sleep(for: grace)
            guard !Task.isCancelled, self.homeAppearances == seen else { return }
            let early = current()
            try? await Task.sleep(for: more)
            guard !Task.isCancelled else { return }
            let drawn = self.homeAppearances == seen ? "chat" : "home"
            miss("new_chat_check from=\(from) current_early=\(early) current_late=\(current()) drawn_late=\(drawn)")
        }
    }

    /// `key` against the main conversation, as the line names it.
    static func name(_ key: SessionKey?, main: SessionKey?) -> String {
        key == nil ? "none" : key == main ? "main" : "other"
    }
}
