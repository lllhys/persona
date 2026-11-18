import Foundation
import Testing
@testable import Persona
import PersonaWire

struct AssistantViewBlocksTests {
    private static let fixtures = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().appending(path: "schema/extension/examples/assistant-text/fences.json")
    private static let valid = #"{"fallback":"示例卡片","sections":[]}"#

    private struct Fixture: Decodable {
        let name: String
        let text: String
        let indexes: [Int]
        let complete: [Bool]
    }

    @Test func sharedGoFenceVectors() throws {
        let fixtures = try JSONDecoder().decode([Fixture].self, from: Data(contentsOf: Self.fixtures))
        #expect(!fixtures.isEmpty)
        for fixture in fixtures {
            let blocks = AssistantViewBlocks.parse(fixture.text)
            // Go's extractor also returns legacy cards with index -1; this parser intentionally does not.
            let expected = zip(fixture.indexes, fixture.complete).filter { $0.0 >= 0 }
            #expect(blocks.map(\.ordinal) == expected.map(\.0), "\(fixture.name): ordinals")
            #expect(blocks.map(\.closed) == expected.map(\.1), "\(fixture.name): closure")
            #expect(outside(fixture.text, blocks: blocks).count == blocks.count + 1)
        }
    }

    @Test(arguments: ["\n", "\r\n", "\r"])
    func rangesPreservePlainTextAndOriginalNewlines(_ newline: String) throws {
        let prefix = "前言🧪 e\u{301}" + newline
        let between = "普通文字和 `persona-view`" + newline + newline
        let suffix = "结尾👩🏽‍💻" + newline
        let firstContent = "invalid" + newline + "第二行" + newline
        let secondContent = Self.valid + newline
        let firstFence = "  ```PERSONA-VIEW extra" + newline + firstContent + " ```\t" + newline
        let secondFence = "~~~persona-view" + newline + secondContent + "~~~~  " + newline
        let text = prefix + firstFence + between + secondFence + suffix
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 2)
        #expect(blocks.map(\.ordinal) == [0, 1])
        #expect(blocks.allSatisfy { $0.closed })
        expectBytes(blocks[0].content, firstContent)
        expectBytes(blocks[1].content, secondContent)
        expectBytes(String(text[blocks[0].range]), firstFence)
        expectBytes(String(text[blocks[1].range]), secondFence)
        let plain = outside(text, blocks: blocks)
        #expect(plain.count == 3)
        for (actual, expected) in zip(plain, [prefix, between, suffix]) { expectBytes(actual, expected) }
        let restored = plain[0] + String(text[blocks[0].range]) + plain[1] + String(text[blocks[1].range]) + plain[2]
        expectBytes(restored, text)
    }

    @Test func rangesRemainExactAtUnicodeScalarBoundaries() throws {
        let prefix = "🧪\r\n"
        let content = "e\u{301}\r\n"
        let fence = "```persona-view\r\n" + content + "```\r\n"
        // A combining mark after a newline can share a Swift Character with that newline.
        let suffix = "\u{301}原样保留"
        let text = prefix + fence + suffix
        let block = try #require(AssistantViewBlocks.parse(text).first)
        expectBytes(block.content, content)
        expectBytes(String(text[block.range]), fence)
        let plain = outside(text, blocks: [block])
        expectBytes(plain[0], prefix)
        expectBytes(plain[1], suffix)
    }

    @Test func invalidBlocksCountButLegacyCardsAndExamplesDoNot() throws {
        let old = "```persona-card\n{\"title\":\"旧卡片\"}\n```\n"
        let invalid = "```persona-view\ninvalid\n```\n"
        let example = "````markdown\n```persona-view\n" + Self.valid + "\n```\n````\n"
        let valid = "~~~persona-view\n" + Self.valid + "\n~~~\n"
        let partial = "```persona-view\n{\"fallback\":"
        let text = old + invalid + example + valid + partial
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 3)
        #expect(blocks.map(\.ordinal) == [0, 1, 2])
        #expect(blocks.map(\.closed) == [true, true, false])
        #expect(blocks.map(\.content) == ["invalid\n", Self.valid + "\n", "{\"fallback\":"])
        #expect(outside(text, blocks: blocks) == [old, example, "", ""])
    }

    @Test func repeatedContentHasDistinctOrdinalsAndRanges() async throws {
        let fence = "```persona-view\n" + Self.valid + "\n```\n"
        let text = fence + "中间\n" + fence
        let blocks = await Task.detached { AssistantViewBlocks.parse(text) }.value
        try #require(blocks.count == 2)
        #expect(blocks.map(\.ordinal) == [0, 1])
        #expect(blocks[0].content == blocks[1].content)
        #expect(blocks[0].range != blocks[1].range)
        #expect(Set(blocks).count == 2, "block data is Hashable and usable off the main actor")
        expectBytes(String(text[blocks[0].range]), fence)
        expectBytes(String(text[blocks[1].range]), fence)
    }

    @Test(arguments: ["", "\n", "\r\n", "\r"])
    func anOpeningFenceAtEOFIsAnEmptyPartialBlock(_ newline: String) throws {
        let prefix = "前言\n"
        let fence = "```persona-view" + newline
        let text = prefix + fence
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(blocks[0].ordinal == 0 && !blocks[0].closed && blocks[0].content.isEmpty)
        expectBytes(String(text[blocks[0].range]), fence)
        #expect(blocks[0].range.upperBound == text.endIndex)
        #expect(outside(text, blocks: blocks) == [prefix, ""])
    }

    @Test(arguments: ["\n", "\r\n", "\r"])
    func aClosingFenceAtEOFNeedsNoNewline(_ newline: String) throws {
        let content = Self.valid + newline
        let text = "```persona-view" + newline + content + "```"
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(blocks[0].closed && blocks[0].range == text.startIndex..<text.endIndex)
        expectBytes(blocks[0].content, content)
        expectBytes(String(text[blocks[0].range]), text)
    }

    @Test(arguments: ["```", "~~~~", "```` not-a-close", "````\u{200B}", "    ````"])
    func incompleteClosersRemainInThePartialContent(_ closer: String) throws {
        let content = "invalid\r\n" + closer + "\r\n"
        let text = "````persona-view\r\n" + content
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(!blocks[0].closed && blocks[0].ordinal == 0)
        expectBytes(blocks[0].content, content)
        expectBytes(String(text[blocks[0].range]), text)
    }

    @Test(arguments: ["```markdown", "````markdown", "~~~~markdown", "```", "~~~"])
    func ordinaryOuterFencesSuppressExamples(_ opening: String) {
        let character = opening.first == "~" ? "~" : "`"
        let length = opening.prefix { String($0) == character }.count
        let closing = String(repeating: character, count: length)
        let text = opening + "\n```persona-view\n" + Self.valid + "\n```\n" + closing + "\n"
        #expect(AssistantViewBlocks.parse(text).isEmpty)
    }

    @Test func longerOuterFenceEndsBeforeTheNextStandaloneView() throws {
        let example = "````markdown\n~~~persona-view\ninvalid\n~~~\n```persona-view\ninvalid\n```\n````\n"
        let real = "```persona-view\n" + Self.valid + "\n```\n"
        let text = example + real
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(blocks[0].ordinal == 0 && blocks[0].closed)
        expectBytes(String(text[blocks[0].range]), real)
        #expect(outside(text, blocks: blocks) == [example, ""])
    }

    @Test(arguments: ["", " ", "  ", "   "])
    func upToThreeLiteralSpacesAreAllowed(_ indent: String) throws {
        let text = indent + "~~~persona-view\n" + Self.valid + "\n" + indent + "~~~~\t\n"
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(blocks[0].closed)
        expectBytes(String(text[blocks[0].range]), text)
    }

    @Test(arguments: ["    ", "\t", " \t", "\u{A0}", "> ", "- ", "inline "])
    func onlyStandaloneFencesAreRecognized(_ prefix: String) {
        let text = prefix + "```persona-view\ninvalid\n" + prefix + "```\n"
        #expect(AssistantViewBlocks.parse(text).isEmpty)
    }

    @Test(arguments: ["persona-view", "PERSONA-VIEW", "PeRsOnA-ViEw", "PERSONA-VİEW"])
    func languageUsesGoLowercaseRules(_ language: String) throws {
        let text = "```" + language + " extra-info\n" + Self.valid + "\n```\n"
        let block = try #require(AssistantViewBlocks.parse(text).first)
        #expect(block.ordinal == 0 && block.closed)
    }

    @Test(arguments: ["persona-card", "persona-viewer", "persona-vıew", "persona-vI\u{307}ew", "persona-view\u{200B}"])
    func otherLanguagesAreNotViews(_ language: String) {
        let text = "```" + language + "\ninvalid\n```\n"
        #expect(AssistantViewBlocks.parse(text).isEmpty)
    }

    @Test func backtickInfoIsCheckedBeyondItsFirstWord() throws {
        let rejected = "```persona-view extra`info\ninvalid\n```\n"
        #expect(AssistantViewBlocks.parse(rejected).isEmpty)
        let accepted = "~~~persona-view extra`info\n" + Self.valid + "\n~~~\n"
        let block = try #require(AssistantViewBlocks.parse(accepted).first)
        #expect(block.closed && block.ordinal == 0)
        expectBytes(block.content, Self.valid + "\n")
    }

    @Test(arguments: ["\t", "\u{B}", "\u{C}", " ", "\u{85}", "\u{A0}", "\u{1680}",
                      "\u{2000}", "\u{200A}", "\u{2028}", "\u{2029}", "\u{202F}", "\u{205F}", "\u{3000}"])
    func infoAndClosersUseGoWhitespace(_ whitespace: String) throws {
        let text = "```" + whitespace + "PERSONA-VIEW" + whitespace + "extra\n" + Self.valid + "\n```" + whitespace + "\n"
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 1)
        #expect(blocks[0].closed)
        expectBytes(blocks[0].content, Self.valid + "\n")
    }

    @Test(arguments: ["\u{85}", "\u{2028}", "\u{2029}"])
    func unicodeNewlineScalarsAreNotPhysicalLineBreaks(_ separator: String) {
        let text = "plain" + separator + "```persona-view\ninvalid\n```\n"
        #expect(AssistantViewBlocks.parse(text).isEmpty)
    }

    @Test func extractionDoesNotTruncateAtGoValidationLimits() throws {
        #expect(AssistantViewBlocks.maxBlocks == 3)
        #expect(AssistantViewBlocks.maxContentBytes == 64 << 10)
        let atLimit = String(repeating: "x", count: AssistantViewBlocks.maxContentBytes - 1) + "\n"
        let overLimit = String(repeating: "é", count: AssistantViewBlocks.maxContentBytes / 2) + "\n"
        #expect(atLimit.utf8.count == AssistantViewBlocks.maxContentBytes)
        #expect(overLimit.count < AssistantViewBlocks.maxContentBytes && overLimit.utf8.count > AssistantViewBlocks.maxContentBytes)
        let contents = ["invalid\n", atLimit, overLimit, Self.valid + "\n", "partial"]
        let text = contents.dropLast().map { "```persona-view\n" + $0 + "```\n" }.joined() + "```persona-view\n" + contents[4]
        let blocks = AssistantViewBlocks.parse(text)
        try #require(blocks.count == 5)
        #expect(blocks.map(\.ordinal) == [0, 1, 2, 3, 4])
        #expect(blocks.map(\.closed) == [true, true, true, true, false])
        for (block, content) in zip(blocks, contents) { expectBytes(block.content, content) }
    }

    @Test func emptyAndNonFenceTextHaveNoBlocks() {
        for text in ["", "plain\r\ntext", "``persona-view\ninvalid\n``\n", "~~persona-view\ninvalid\n~~\n"] {
            #expect(AssistantViewBlocks.parse(text).isEmpty)
        }
    }

    private func outside(_ text: String, blocks: [AssistantViewBlocks.Block]) -> [String] {
        var parts: [String] = []
        var position = text.startIndex
        for block in blocks {
            #expect(block.range.lowerBound >= position && block.range.upperBound <= text.endIndex)
            parts.append(String(text[position..<block.range.lowerBound]))
            position = block.range.upperBound
        }
        parts.append(String(text[position...]))
        return parts
    }

    private func expectBytes(_ actual: String, _ expected: String) {
        #expect(Array(actual.utf8) == Array(expected.utf8))
    }
}
