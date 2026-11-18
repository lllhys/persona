import Foundation
import Testing
@testable import PersonaKit

/// Paths in messages linked where they are there (docs/apple/ios.md「File viewer」): the same cases as the web's
/// web/src/lib/path-refs.test.ts and web/src/ui/PathLinks.test.tsx.
struct PathRefsTests {
    private func paths(_ t: String) -> [String] {
        PathRefs.find(in: t).map { r in r.line.map { "\(r.path):\($0)" + (r.column.map { ":\($0)" } ?? "") } ?? r.path }
    }

    private func slice(_ t: String, _ r: PathRef) -> String { (t as NSString).substring(with: r.range) }

    @Test func findsAbsoluteHomeAndRelativePathsWithAFolder() {
        #expect(paths("读 /tmp/persona-tasks/1664-lark-test.md 并完整执行这个任务") == ["/tmp/persona-tasks/1664-lark-test.md"])
        #expect(paths("看 ~/code/app/README.md 和 web/src/lib/api.ts") == ["~/code/app/README.md", "web/src/lib/api.ts"])
        #expect(paths("目录 /var/log/ 里") == ["/var/log/"])
    }

    @Test func takesALine() {
        #expect(paths("见 server/internal/node/files.go:63 和 web/src/ui/Markdown.tsx:322:7") == ["server/internal/node/files.go:63", "web/src/ui/Markdown.tsx:322:7"])
        #expect(paths("改了 src/a.ts:12-20，还有 /x/y/b.go#L8") == ["src/a.ts:12", "/x/y/b.go:8"])
        let t = "at src/a.ts:12."
        let r = PathRefs.find(in: t)[0]
        #expect(r.path == "src/a.ts" && r.line == 12)
        #expect(slice(t, r) == "src/a.ts:12")
    }

    @Test func endsAnASCIIPathAtChineseFullWidthPunctuationAndAFullStop() {
        #expect(paths("写到 /tmp/a.md里了") == ["/tmp/a.md"])
        #expect(paths("已写入 /tmp/out/report.md。") == ["/tmp/out/report.md"])
        #expect(paths("See /tmp/out/report.md.") == ["/tmp/out/report.md"])
        #expect(paths("（/tmp/a/b.txt）") == ["/tmp/a/b.txt"])
        let t = "See /tmp/out/report.md."
        #expect(slice(t, PathRefs.find(in: t)[0]) == "/tmp/out/report.md")
    }

    @Test func takesNamesInAnyScriptOutsideBackticks() {
        // a Chinese file name, Chinese folders, with and without an extension, a folder
        #expect(paths("已保存到 /home/me/.persona/assistant/report/报表_2026H2.xlsx") == ["/home/me/.persona/assistant/report/报表_2026H2.xlsx"])
        #expect(paths("在 ~/文档/项目/notes.md 里") == ["~/文档/项目/notes.md"])
        #expect(paths("合同在 /home/me/资料/合同 和 /var/log/归档/") == ["/home/me/资料/合同", "/var/log/归档/"])
        // Japanese, Korean, accents (a combining one too), mixed scripts
        #expect(paths("~/ドキュメント/会議メモ.txt と /Users/me/デスクトップ/レポート.pdf") == ["~/ドキュメント/会議メモ.txt", "/Users/me/デスクトップ/レポート.pdf"])
        #expect(paths("~/문서/보고서.hwp, ~/Café/menu\u{301}.txt") == ["~/문서/보고서.hwp", "~/Café/menu\u{301}.txt"])
        #expect(paths("/srv/data/2026年/sales-売上_v2.csv") == ["/srv/data/2026年/sales-売上_v2.csv"])
        // the line, with a Chinese word after it
        #expect(paths("见 /tmp/报告/main.go:12行 和 ~/项目/说明.md#L3") == ["/tmp/报告/main.go:12", "~/项目/说明.md:3"])
        // a relative one has an ASCII first folder in text, so the words before it stay out
        #expect(paths("请看docs/设计/说明.md") == ["docs/设计/说明.md"])
        #expect(paths("请看文档/报告.md").isEmpty)
        // where it is: UTF-16, past a character outside the BMP
        let t = "𠀋 /tmp/𠀋/报告.md"
        #expect(slice(t, PathRefs.find(in: t)[0]) == "/tmp/𠀋/报告.md")
    }

