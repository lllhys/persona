import Foundation

/// Results derived from a message's text, computed once per distinct text. Chat rows redraw all the time while
/// the list scrolls or streams; the regexes, link detectors and parsers behind these were the main thread's
/// hottest work (a profile of a long chat: ~15% of all main-thread time). Hashing the text is far cheaper.
public nonisolated final class TextMemo<Value>: @unchecked Sendable {
    private var values: [String: Value] = [:]
    private let lock = NSLock()
    private let limit: Int

    public init(limit: Int = 4000) { self.limit = limit }

    public func callAsFunction(_ text: String, _ make: (String) -> Value) -> Value {
        lock.lock()
        if let v = values[text] { lock.unlock(); return v }
        lock.unlock()
        let v = make(text)
        lock.lock()
        if values.count >= limit { values.removeAll(keepingCapacity: true) } // a long session's worth; then start over
        values[text] = v
        lock.unlock()
        return v
    }
}

/// Keys for memos of a chat's items. An item id is only unique within its session — a runtime's are "<seq>#<block>",
/// and two branches of one DSH session share theirs up to where they part — so a memo shared by every chat keys an
/// item by its chat and its id (#372). Outside a chat (an empty scope) the id alone, as before.
public nonisolated enum MemoScope {
    /// The scope of the chat of machine `machine`, session `session`.
    public static func of(machine: String, session: String) -> String { machine + "/" + session }

    /// The memo key of item `id` in the chat of `scope`.
    public static func key(_ scope: String, _ id: String) -> String { scope.isEmpty ? id : scope + "\u{0}" + id }
}
