import Foundation
import PersonaKit
import Testing
@testable import Persona

// The app's side of what moved to PersonaKit (#2095): the table view's cell widths, the fences drawn as pictures, the
// chips' list of paths. DocumentText, TableParsing, MathMarkdown and Links themselves are tested in PersonaKit.
struct PersonaKitAppSideTests {
    @Test func cellWidths() {
        #expect(TableFileModel.cells("abc") == 3)
        #expect(TableFileModel.cells("上海") == 4)
        #expect(TableFileModel.cells("") == 0)
    }

    @Test func fencesDrawnAsPictures() {
        #expect(RenderedFence.kind("math") == .displayMath)
        #expect(RenderedFence.kind("LaTeX") == .displayMath)
        #expect(RenderedFence.kind("mermaid") == .mermaid)
        #expect(RenderedFence.kind("swift") == nil)
        #expect(RenderedFence.kind(nil) == nil)
    }

    /// Paths end at the same punctuation as links (full-width too), and before a CJK word right after an extension.
    @Test func pathsEndLikeLinks() {
        #expect(FilePaths.find(in: "改了 ios/Persona/Foo.swift里的函数") == ["ios/Persona/Foo.swift"])
        #expect(FilePaths.find(in: "日志在 /var/log/app.log，看一下") == ["/var/log/app.log"])
        #expect(FilePaths.find(in: "（~/.config/app/x.json）") == ["~/.config/app/x.json"])
    }

    /// In backticks a path is whole, Chinese names and spaces too (the files an agent made, #1543): not cut at the
    /// first space into its folder. Outside them paths end at a space or punctuation; order is the text's.
    @Test func aPathInBackticksIsWhole() {
        #expect(FilePaths.find(in: "对比文档 `/Users/me/code/blog/docs/季度报告.docx`，表格 `~/out/Q4 差异.xlsx`")
                == ["/Users/me/code/blog/docs/季度报告.docx", "~/out/Q4 差异.xlsx"])
        #expect(FilePaths.find(in: "先看 /var/log/app.log，再看 `/srv/报告/汇总.pptx`") == ["/var/log/app.log", "/srv/报告/汇总.pptx"])
        #expect(FilePaths.find(in: "`/a` 和 `https://example.com/x` 和 `npm run build`").isEmpty)
        #expect(FilePaths.find(in: "改了 `ios/Persona/Foo.swift` 里的") == ["ios/Persona/Foo.swift"])
    }

    @Test func pictureKeys() {
        let a = MarkdownRenderer.key(.mermaid, "graph TD; A-->B", dark: true, points: 17)
        #expect(a != MarkdownRenderer.key(.mermaid, "graph TD; A-->B", dark: false, points: 17))
        // A formula is a template image: one picture for both appearances.
        #expect(MarkdownRenderer.key(.displayMath, "x", dark: true, points: 17) == MarkdownRenderer.key(.displayMath, "x", dark: false, points: 17))
        #expect(MarkdownRenderer.key(.displayMath, "x", dark: false, points: 17) != MarkdownRenderer.key(.inlineMath, "x", dark: false, points: 17))
    }
}
