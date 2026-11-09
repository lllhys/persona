import Foundation
import MarkdownUI
import SwiftUI
import Testing
@testable import Persona

/// Rendering helpers retain hot results without changing code text, table content or font-sensitive measurements.
@MainActor
struct MarkdownCodeMemoTests {
    @Test func repeatedBlocksDoNotHighlightAgain() {
        let memo = MarkdownCodeMemo(limit: 3)
        var highlights = 0
        let code = "let greeting = \"你好 👋\""
        func shown() -> Text {
            memo.value(code, language: "swift", scale: 1) {
                highlights += 1
                return Text(code)
            }
        }
        let first = shown()
        for _ in 0..<100 { #expect(shown() == first) }
        #expect(highlights == 1)
    }

    @Test func aFullCacheEvictsOnlyTheLeastRecentlyUsedBlock() {
        let memo = MarkdownCodeMemo(limit: 3)
        var made: [String: Int] = [:]
        func show(_ code: String) -> Text {
            memo.value(code, language: "swift", scale: 1) {
                made[code, default: 0] += 1
                return Text(code)
            }
        }
        _ = show("a")
        _ = show("b")
        _ = show("c")
        _ = show("a") // The oldest insertion is still on screen, so keep it.
        _ = show("d")
        _ = show("a")
        _ = show("c")
        _ = show("d")
        #expect(made == ["a": 1, "b": 1, "c": 1, "d": 1])
        _ = show("b")
        #expect(made["b"] == 2)
    }

    @Test func hotBlocksSurviveMoreThanOneCachesWorthOfNewBlocks() {
        let memo = MarkdownCodeMemo(limit: 300)
        var hotHighlights = 0
        for index in 0..<650 {
            _ = memo.value("visible", language: "swift", scale: 1) {
                hotHighlights += 1
                return Text("visible")
            }
            let code = "let value = \(index)"
            _ = memo.value(code, language: "swift", scale: 1) { Text(code) }
        }
        #expect(hotHighlights == 1)
    }

    @Test func codeLanguageAndFontScaleEachHaveTheirOwnResult() {
        let memo = MarkdownCodeMemo()
        var made = 0
        func show(_ code: String, _ language: String?, _ scale: CGFloat) -> Text {
            memo.value(code, language: language, scale: scale) {
                made += 1
                return Text("result \(made)")
            }
        }
        let first = show("let value = 1", "swift", 1)
        #expect(show("let value = 2", "swift", 1) != first)
        #expect(show("let value = 1", "python", 1) != first)
        #expect(show("let value = 1", nil, 1) != first)
        #expect(show("let value = 1", "swift", 1.2) != first)
        #expect(show("let value = 1", "swift", 1) == first)
        #expect(made == 5)
    }

    @Test func theSourceByteBudgetAlsoEvictsOnlyColdBlocks() {
        let memo = MarkdownCodeMemo(limit: 8, byteLimit: 8)
        var made: [String: Int] = [:]
        func show(_ code: String) -> Text {
            memo.value(code, language: nil, scale: 1) {
                made[code, default: 0] += 1
                return Text(code)
            }
        }
        _ = show("aaaa")
        _ = show("bbbb")
        _ = show("aaaa")
        _ = show("cccc")
        _ = show("aaaa")
        _ = show("cccc")
        #expect(made == ["aaaa": 1, "bbbb": 1, "cccc": 1])
        _ = show("bbbb")
        #expect(made["bbbb"] == 2)
    }

    @Test func byteBudgetCountsUTF8AndLanguageNotJustCharacters() {
        let memo = MarkdownCodeMemo(limit: 8, byteLimit: 9)
        var made = 0
        func show(_ code: String) {
            _ = memo.value(code, language: "x", scale: 1) {
                made += 1
                return Text(code)
            }
        }
        show("🙂") // Four UTF-8 bytes plus the language byte.
        show("🚀")
        show("🙂")
        #expect(made == 3)
    }

    @Test func oversizedBlocksDoNotDisplaceCachedBlocks() {
        let memo = MarkdownCodeMemo(limit: 3, byteLimit: 8)
        var smallHighlights = 0
        var largeHighlights = 0
        func small() -> Text {
            memo.value("small", language: nil, scale: 1) {
                smallHighlights += 1
                return Text("small")
            }
        }
        let first = small()
        for _ in 0..<2 {
            let large = memo.value("123456789", language: nil, scale: 1) {
                largeHighlights += 1
                return Text("123456789")
            }
            #expect(large == Text("123456789"))
            #expect(small() == first)
        }
        #expect(smallHighlights == 1)
        #expect(largeHighlights == 2)
    }

    @Test func largePlainTextBelowTheByteBudgetDoesNotEvictHighlightedHotBlocks() {
        let memo = MarkdownCodeMemo()
        var highlighted = 0
        let hot = (0..<40).map { String(repeating: "x", count: 5_000) + String($0) }
        for _ in 0..<3 {
            for code in hot {
                _ = memo.value(code, language: "swift", scale: 1) {
                    highlighted += 1
                    return Text(code)
                }
            }
            let plain = String(repeating: "z", count: 1_900_000)
            _ = memo.value(plain, language: "swift", scale: 1) { Text(plain) }
        }
        #expect(highlighted == 40)
    }

    @Test func aZeroEntryLimitDoesNotCache() {
        let memo = MarkdownCodeMemo(limit: 0)
        var highlights = 0
        for _ in 0..<2 {
            _ = memo.value("code", language: nil, scale: 1) {
                highlights += 1
                return Text("code")
            }
        }
        #expect(highlights == 2)
    }
}

@MainActor
struct MarkdownTablePreparationTests {
    private static let table = "| # | 事项 | 裁定 |\n| --- | :-: | ---: |\n| 1 | **小组件** | 见 [文档](https://example.com) 和 `a\\|b` |"

