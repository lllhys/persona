import Foundation
import Testing
import PersonaKit
@testable import Persona

/// Select Messages, Include Questions, Copy and Export Image (#9468): which questions a pick brings, the copied text,
/// the parts a reply is drawn in, a picture's size in the words, and where a long picture's pages break.
@MainActor
struct ChatExportTests {
    private func item(_ seq: Int, _ role: String, _ text: String, kind: ItemKind = .text) -> Item {
        Item(id: "m\(seq)", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: nil, toolName: nil, toolInput: nil,
             isError: nil, image: nil)
    }

    private func blocks(_ items: [Item]) -> [ChatBlock] { ChatBlock.build(ChatStore.buildRows(items)) }

    // MARK: Include Questions

    @Test func aReplyBringsItsTurnsQuestionsInOrder() {
        let map = PickQuestions.map(blocks([
            item(1, "user", "小组件刷新太慢"), item(2, "user", "顺便看看耗电"), item(3, "assistant", "原因找到了"),
            item(4, "user", "改成 15 分钟会更费电吗？"), item(5, "assistant", "会多一点"),
        ]))
        #expect(map["m3"] == ["m1", "m2"])
        #expect(map["m5"] == ["m4"])
        #expect(map["m1"] == nil, "a question brings nothing")
    }

    @Test func aQuestionWithoutItsOwnReplyGoesWithTheNext() {
        // Two turns: the first a question alone, the second a question and the reply.
        let rows = ChatStore.buildRows([item(1, "user", "Look at the widget"), item(2, "user", "and its battery use"),
                                        item(3, "assistant", "Both are fine.")])
        let map = PickQuestions.map(ChatBlock.build(rows))
        #expect(map["m3"] == ["m1", "m2"])
    }

    @Test func injectedLinesAndLoopTicksAreNoQuestions() {
        let map = PickQuestions.map(blocks([item(1, "user", "/loop 5m check the build"), item(2, "assistant", "Still green.")]))
        #expect(map["m2"] == nil)
    }

    @Test func broughtLeavesOutWhatIsPickedAndOffersOnlyThen() {
        let questions = ["a1": ["q1"], "a2": ["q2", "q3"]]
        #expect(PickQuestions.brought(["a1", "a2"], questions: questions) == ["q1", "q2", "q3"])
        #expect(PickQuestions.brought(["a2", "q2"], questions: questions) == ["q3"])
        #expect(PickQuestions.offered(["a1"], questions: questions))
        #expect(!PickQuestions.offered(["a1", "q1"], questions: questions), "the question is picked already")
        #expect(!PickQuestions.offered(["q1"], questions: questions), "only questions picked")
    }

    @Test func includeIsOnByDefaultAndRemembered() {
        let defaults = UserDefaults.standard
        let before = defaults.object(forKey: MessagePick.includeKey)
        defer { defaults.set(before, forKey: MessagePick.includeKey) }
        defaults.removeObject(forKey: MessagePick.includeKey)
        let pick = MessagePick()
        pick.start("a")
        #expect(pick.include)
        _ = pick.setInclude(false)
        let next = MessagePick()
        next.start("b")
        #expect(!next.include, "the last choice is kept")
    }

    @Test func whatGoesNeverPassesAForwardsMost() {
        let defaults = UserDefaults.standard
        let before = defaults.object(forKey: MessagePick.includeKey)
        defer { defaults.set(before, forKey: MessagePick.includeKey) }
        defaults.set(true, forKey: MessagePick.includeKey)
        let pick = MessagePick()
        var questions: [String: [String]] = [:]
        for i in 0..<20 { questions["a\(i)"] = ["q\(i)"] }
        pick.readQuestions = { questions }
        pick.start("a0")
        var picked = 1
        for i in 1..<20 where pick.toggle("a\(i)") { picked += 1 }
        #expect(picked == ForwardMessage.most / 2, "each reply brings one question: 15 replies and 15 questions")
        #expect(pick.going.count == ForwardMessage.most)
        #expect(!pick.toggle("a19"))
        // Off, the replies alone fit; on again would go past the most.
        #expect(pick.setInclude(false))
        for i in 15..<20 { _ = pick.toggle("a\(i)") }
        #expect(pick.ids.count == 20)
        #expect(!pick.setInclude(true))
        #expect(!pick.include)
    }

    // MARK: Copy

    @Test func copiedTextSaysWhoWroteEachWithABlankLineBetween() {
        let text = ExportText.copy([
            ExportMessage(id: "1", role: .user, author: "Me", text: "Why is the widget slow?"),
            ExportMessage(id: "2", role: .agent, author: "Claude", text: "## Found it\nIt refreshes every **30 minutes**."),
        ])
        let me = String(localized: "\("Me"): [Copied Messages]"), claude = String(localized: "\("Claude"): [Copied Messages]")
        #expect(text == "\(me)\nWhy is the widget slow?\n\n\(claude)\n## Found it\nIt refreshes every **30 minutes**.")
        #expect(me.hasPrefix("Me") && !me.contains("["), "the catalog has the line's format: \(me)")
    }

    // MARK: Parts

