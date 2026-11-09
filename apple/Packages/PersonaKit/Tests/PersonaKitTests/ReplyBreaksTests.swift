import Foundation
import Testing
@testable import PersonaKit

struct ReplyBreaksTests {
    /// A reply that showed as one block (#2088), made up: a bold line with steps 5–7 under it, then another bold line
    /// with its sentence.
    private let reply = """
    **Also fine, and cheaper**
    5. Attach the screenshot from before
    6. Note which build it was
    7. Send it to the reviewer

    **How to tell it worked**
    The page opens without a reload.
    """

    @Test func stepsGoingOnAtFiveUnderALineOfTextBecomeAList() throws {
        let out = ReplyBreaks.apply(reply)
        #expect(out == """
        **Also fine, and cheaper**

        5. Attach the screenshot from before
        6. Note which build it was
        7. Send it to the reviewer

        **How to tell it worked**
        The page opens without a reload.
        """)
        // Parsed by CommonMark, as the chat's renderer parses it: before, the steps were the paragraph's lines.
        #expect(try ordered(reply, ["Attach", "Note", "Send"]) == [false, false, false])
        #expect(try ordered(out, ["Attach", "Note", "Send"]) == [true, true, true])
        #expect(try ordered(out, ["cheaper", "reload"]) == [false, false])
    }

    @Test func aHeadingUnderALineOfTextGetsABlankLine() {
        #expect(ReplyBreaks.apply("Done.\n## Next\nMore.") == "Done.\n\n## Next\nMore.")
        #expect(ReplyBreaks.apply("Done.\n#hashtag") == "Done.\n#hashtag") // not a heading
    }

    @Test func aListStaysAsItIs() {
        for text in [
            "1. one\n2. two\n3. three",
            "4. four\nwritten on\n5. five",            // a line of item 4, then item 5: still one tight list
            "- a\n- b\n5. five",
            "- a\n  more of a\n6. six",
        ] {
            #expect(ReplyBreaks.apply(text) == text, "\(text)")
        }
        // From 1 a list interrupts the paragraph by itself; the blank line changes nothing about the blocks.
        #expect(ReplyBreaks.apply("Text\n1. one\n2. two") == "Text\n\n1. one\n2. two")
    }

    @Test func codeStaysAsItIs() {
        let fenced = "```\nprint(1)\n5. not a list\n```"
        #expect(ReplyBreaks.apply(fenced) == fenced)
        let longer = "````md\nText\n5. item\n```\n# not a heading\n````\nAfter"
        #expect(ReplyBreaks.apply(longer) == longer)
        let indented = "Text\n\n    code\n    5. still code"
        #expect(ReplyBreaks.apply(indented) == indented)
    }

    @Test func otherLinesStayAsTheyAre() {
        for text in ["One line", "Line one\nLine two", "Version 3.14 is out\nand 2.0 too", "Paid $5.\nThen 10 more", ""] {
            #expect(ReplyBreaks.apply(text) == text, "\(text)")
        }
        // A table row, then steps: the steps leave the table (a row needs no pipe, so "5. step" was a row).
        #expect(ReplyBreaks.apply("| a | b |\n| - | - |\n| 1 | 2 |\n5. step") == "| a | b |\n| - | - |\n| 1 | 2 |\n\n5. step")
    }

    /// Whether each of `words` is in an ordered list's item once `markdown` is parsed.
    private func ordered(_ markdown: String, _ words: [String]) throws -> [Bool] {
        let parsed = try AttributedString(markdown: markdown, options: .init(interpretedSyntax: .full))
        return words.map { word in
            guard let range = parsed.range(of: word), let intent = parsed[range].runs.first?.presentationIntent else { return false }
            return intent.components.contains { if case .orderedList = $0.kind { true } else { false } }
        }
    }
}