    @Test func repeatedLayoutsDoNotRenderOrMeasureAgain() {
        var renders = 0, measurements = 0
        let preparation = MarkdownTablePreparation(render: { content in
            renders += 1
            return content.renderMarkdown()
        }, measure: { markdown, size in
            measurements += 1
            return MarkdownTableText.measure(markdown, fontSize: size)
        })
        let content = MarkdownContent(Self.table)
        let expected = MarkdownTableText.measure(content.renderMarkdown(), fontSize: 15)
        for _ in 0..<100 {
            let result = preparation.measure(content, fontSize: 15)
            #expect(result.natural == expected.natural)
            #expect(result.minimum == expected.minimum)
        }
        // SwiftUI can provide an equal content value from another configuration instance.
        _ = preparation.measure(MarkdownContent(Self.table), fontSize: 15)
        #expect(renders == 1)
        #expect(measurements == 1)
    }

    @Test func fontChangesRemeasureWithoutRenderingTheSourceAgain() {
        var renders = 0, measurements = 0
        let preparation = MarkdownTablePreparation(render: { content in
            renders += 1
            return content.renderMarkdown()
        }, measure: { markdown, size in
            measurements += 1
            return MarkdownTableText.measure(markdown, fontSize: size)
        })
        let content = MarkdownContent(Self.table)
        let small = preparation.measure(content, fontSize: 15)
        let large = preparation.measure(content, fontSize: 30)
        let expected = MarkdownTableText.measure(content.renderMarkdown(), fontSize: 30)
        #expect(large.natural == expected.natural)
        #expect(large.minimum == expected.minimum)
        #expect(zip(small.natural, large.natural).allSatisfy { $0.0 < $0.1 })
        _ = preparation.measure(content, fontSize: 30)
        #expect(renders == 1)
        #expect(measurements == 2)
    }

    @Test func changedContentReplacesTheCurrentTableEvenAtTheSameFontSize() {
        var renders = 0, measurements = 0
        let preparation = MarkdownTablePreparation(render: { content in
            renders += 1
            return content.renderMarkdown()
        }, measure: { markdown, size in
            measurements += 1
            return MarkdownTableText.measure(markdown, fontSize: size)
        })
        let first = MarkdownContent(Self.table)
        let second = MarkdownContent("| Name | Value |\n| --- | --- |\n| Current | A much longer value |")
        let a = preparation.measure(first, fontSize: 15)
        let b = preparation.measure(second, fontSize: 15)
        let expected = MarkdownTableText.measure(second.renderMarkdown(), fontSize: 15)
        #expect(b.natural == expected.natural)
        #expect(b.minimum == expected.minimum)
        #expect(b.natural.count == 2)
        #expect(a.natural.count == 3)
        let again = preparation.measure(first, fontSize: 15)
        #expect(again.natural == a.natural)
        #expect(again.minimum == a.minimum)
        #expect(renders == 3)
        #expect(measurements == 3)
    }
}
