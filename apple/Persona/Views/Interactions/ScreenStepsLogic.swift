import Foundation

/// Where this device has been in a terminal's multi-question dialog (#2970): which step each question it saw is, what it
/// answered there, and the step the next card should show after a key it pressed. The terminal marks the current tab by
/// color only, which the screen reading drops, so the app keeps track itself. In memory, per session.
nonisolated struct ScreenTrail: Equatable, Sendable {
    /// The dialog's step labels: another dialog starts another trail.
    var labels: [String]
    /// The step each question seen was on.
    var steps: [String: Int] = [:]
    /// What this device answered at a step, for the review it makes before the last multi-select answer.
    var answers: [Int: String] = [:]
    /// The step the next card should be on, set when this device pressed a key or answered, and when.
    var expect: Int?
    var expectAt: Date?
    /// "Answer X" on the review: ← is pressed again on each card until this step shows.
    var walkTo: Int?
}

/// A screen card this device answered in a multi-question dialog: shown, greyed, with a spinner until the next card of
/// that dialog comes (the hub reads the screen again after the keys).
nonisolated struct ScreenMove: Equatable, Sendable {
    var card: Interaction
    var at: Date
}

/// A row of a dialog's review: the step's label, the question as the terminal's review page words it, and the answer
/// (nil: not answered; empty: answered, with no words to show).
nonisolated struct ScreenReviewRow: Equatable, Sendable, Identifiable {
    var index: Int
    var label: String
    var question: String?
    var answer: String?
    var id: Int { index }
}