    @Test func endsAPathAtChineseAndJapanesePunctuationNotAtTheNames() {
        #expect(paths("已写入 /tmp/报告/第四季度.docx。") == ["/tmp/报告/第四季度.docx"])
        #expect(paths("先看 /tmp/报告.md，再看 /tmp/数据.csv、/tmp/图表.png；") == ["/tmp/报告.md", "/tmp/数据.csv", "/tmp/图表.png"])
        #expect(paths("（/tmp/资料/a.txt）「~/笔记/待办.md」『~/メモ/買い物.txt』") == ["/tmp/资料/a.txt", "~/笔记/待办.md", "~/メモ/買い物.txt"])
        #expect(paths("见：/tmp/报告.md！") == ["/tmp/报告.md"])
    }

    @Test func endsANameBeforeAChineseOrJapaneseWordRightAfterItsExtension() {
        #expect(paths("写到 /tmp/报告.md里了") == ["/tmp/报告.md"])
        #expect(paths("~/ドキュメント/メモ.txtを開いて") == ["~/ドキュメント/メモ.txt"])
        // two paths joined by the Chinese word for "and"
        #expect(paths("/tmp/报告.md和/tmp/数据.csv") == ["/tmp/报告.md", "/tmp/数据.csv"])
    }

    @Test func leavesURLsWordsWithASlashVersionsAndRootsAlone() {
        #expect(paths("https://example.com/a/b.md and //cdn/x/y.js").isEmpty)
        #expect(paths("model deepseek/v4.1-flash, and/or, /tmp alone, 1/2").isEmpty)
        #expect(paths("a ~ b / c").isEmpty)
        #expect(paths("网址 https://example.com/a/b 不算").isEmpty)
        // a path right after a URL and its punctuation
        #expect(paths("链接 https://example.com/a，文件在/tmp/报告/a.md") == ["/tmp/报告/a.md"])
    }

    @Test func takesAPathInBackticksWhole() {
        #expect(paths("生成了 `~/报告/第四季度.docx`") == ["~/报告/第四季度.docx"])
        let r = PathRefs.find(in: "在 `~/My Notes/todo.md:3`")[0]
        #expect(r.path == "~/My Notes/todo.md" && r.line == 3)
        let s = PathRefs.find(in: "`/tmp/a.md:12`")[0]
        #expect(s.path == "/tmp/a.md" && s.line == 12) // the line comes off a whole span too
        // both the whole span and the paths in it are candidates; what is there decides
        #expect(paths("`/tmp/a.md 和 /tmp/b.md`") == ["/tmp/a.md 和 /tmp/b.md", "/tmp/a.md", "/tmp/b.md"])
    }

    @Test func asksAboutEachPathOnce() {
        #expect(PathRefs.unique(PathRefs.find(in: "src/a.ts:1 src/a.ts:2 `src/a.ts`")) == ["src/a.ts"])
    }

    @Test func aCodeSpanThatIsAWholePath() {
        #expect(PathRefs.codePathRef("src/a.ts:12").map { [$0.path, "\($0.line ?? 0)"] } == ["src/a.ts", "12"])
        let r = PathRefs.codePathRef(" /tmp/x/y.md ")
        #expect(r?.path == "/tmp/x/y.md" && r?.range == NSRange(location: 1, length: 11))
        #expect(PathRefs.codePathRef("~/报告/第四季度.docx")?.path == "~/报告/第四季度.docx")
        let rel = PathRefs.codePathRef("docs/设计/说明.md:7")
        #expect(rel?.path == "docs/设计/说明.md" && rel?.line == 7 && rel?.whole == true)
        #expect(PathRefs.codePathRef("和/或") == nil)
        #expect(PathRefs.codePathRef("ls /tmp/x/y.md") == nil)
        #expect(PathRefs.codePathRef("/tmp") == nil)
        #expect(PathRefs.codePathRef("https://example.com/a/b") == nil)
    }

