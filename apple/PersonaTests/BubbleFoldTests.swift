import SwiftUI
import Testing
@testable import Persona

/// Your own long message folds as the web folds it (#5676, `BubbleFoldRule`): past 14 lines to 10, at the accessibility
/// text sizes past 9 to 5; a message too short to fold in the narrowest bubble is never measured; folded, only its start
/// is laid out.
struct BubbleFoldTests {
    @Test func theWebsLinesAndFewerAtAccessibilitySizes() {
        #expect(BubbleFoldRule(.large) == .regular)
        #expect(BubbleFoldRule(.xxxLarge) == .regular)
        #expect(BubbleFoldRule.regular.limit == 14)
        #expect(BubbleFoldRule.regular.shown == 10)
        #expect(BubbleFoldRule(.accessibility1) == .large)
        #expect(BubbleFoldRule(.accessibility5) == .large)
        #expect(BubbleFoldRule.large.limit == 9)
        #expect(BubbleFoldRule.large.shown == 5)
    }

    @Test func linesCountWrappedAndEmptyLines() {
        // "abc" and "" take a line each; ten characters at four a line take three.
        #expect(BubbleFoldRule.lines([3, 0, 10], perLine: 4) == 5)
        #expect(BubbleFoldRule.lengths("ab\n\n中文字") == [2, 0, 3])
    }

    @Test func charactersPerLineShrinkAsTheTextGrows() {
        #expect(BubbleFoldRule.perLine(BubbleFoldRule.narrowest, size: 17) == 9)
        #expect(BubbleFoldRule.perLine(BubbleFoldRule.narrowest, size: 53) == 2)
        #expect(BubbleFoldRule.perLine(BubbleFoldRule.narrowest, size: 500) == 1)
    }

    /// A short question is drawn as before: no fold, no measuring (the web's NARROWEST_CHARS check).
    @Test func shortMessagesNeverFold() {
        let perLine = BubbleFoldRule.perLine(BubbleFoldRule.narrowest, size: 17)
        let short = BubbleFoldRule.lengths("看一下 CI 为什么红了")
        #expect(BubbleFoldRule.lines(short, perLine: perLine) <= BubbleFoldRule.regular.limit)
        let long = BubbleFoldRule.lengths(String(repeating: "一行很长的话", count: 30))
        #expect(BubbleFoldRule.lines(long, perLine: perLine) > BubbleFoldRule.regular.limit)
    }

    @Test func foldedOnlyTheStartIsLaidOut() throws {
        // Lines: the start ends before the line past `lines`.
        let many = (1...40).map { "第 \($0) 行" }.joined(separator: "\n")
        let start = try #require(BubbleFoldRule.start(of: many, lines: 16))
        #expect(start.split(separator: "\n", omittingEmptySubsequences: false).count == 16)
        #expect(many.hasPrefix(start))
        // What a bubble draws, kept once per text: the same start.
        #expect(BubbleFoldRule.shownStart(of: many, rule: .regular, ems: nil) == start)
        #expect(BubbleFoldRule.shownStart(of: many, rule: .large, ems: nil) == BubbleFoldRule.start(of: many, lines: 11))
        // Characters: one line of 10 000 is cut at `startCharacters`.
        let wide = String(repeating: "字", count: 10_000)
        #expect(BubbleFoldRule.start(of: wide, lines: 16)?.count == BubbleFoldRule.startCharacters)
        // All of it: nothing to cut.
        #expect(BubbleFoldRule.start(of: "短的\n一句", lines: 16) == nil)
        // Never in the middle of a character made of several scalars.
        let flags = String(repeating: "🇨🇳", count: 4_000)
        let cut = try #require(BubbleFoldRule.start(of: flags, lines: 16))
        #expect(cut.count == BubbleFoldRule.startCharacters)
        #expect(cut.unicodeScalars.count == 2 * BubbleFoldRule.startCharacters)
    }

    /// A long paragraph with few line breaks (a pasted report) is cut where its lines wrap at the bubble's width, not
    /// laid out whole: wide characters an em each, letters less.
    @Test func theStartWrapsAtTheBubblesWidth() throws {
        let wide = String(repeating: "字", count: 1_000)
        #expect(BubbleFoldRule.start(of: wide, lines: 16, ems: 17)?.count == 16 * 17)
        let letters = String(repeating: "a", count: 1_000)
        #expect(BubbleFoldRule.start(of: letters, lines: 16, ems: 17)?.count == 16 * 42) // 0.4 em each
        // Line breaks still end lines: two short lines and wrapped ones after them.
        let mixed = "一\n二\n" + wide
        #expect(BubbleFoldRule.start(of: mixed, lines: 4, ems: 10) == "一\n二\n" + String(repeating: "字", count: 20))
        // Short enough: nothing to cut.
        #expect(BubbleFoldRule.start(of: "短的一句", lines: 16, ems: 17) == nil)
        // Kept by width too: another width, another start.
        let narrow = try #require(BubbleFoldRule.shownStart(of: wide, rule: .regular, ems: 10))
        let broad = try #require(BubbleFoldRule.shownStart(of: wide, rule: .regular, ems: 20))
        #expect(narrow.count == 16 * 10)
        #expect(broad.count == 16 * 20)
    }
}