nonisolated enum ScreenStepsLogic {
    /// How long a step this device expects stays the answer to "which one is this".
    static let expectFor: TimeInterval = 30
    /// How long a card waits for the next one before it gives up and comes back.
    static let moveFor: TimeInterval = 10

    /// Whether the new controls are drawn: iPhone and iPad (the Mac app is frozen, docs/apple/mac.md).
    static var enabled: Bool {
        #if os(iOS)
        true
        #else
        false
        #endif
    }

    /// The step a dialog is on, when it can be told: a multi-select question whose row says Submit is the last one; a
    /// question seen before is where it was; after this device's key, the step it expected; a question with nothing
    /// picked is the first one not answered (the terminal goes forward to it). Nil on the review tab or when unsure.
    static func current(_ d: ScreenParser.Dialog, trail: ScreenTrail?, now: Date) -> Int? {
        let n = d.steps.count
        guard n > 1, !d.review else { return nil }
        if d.multi, d.submit == "Submit" { return n - 1 }
        if let t = trail, t.labels == d.steps.map(\.label) {
            if let i = t.steps[d.question], i < n { return i }
            if let e = t.expect, let at = t.expectAt, now.timeIntervalSince(at) < expectFor, (0..<n).contains(e) { return e }
        }
        if !hasAnswer(d), let i = d.steps.firstIndex(where: { !$0.done }) { return i }
        return nil
    }

    /// The question shows an answer: an option picked before (✔ after it, a ticked box) or words in its field.
    static func hasAnswer(_ d: ScreenParser.Dialog) -> Bool {
        d.options.contains { $0.kind != .chat && ($0.checked || !($0.text ?? "").isEmpty) }
    }

    /// Whether answering this question can submit them all at once: the node presses the review tab's Submit answers
    /// after an answer that names its option (a multi-select one always) when every question has one. A single-choice
    /// answer that can goes as keys instead, so the review tab comes as its own card; a multi-select one waits for the
    /// review the app shows itself.
    static func submitsAll(_ d: ScreenParser.Dialog, current: Int?) -> Bool {
        let n = d.steps.count
        guard n > 1, !d.review else { return false }
        let open = d.steps.indices.filter { !d.steps[$0].done }
        if d.multi { return d.submit == "Submit" && open.allSatisfy { $0 == n - 1 } }
        if let c = current { return c == n - 1 && open.allSatisfy { $0 == c } }
        return open.count <= 1
    }

    /// The review tab's rows: its "● question" / "→ answer" lines (wrapped lines joined) matched in order to the answered
    /// steps; a step without an answer is a row of its own, in its place.
    static func reviewRows(_ d: ScreenParser.Dialog) -> [ScreenReviewRow] {
        var pairs: [(q: String, a: String)] = []
        var inAnswer = false
        for line in (d.detail ?? "").components(separatedBy: "\n") {
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("●") {
                pairs.append((String(t.dropFirst()).trimmingCharacters(in: .whitespaces), ""))
                inAnswer = false
            } else if t.hasPrefix("→"), !pairs.isEmpty {
                pairs[pairs.count - 1].a = join(pairs[pairs.count - 1].a, String(t.dropFirst()).trimmingCharacters(in: .whitespaces))
                inAnswer = true
            } else if !t.isEmpty, !pairs.isEmpty, !t.hasPrefix("⚠") { // the hub trims every line: a wrapped one goes on
                if inAnswer { pairs[pairs.count - 1].a = join(pairs[pairs.count - 1].a, t) } else { pairs[pairs.count - 1].q = join(pairs[pairs.count - 1].q, t) }
            }
        }
        var rows: [ScreenReviewRow] = []
        var k = 0
        for (i, s) in d.steps.enumerated() {
            if s.done, k < pairs.count {
                rows.append(ScreenReviewRow(index: i, label: s.label, question: pairs[k].q, answer: pairs[k].a))
                k += 1
            } else {
                rows.append(ScreenReviewRow(index: i, label: s.label, question: nil, answer: s.done ? "" : nil))
            }
        }
        for p in pairs[k...] {
            rows.append(ScreenReviewRow(index: rows.count, label: "", question: p.q, answer: p.a))
        }
        return rows
    }

    /// A wrapped line joined to the one before: with a space between words, none between Chinese or Japanese characters.
    static func join(_ a: String, _ b: String) -> String {
        guard let last = a.unicodeScalars.last, let first = b.unicodeScalars.first else { return a + b }
        return last.value >= 0x2E80 || first.value >= 0x2E80 ? a + b : a + " " + b
    }

    /// The review the app shows before the last multi-select answer: each step's answer as this device gave it (an empty
    /// one when it was answered elsewhere), the last one what is picked now.
    static func heldRows(_ d: ScreenParser.Dialog, trail: ScreenTrail?, last: String) -> [ScreenReviewRow] {
        d.steps.enumerated().map { i, s in
            if i == d.steps.count - 1 { return ScreenReviewRow(index: i, label: s.label, question: d.question, answer: last) }
            return ScreenReviewRow(index: i, label: s.label, question: nil, answer: trail?.answers[i] ?? (s.done ? "" : nil))
        }
    }

    /// The trail after a card of the dialog shows: a new dialog starts a new one; the question is noted at its step.
    static func seen(_ d: ScreenParser.Dialog, trail: ScreenTrail?, now: Date) -> ScreenTrail {
        let labels = d.steps.map(\.label)
        var t = trail?.labels == labels ? trail! : ScreenTrail(labels: labels)
        if let i = current(d, trail: t, now: now), !d.review { t.steps[d.question] = i }
        return t
    }

    /// What this device sends from a step, as the trail notes it: the step the next card should be on.
    static func sent(_ trail: ScreenTrail, from current: Int?, move: Int, answer: String?, now: Date) -> ScreenTrail {
        var t = trail
        if let c = current {
            if let answer { t.answers[c] = answer }
            let next = c + move
            t.expect = (0..<t.labels.count).contains(next) ? next : nil
        } else {
            t.expect = nil
        }
        t.expectAt = now
        return t
    }

    /// The card ids of one session's terminal dialogs: a screen card's id is `scr_<machine>_<session>_<rev>` (group
    /// first in its uid). A question gone back to comes again under the id it had.
    static func screenPrefix(_ uid: String) -> String? {
        guard uid.contains("scr_"), let cut = uid.lastIndex(of: "_") else { return nil }
        return String(uid[...cut])
    }
}
