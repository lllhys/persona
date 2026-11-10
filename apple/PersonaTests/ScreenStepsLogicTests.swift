import Foundation
import Testing
@testable import Persona

/// A terminal dialog of several questions (#2970): which step a card is on, when an answer may submit them all, the
/// review's rows, and how each answer goes out.
struct ScreenStepsLogicTests {
    private typealias D = ScreenParser.Dialog

    private func dialog(_ question: String, done: [Bool], options: [D.Option] = [], multi: Bool = false, submit: String? = nil,
                        review: Bool = false, incomplete: Bool = false, detail: String? = nil) -> D {
        var d = D(header: nil, question: question, detail: nil, options: options)
        d.steps = done.enumerated().map { .init(label: ["环境", "平台", "通知"][$0.offset], done: $0.element) }
        d.multi = multi
        d.submit = submit
        d.review = review
        d.incomplete = incomplete
        d.detail = detail
        return d
    }

    private let plain = [D.Option(key: "1", label: "预览"), D.Option(key: "2", label: "正式"), D.Option(key: "3", label: "Type something.", kind: .typeSomething)]
    private let now = Date(timeIntervalSince1970: 1_000_000)

    @Test func theStepIsTheFirstUnansweredUntilTheAppKnowsBetter() {
        #expect(ScreenStepsLogic.current(dialog("先发哪个环境？", done: [false, false, false], options: plain), trail: nil, now: now) == 0)
        #expect(ScreenStepsLogic.current(dialog("发完通知谁？", done: [true, false, false], options: plain), trail: nil, now: now) == 1)
        // A question gone back to shows its answer: which step it is can't be read off the screen…
        var picked = plain
        picked[0].checked = true
        let back = dialog("先发哪个环境？", done: [true, true, false], options: picked)
        #expect(ScreenStepsLogic.current(back, trail: nil, now: now) == nil)
        // …but the app saw it before, or pressed ← from step 1 a moment ago.
        var seen = ScreenTrail(labels: ["环境", "平台", "通知"])
        seen.steps["先发哪个环境？"] = 0
        #expect(ScreenStepsLogic.current(back, trail: seen, now: now) == 0)
        var pressed = ScreenTrail(labels: ["环境", "平台", "通知"])
        pressed.expect = 0
        pressed.expectAt = now
        #expect(ScreenStepsLogic.current(back, trail: pressed, now: now.addingTimeInterval(5)) == 0)
        #expect(ScreenStepsLogic.current(back, trail: pressed, now: now.addingTimeInterval(60)) == nil)
        // Another dialog's trail says nothing; a multi-select question whose row says Submit is the last; the review is none.
        var other = pressed
        other.labels = ["A", "B"]
        #expect(ScreenStepsLogic.current(back, trail: other, now: now) == nil)
        #expect(ScreenStepsLogic.current(dialog("哪些平台？", done: [true, true, true], multi: true, submit: "Submit"), trail: nil, now: now) == 2)
        #expect(ScreenStepsLogic.current(dialog("Ready to submit your answers?", done: [true, true, true], review: true), trail: nil, now: now) == nil)
    }

    @Test func onlyTheAnswerThatCanFinishThemWaits() {
        let last = dialog("发完通知谁？", done: [true, true, false], options: plain)
        #expect(ScreenStepsLogic.submitsAll(last, current: 2))
        #expect(!ScreenStepsLogic.submitsAll(last, current: 1)) // the terminal moves on to the last question, not the review
        #expect(ScreenStepsLogic.submitsAll(last, current: nil)) // can't tell: it might be
        #expect(!ScreenStepsLogic.submitsAll(dialog("?", done: [false, false, false], options: plain), current: nil))
        #expect(!ScreenStepsLogic.submitsAll(dialog("?", done: [true, false, false], multi: true, submit: "Next"), current: 1))
        #expect(ScreenStepsLogic.submitsAll(dialog("?", done: [true, true, false], multi: true, submit: "Submit"), current: 2))
        #expect(!ScreenStepsLogic.submitsAll(dialog("?", done: [false, true, false], multi: true, submit: "Submit"), current: 2))
        #expect(!ScreenStepsLogic.submitsAll(dialog("?", done: [true, true, true], review: true), current: nil))
    }

