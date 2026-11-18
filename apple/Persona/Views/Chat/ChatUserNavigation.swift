import Foundation
import PersonaWire

@MainActor enum ChatUserNavigation {
    /// The initial navigator shows the latest 50 genuine inputs, not 50 transcript items or model turns.
    static let initialCount = 50

    /// Grow only when earlier history is read. The index, not a loaded page or a grouped bubble, owns the ordinal.
    static func marks(index: ChatUserInputIndex?, lines: [ChatLine], history: String?, revealingOlder: Bool) -> [EdgeMark] {
        let loaded = EdgeScrubState.marks(lines)
        guard let index, index.isValid, history == nil || index.history == history else { return loaded }
        let loadedBySequence = Dictionary(loaded.compactMap { mark in mark.seq.map { ($0, mark) } }, uniquingKeysWith: { first, _ in first })
        var first = max(0, index.total - initialCount)
        if revealingOlder, let older = index.inputs.firstIndex(where: { loadedBySequence[$0.seq] != nil }) {
            first = min(first, older)
        }
        return index.inputs.enumerated().dropFirst(first).map { offset, input in
            let loaded = loadedBySequence[input.seq]
            return EdgeMark(id: loaded?.id ?? input.id, text: loaded?.text ?? "", seq: input.seq, ordinal: offset + 1)
        }
    }

    /// Image-only inputs use an image-group line id rather than the hub's item id.
    static func lineID(for mark: EdgeMark, lines: [ChatLine]) -> String? {
        lines.first { line in
            if line.id == mark.id || line.holds(mark.id) { return true }
            if case .userImages(_, let images) = line.content {
                return images.contains { $0.seq == mark.seq }
            }
            return false
        }?.id
    }
}