    @Test func picksTheOnesThereTheLongerFirstNoneOverlapping() {
        let t = "`/tmp/a.md 和 /tmp/b.md`"
        let there: Set = ["/tmp/a.md", "/tmp/b.md"]
        #expect(PathRefs.pick(PathRefs.find(in: t)) { there.contains($0) }.map(\.path) == ["/tmp/a.md", "/tmp/b.md"])
        #expect(PathRefs.pick(PathRefs.find(in: t)) { _ in true }.map(\.path) == ["/tmp/a.md 和 /tmp/b.md"])
        #expect(PathRefs.pick(PathRefs.find(in: "x /tmp/a/b.md")) { _ in false }.isEmpty)
    }

    @Test func whileStreamingLeavesOutTheWordStillBeingWritten() {
        #expect(PathRefs.unique(PathRefs.find(in: PathRefs.written("写到 /tmp/persona-tasks/1664-lark-test.md 了，再看 /tmp/jar"))) == ["/tmp/persona-tasks/1664-lark-test.md"])
        #expect(PathRefs.written("/tmp/jar") == "")
        #expect(PathRefs.written("a /tmp/x/y.md\n") == "a /tmp/x/y.md")
    }

    @Test func theLinkCarriesThePathAndTheLine() throws {
        let url = try #require(PathLinkURL.make("~/My Notes/报告 (1).md", line: 12))
        #expect(!url.absoluteString.contains(" ") && !url.absoluteString.contains("("))
        let back = try #require(PathLinkURL.parse(url))
        #expect(back.path == "~/My Notes/报告 (1).md" && back.line == 12)
        #expect(PathLinkURL.parse(try #require(PathLinkURL.make("src/a.ts", line: nil)))?.line == nil)
        #expect(PathLinkURL.parse(URL(string: "https://example.com/a")!) == nil)
    }

    /// Each ASCII letter or digit could be taken two ways, so a key ending in a hash was tried 2^n ways: seconds for
    /// this 16-character last part, minutes for 22, hours for a 32-character hash (#8903).
    @Test func finishesAtOnceOnALongLastPartWithNoExtension() {
        let key = "bucket/2024/1/1/0123456789abcdef"
        let hash32 = "0123456789abcdef0123456789abcdef"
        let started = ContinuousClock.now
        #expect(paths("the image key is \(key) in the store") == [])
        #expect(paths("the image key is `\(key)`") == [])
        #expect(paths("saved to /\(key)") == ["/\(key)"])
        #expect(paths("saved to bucket/2024/1/1/\(hash32).png") == ["bucket/2024/1/1/\(hash32).png"])
        #expect(paths("saved to ~/报告/\(hash32)") == ["~/报告/\(hash32)"])
        #expect(ContinuousClock.now - started < .milliseconds(500))
    }

    /// The answer an iPhone froze on until the watchdog killed it (#9173): model names in code spans, an organization
    /// then a long ASCII name with no extension. Before #8911 each was tried 2^n ways in the row's body: 120 ms for a
    /// 28-character one, more than five seconds (never, in practice) for the 33 to 40-character ones below.
    @Test func finishesAtOnceOnModelNamesInCodeSpans() {
        let names = ["acme/halfbody_person_detection", "acme/head-detection-v2", "acme/image-eva02-large-tagger-v1",
                     "vendor/dinov3-vitl16-pretrain-lvd1689m", "lab/eva02_large_patch14_clip_336", "acme/ml-tagger-onnx"]
        let answer = "模型已经在后台下载：\n\n| 模型 | 大小 |\n|---|---:|\n"
            + names.map { "| `\($0)` v1.0_s | 1.2 GB |" }.joined(separator: "\n")
            + "\n\n下载日志：`work/download_heavy.log`，清单：`work/models/manifest.json`。对比图：`~/Pictures/对比图-v2.1/正面vs背面.jpg`、`侧面vs背面.jpg`。"
        let started = ContinuousClock.now
        for name in names { #expect(PathRefs.codePathRef(name) == nil, "\(name) is no file") }
        #expect(PathMarkdown(answer).paths == ["work/download_heavy.log", "work/models/manifest.json", "~/Pictures/对比图-v2.1/正面vs背面.jpg"])
        #expect(ContinuousClock.now - started < .milliseconds(200))
    }
}

/// An answer's Markdown with its paths linked: only the ones there, in text and whole code spans, with the line;
/// code blocks, links and formulas left alone.
struct PathMarkdownTests {
    private let there: Set = ["src/a.ts", "~/notes/"]
    private func link(_ p: String, _ line: Int? = nil) -> String { PathLinkURL.make(p, line: line)!.absoluteString }

