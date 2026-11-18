import Foundation

/// Full-history count/position metadata, independent of the loaded transcript. It contains no message bodies.
public nonisolated struct ChatUserInputIndex: Decodable, Equatable, Sendable {
    public init(total: Int, inputs: [Position], history: String) {
        self.total = total
        self.inputs = inputs
        self.history = history
    }

    public nonisolated struct Position: Decodable, Equatable, Sendable {
        public var id: String
        public var seq: Int

        public init(id: String, seq: Int) {
            self.id = id
            self.seq = seq
        }
    }

    public var total: Int
    public var inputs: [Position]
    public var history: String

    public var isValid: Bool {
        guard total == inputs.count else { return false }
        var ids = Set<String>()
        var previous = 0
        for input in inputs {
            guard !input.id.isEmpty, input.seq > 0, input.seq >= previous, ids.insert(input.id).inserted else { return false }
            previous = input.seq
        }
        return true
    }
}