    @Test func theReviewRowsFollowTheTabBar() {
        // As the hub reads Claude Code 2.1.316's review tab (claude-2.1.316-questions-review-incomplete-100): every line
        // trimmed, a long question wrapped.
        let detail = "● 示例一：先发哪个环境？\n→ 预览\n● 示例二：哪些平台要一起\n发？\n→ 手机, 电脑"
        let rows = ScreenStepsLogic.reviewRows(dialog("Ready to submit your answers?", done: [true, true, false], review: true, incomplete: true, detail: detail))
        #expect(rows == [
            ScreenReviewRow(index: 0, label: "环境", question: "示例一：先发哪个环境？", answer: "预览"),
            ScreenReviewRow(index: 1, label: "平台", question: "示例二：哪些平台要一起发？", answer: "手机, 电脑"),
            ScreenReviewRow(index: 2, label: "通知", question: nil, answer: nil),
        ])
        // Answered with nothing to read back: answered all the same.
        let bare = ScreenStepsLogic.reviewRows(dialog("Ready?", done: [true, true, true], review: true))
        #expect(bare.map(\.answer) == ["", "", ""])
        #expect(ScreenStepsLogic.join("Which platforms should", "ship?") == "Which platforms should ship?")
    }

    @Test func theHeldReviewHasWhatThisDeviceAnswered() {
        var t = ScreenTrail(labels: ["环境", "平台", "通知"])
        t.answers[0] = "预览"
        let d = dialog("发完通知谁？", done: [true, true, false], multi: true, submit: "Submit")
        #expect(ScreenStepsLogic.heldRows(d, trail: t, last: "设计组").map(\.answer) == ["预览", "", "设计组"])
    }

    @Test func theTrailNotesWhereTheNextCardShouldBe() {
        let t = ScreenTrail(labels: ["环境", "平台", "通知"])
        let back = ScreenStepsLogic.sent(t, from: 1, move: -1, answer: nil, now: now)
        #expect(back.expect == 0 && back.expectAt == now)
        let answered = ScreenStepsLogic.sent(t, from: 2, move: 1, answer: "设计组", now: now)
        #expect(answered.expect == nil && answered.answers[2] == "设计组") // past the last question: the review
        #expect(ScreenStepsLogic.sent(t, from: 3, move: -1, answer: nil, now: now).expect == 2) // ← from the review
        // A card seen notes its question at its step; another dialog starts again.
        let seen = ScreenStepsLogic.seen(dialog("先发哪个环境？", done: [false, false, false], options: plain), trail: answered, now: now)
        #expect(seen.steps["先发哪个环境？"] == 0 && seen.answers[2] == "设计组")
        var other = dialog("?", done: [false, false], options: plain)
        other.steps = [.init(label: "A", done: false), .init(label: "B", done: false)]
        #expect(ScreenStepsLogic.seen(other, trail: answered, now: now).answers.isEmpty)
    }

    @Test func answersNameTheirOptionUnlessTheyMayBeTheLast() {
        let early = dialog("先发哪个环境？", done: [false, false, false], options: plain)
        #expect(ScreenAnswerRoute.choice(plain[0], early, step: 0, hubRead: true).option == "1")
        let last = dialog("发完通知谁？", done: [true, true, false], options: plain)
        let keys = ScreenAnswerRoute.choice(plain[0], last, step: 2, hubRead: true)
        #expect(keys.option == nil && keys.keys == ["1"])
        #expect(ScreenAnswerRoute.choice(plain[0], early, step: 0, hubRead: false).keys == ["1"]) // an older hub
        // Words: by the option, unless the field already has some (cleared first) or it may be the last answer.
        #expect(ScreenAnswerRoute.typed(plain[2], text: "都发", early, step: 0, hubRead: true).option == "3")
        var filled = plain[2]
        filled.text = "先发预览"
        let again = ScreenAnswerRoute.typed(filled, text: "都发", early, step: 0, hubRead: true)
        #expect(again.option == nil && again.preKeys == ["3", "ctrl+e", "ctrl+u"] && again.text == "都发" && again.keys == ["enter"])
        #expect(ScreenAnswerRoute.typed(plain[2], text: "都发", last, step: 2, hubRead: true).preKeys == ["3"])
        let multi = [D.Option(key: "1", label: "手机 (Recommended)"), D.Option(key: "2", label: "手表")]
        #expect(ScreenAnswerRoute.summary(dialog("?", done: [true, false, false], options: multi), keys: ["1", "2"], text: " 平板 ")
                == ["手机", "手表", "平板"].joined(separator: String(localized: ", ", comment: "Separator between items in a list")))
    }

    @Test func aSessionsScreenCardsShareAPrefix() {
        #expect(ScreenStepsLogic.screenPrefix("scr_macbook_s_1_3f9a1c") == "scr_macbook_s_1_")
        #expect(ScreenStepsLogic.screenPrefix("home|scr_macbook_s_3f9a1c") == "home|scr_macbook_s_")
        #expect(ScreenStepsLogic.screenPrefix("ix_42") == nil)
    }
}