    @Test func linksTextAndCodeSpansWithTheLineAndLeavesCodeBlocksAndLinksAlone() {
        let md = "改了 `src/a.ts:12`，见 src/a.ts:40 和 ~/notes/。\n\n```\ncat src/a.ts\n```\n\n[链接](https://example.com/src/a.ts) 和 <https://example.com/x/src/a.ts>"
        let p = PathMarkdown(md)
        #expect(p.paths == ["src/a.ts", "~/notes/"])
        let out = p.linked { there.contains($0) }
        #expect(out.paths == ["src/a.ts", "~/notes/"])
        #expect(out.markdown == "改了 [`src/a.ts:12`](\(link("src/a.ts", 12)))，见 [src/a.ts:40](\(link("src/a.ts", 40))) 和 [~/notes/](\(link("~/notes/")))。"
                + "\n\n```\ncat src/a.ts\n```\n\n[链接](https://example.com/src/a.ts) 和 <https://example.com/x/src/a.ts>")
    }

    @Test func linksOnlyThePathsThatAreThere() {
        let md = "读 /tmp/persona-tasks/1664-lark-test.md 并完整执行这个任务，别动 /tmp/other/x.md"
        let out = PathMarkdown(md).linked { $0 == "/tmp/persona-tasks/1664-lark-test.md" }
        #expect(out.markdown == "读 [/tmp/persona-tasks/1664-lark-test.md](\(link("/tmp/persona-tasks/1664-lark-test.md"))) 并完整执行这个任务，别动 /tmp/other/x.md")
        // nothing there (or no answer yet): the same Markdown
        #expect(PathMarkdown(md).linked { _ in false }.markdown == md)
    }

    @Test func leavesFormulasIndentedCodeAndCodeSpansThatAreNotPathsAlone() {
        // a formula is an inline image by now (`MathMarkdown`)
        let md = "公式 ![src/a.ts](persona-math:src%2Fa.ts) 和 `cat src/a.ts`\n\n    src/a.ts"
        let p = PathMarkdown(md)
        #expect(p.paths.isEmpty)
    }

    /// Where code is drawn in the body's color (iOS, #2984), a path's code span is linked as its words, so it stays blue.
    @Test func aCodeSpanLinkedAsItsWords() {
        let md = "见 `src/__init__.py:3` 和 `~/notes/a[1].md`。"
        let p = PathMarkdown(md)
        let out = p.linked(plainCode: true) { _ in true }.markdown
        #expect(out == "见 [src/\\_\\_init\\_\\_.py:3](\(link("src/__init__.py", 3))) 和 [\\~/notes/a\\[1\\].md](\(link("~/notes/a[1].md")))。")
        #expect(p.linked { _ in true }.markdown.contains("[`src/__init__.py:3`]"))
    }