    @Test func picturesOnLinesOfTheirOwnAreOneRunAsTheChatReadsThem() {
        let parts = ExportParts.of("Before.\n\n![a](https://example.com/a.png)\n![b](https://example.com/b.png)\n\n![c](https://example.com/c.png)\n\nAfter.")
        #expect(parts.count == 3)
        guard parts.count == 3, case .images(let run) = parts[1] else { return }
        #expect(run.map(\.url) == ["https://example.com/a.png", "https://example.com/b.png", "https://example.com/c.png"])
        // A picture right under a line of words is a run of its own too (`ReplyImages`).
        let under = ExportParts.of("The chart:\n![chart](https://example.com/chart.png)")
        #expect(under.contains { if case .images(let r) = $0 { r.map(\.url) == ["https://example.com/chart.png"] } else { false } })
    }

    @Test func aParagraphWithFormulasIsDrawnAsTextOtherFormulasAsCode() {
        let md = MathMarkdown.rewrite("Euler: $e^{i\\pi}+1=0$ holds.\n\n- in a list $x^2$")
        let parts = ExportParts.blocks(md)
        #expect(parts.count == 2)
        guard case .mathText(let p) = parts[0], case .markdown(let list) = parts[1] else {
            Issue.record("parts: \(parts)")
            return
        }
        #expect(ExportMath.formulas(p) == ["e^{i\\pi}+1=0"])
        #expect(list.contains("`x^2`") && !list.contains(MathMarkdown.scheme))
    }

    @Test func aFenceStaysWholeAndALongOneIsDrawnInPieces() {
        let short = "Code:\n```swift\nlet a = 1\n\nlet b = 2\n```"
        #expect(ExportParts.blocks(short) == [.markdown("Code:\n\n```swift\nlet a = 1\n\nlet b = 2\n```")])
        let long = "```\n" + (1...130).map { "line \($0)" }.joined(separator: "\n") + "\n```"
        let pieces = ExportParts.blocks(long)
        #expect(pieces.count == 3)
        if case .markdown(let first) = pieces[0] {
            #expect(first.hasPrefix("```\nline 1\n") && first.hasSuffix("line 60\n```"))
        }
        if case .markdown(let last) = pieces[2] { #expect(last.hasSuffix("line 130\n```")) }
    }

    @Test func plainBlocksAreDrawnTogetherUpToARun() {
        let para = String(repeating: "字", count: 500)
        let parts = ExportParts.blocks([para, para, para].joined(separator: "\n\n"))
        #expect(parts.count == 2, "two blocks fit a run, the third starts the next")
    }

    @Test func mathAndDiagramFencesAreFoundForDrawingFirst() {
        let found = ExportMath.fences("```math\na^2+b^2=c^2\n```\n\n```mermaid\ngraph TD\nA-->B\n```\n\n```swift\nlet x = 1\n```")
        #expect(found.map(\.0) == [.displayMath, .mermaid])
        #expect(found.map(\.1) == ["a^2+b^2=c^2", "graph TD\nA-->B"])
    }

    // MARK: Pages

    @Test func blocksStackAndBreakBetweenThemselves() {
        let pages = ExportPages.split([100, 300, 300, 50], gaps: [0, 10, 10, 10], room: 500)
        #expect(pages.map { $0.map(\.block) } == [[0, 1], [2, 3]])
        #expect(pages[1].allSatisfy { $0.from == 0 })
    }

    @Test func aBlockTallerThanAPageIsCutWhereItMayBe() {
        // Blank rows every 90 points: cuts land on them, never past the page.
        let pages = ExportPages.split([200, 1000], gaps: [0, 10], room: 400) { _, from, limit in
            stride(from: 90, through: 1000, by: 90).map(CGFloat.init).last { $0 > from + ExportPages.least && $0 <= limit } ?? limit
        }
        let tall = pages.flatMap { $0 }.filter { $0.block == 1 }
        #expect(tall.first?.from == 0)
        #expect(zip(tall, tall.dropFirst()).allSatisfy { $0.to == $1.from }, "the slices follow each other")
        #expect(tall.last?.to == 1000)
        for page in pages {
            let used = page.enumerated().reduce(CGFloat(0)) { $0 + $1.element.height + ($1.offset == 0 ? 0 : 10) }
            #expect(used <= 400)
        }
        #expect(tall.dropLast().allSatisfy { $0.to.truncatingRemainder(dividingBy: 90) == 0 })
    }

    @Test func aPageHoldsAtMostItsPixelsAndItsMemory() {
        // 393 pt at 3×: the memory rule (64 MB) is the tighter one.
        let h = ChatExportRenderer.pageHeight(width: 393, scale: 3)
        #expect(h * 3 <= ChatExportRenderer.maxPixels)
        #expect(393 * 3 * h * 3 * 4 <= ChatExportRenderer.maxBytes + 1)
        // 360 pt at 2×: the pixel rule.
        #expect(ChatExportRenderer.pageHeight(width: 360, scale: 2) * 2 == ChatExportRenderer.maxPixels)
    }

    @Test func theHeadingSaysTheDayOrTheDaysAndWhoAnswers() {
        let day = Date(timeIntervalSince1970: 1_800_000_000)
        let one = ChatExportRenderer.subtitle([ExportMessage(id: "1", role: .user, author: "Me", text: "", time: day)], agent: "Claude")
        #expect(one.hasSuffix(" · Claude") && !one.contains("–"))
        let two = ChatExportRenderer.subtitle([ExportMessage(id: "1", role: .user, author: "Me", text: "", time: day),
                                               ExportMessage(id: "2", role: .agent, author: "Claude", text: "", time: day.addingTimeInterval(3 * 86_400))],
                                              agent: "Claude")
        #expect(two.contains(" – "))
    }
}
