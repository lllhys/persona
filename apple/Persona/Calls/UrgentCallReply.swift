import Foundation

/// What the owner's words on an urgent call mean (`UrgentCallSession`): an option of the call's card, approving or
/// denying it, yes or no to the read-back, hanging up, or words for the conversation. Plain text in, so the tests read
/// it without a call; the words it knows are the three languages the app speaks.
nonisolated enum UrgentCallReply: Equatable, Sendable {
    case option(String) // a card option's label, as the card has it
    case allow
    case deny
    case confirm
    case cancel
    case hangUp
    case words(String)

    /// What `said` is while the call waits for an answer to a card with these option labels (approval: a permission
    /// or plan card, answered by allowing or denying it).
    static func answer(_ said: String, options: [String], approval: Bool) -> UrgentCallReply {
        let heard = normalized(said)
        guard !heard.isEmpty else { return .words("") }
        if let label = option(said, in: options) { return .option(label) }
        if approval {
            if says(said, denyWords) { return .deny }
            if says(said, allowWords) { return .allow }
        }
        if says(said, hangUpWords) { return .hangUp }
        return .words(said.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// What `said` is after Persona read the decision back: yes, no, or something else (taken as no).
    static func readBack(_ said: String) -> UrgentCallReply {
        if says(said, cancelWords) { return .cancel }
        if says(said, confirmWords) { return .confirm }
        return .cancel
    }

    /// The option the words name: by its label (or the label within the words), or by its place ("the second one").
    static func option(_ said: String, in options: [String]) -> String? {
        let heard = normalized(said)
        // Matched as shown: "(Recommended)" is the card's mark, not words the owner says.
        let labels = options.map { (label: $0, key: normalized(QuestionOption.display($0).0)) }.filter { !$0.key.isEmpty }
        if let exact = labels.first(where: { $0.key == heard }) { return exact.label }
        // The longest label inside the words first: "Roll back to 4.2" before "Roll back".
        if let inside = labels.sorted(by: { $0.key.count > $1.key.count }).first(where: { heard.contains($0.key) }) { return inside.label }
        if heard.count >= 2, let partial = labels.first(where: { $0.key.contains(heard) }) { return partial.label }
        for (i, words) in ordinals.enumerated() where i < labels.count && says(said, words) {
            return labels[i].label
        }
        return nil
    }

    /// Lower case, without spaces or punctuation, so "Roll back." and "roll back" are one answer.
    static func normalized(_ s: String) -> String {
        String(s.lowercased().unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) && !CharacterSet.punctuationCharacters.contains($0) })
    }

    /// Whether `said` has one of `words`: a word in Latin letters as a whole word ("no" is not in "now"), any other
    /// within the words, spaces and punctuation aside.
    static func says(_ said: String, _ words: [String]) -> Bool {
        let lower = said.lowercased()
        let heard = normalized(said)
        return words.contains { word in
            if word.unicodeScalars.allSatisfy(\.isASCII) {
                let pattern = "\\b" + NSRegularExpression.escapedPattern(for: word) + "\\b"
                return lower.range(of: pattern, options: .regularExpression) != nil
            }
            return heard.contains(normalized(word))
        }
    }

    // The words, i18n-ignore: they are matched against what the speech recognizer heard, not shown.
    static let confirmWords = ["确认", "对的", "是的", "没错", "好的", "可以", "confirm", "yes", "correct", "確認", "はい"] // i18n-ignore
    static let cancelWords = ["不对", "不是", "取消", "重新", "不要", "cancel", "no", "wrong", "いいえ", "違う", "キャンセル"] // i18n-ignore
    static let allowWords = ["批准", "同意", "允许", "可以做", "approve", "allow", "go ahead", "承認", "許可"] // i18n-ignore
    static let denyWords = ["不批准", "不同意", "不允许", "拒绝", "不行", "deny", "reject", "don't", "却下", "拒否", "だめ"] // i18n-ignore
    static let hangUpWords = ["挂断", "挂了", "没有了", "没别的", "就这样", "hang up", "that's all", "nothing else", "bye", "以上", "切って"] // i18n-ignore
    static let ordinals: [[String]] = [
        ["第一", "first", "1番", "一番目"], ["第二", "second", "2番", "二番目"], ["第三", "third", "3番", "三番目"], // i18n-ignore
        ["第四", "fourth", "4番", "四番目"], // i18n-ignore
    ]
}