    @Test func linksAPathWhoseNamesAreNotASCIIWholeAndLeavesTheWordsGluedToItOut() {
        let md = "已保存到 /home/me/report/报表_2026H2.xlsx，摘要在 ~/ドキュメント/メモ.txtにあります。"
        let there: Set = ["/home/me/report/", "/home/me/report/报表_2026H2.xlsx", "~/ドキュメント/メモ.txt"]
        let p = PathMarkdown(md)
        #expect(p.paths == ["/home/me/report/报表_2026H2.xlsx", "~/ドキュメント/メモ.txt"])
        let out = p.linked { there.contains($0) }
        #expect(out.markdown == "已保存到 [/home/me/report/报表_2026H2.xlsx](\(link("/home/me/report/报表_2026H2.xlsx")))，"
                + "摘要在 [~/ドキュメント/メモ.txt](\(link("~/ドキュメント/メモ.txt")))にあります。")
    }

    @Test func aWholeSpanWithChineseAndSpaces() {
        let md = "生成了 `~/报告/第四季度 终稿.docx`。"
        let out = PathMarkdown(md).linked { $0 == "~/报告/第四季度 终稿.docx" }
        #expect(out.markdown == "生成了 [`~/报告/第四季度 终稿.docx`](\(link("~/报告/第四季度 终稿.docx")))。")
    }
}

/// Where a line's links are, so its paths and code spans are left alone (`PathMarkdown.protectedRanges`, #9173): the
/// matches the expression it replaced found, in one pass.
struct ProtectedRangesTests {
    /// The expression the scanner replaced: the reference for what it has to find.
    private static let expression = try! NSRegularExpression(pattern: #"!?\[[^\]\n]*\]\([^)\n]*\)|!?\[[^\]\n]*\]\[[^\]\n]*\]|<[^\s<>]+>|^\s*\[[^\]\n]+\]:.*$"#)

    private func expected(_ line: String) -> [NSRange] {
        Self.expression.matches(in: line, range: NSRange(location: 0, length: (line as NSString).length)).map(\.range)
    }

    /// The same numbers on every run, so a failure can be run again.
    private struct Seeded: RandomNumberGenerator {
        var state: UInt64
        mutating func next() -> UInt64 {
            state &+= 0x9E37_79B9_7F4A_7C15
            var z = state
            z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
            z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
            return z ^ (z >> 31)
        }
    }

    @Test func findsWhatTheExpressionFound() {
        let written = ["见 [文档](https://example.com/a/b.md) 和 ![公式](persona-math:x%2Fy)", "[a][ref] 和 [b][]", "<https://example.com/x>",
                       "  [ref]: https://example.com/a.md \"标题\"", "[ref]:", "[]: x", "[a](b", "[a]", "![a](u) ![b](", "<a b>", "<>",
                       "[a](u)[b](v)<w>", "!![a](u)", "[[a](u)", "[a]]((u)", "[a](u))", "\u{3000}[r]: x", "[r]: x\r", ""]
        for line in written { #expect(PathMarkdown.protectedRanges(in: line) == expected(line), "\(line)") }
        var rng = Seeded(state: 9173)
        let pieces = ["[", "]", "(", ")", "<", ">", "!", ":", " ", "\t", "\u{3000}", "a", "中", "/", ".md"]
        for _ in 0..<5_000 {
            let line = (0..<Int.random(in: 0...24, using: &rng)).map { _ in pieces.randomElement(using: &rng)! }.joined()
            #expect(PathMarkdown.protectedRanges(in: line) == expected(line), "\(line)")
        }
    }

    /// The expression looked for each `[`'s `]` from scratch: 1.5 s for this line, and copying code had no time limit.
    @Test func staysFastOnALineOfUnclosedBrackets() {
        let lines = [String(repeating: "[", count: 8_000), String(repeating: "![x](u ", count: 1_200), String(repeating: "[a](", count: 2_000)]
        let started = ContinuousClock.now
        for line in lines {
            #expect(PathMarkdown.protectedRanges(in: line).isEmpty)
            #expect(PathMarkdown(line + " `src/a.ts`").paths == ["src/a.ts"])
            #expect(CodeCopyMarkdown.linked(line + " `x`").hasSuffix("(\(CodeCopyURL.make("x")!.absoluteString))"))
        }
        #expect(ContinuousClock.now - started < .milliseconds(300))
    }
}
