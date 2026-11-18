import Foundation
import Testing
@testable import PersonaKit

/// A little room where inline code meets a CJK letter (#9477): a thin space there, none beside a space, CJK punctuation
/// or anything else; code blocks and code in links stay as written.
struct InlineCodeGapTests {
    private let t = "\u{2009}"

    @Test func roomOnlyWhereCodeMeetsACJKLetter() {
        #expect(InlineCodeGap.apply("用`pnpm`装依赖") == "用\(t)`pnpm`\(t)装依赖")
        #expect(InlineCodeGap.apply("提交是 `3854bdc`，本地提交") == "提交是 `3854bdc`，本地提交")
        #expect(InlineCodeGap.apply("写成`设置`。再用`ja`、`en`") == "写成\(t)`设置`。再用\(t)`ja`、`en`")
        #expect(InlineCodeGap.apply("かな`x`カナ と 한글`y`") == "かな\(t)`x`\(t)カナ と 한글\(t)`y`")
        #expect(InlineCodeGap.apply("v`1.2` and `a`b") == "v`1.2` and `a`b")
        #expect(InlineCodeGap.apply("没有代码。") == "没有代码。")
    }

    @Test func codeBlocksAndLinksStayAsWritten() {
        let md = "```sh\n用`x`装\n```\n\n    用`indented`装\n\n见[`在链接里`](https://example.com)里"
        #expect(InlineCodeGap.apply(md) == md)
    }

    @Test func aMarkOfItsOwn() {
        #expect(InlineCodeGap.apply("用`a`装", gap: "\u{E000}") == "用\u{E000}`a`\u{E000}装")
    }

    @Test func theCopyLinksKeepTheRoomOutside() {
        let spaced = InlineCodeGap.apply("用`pnpm`装")
        #expect(CodeCopyMarkdown.linked(spaced) == "用\(t)[`pnpm`](\(CodeCopyURL.make("pnpm")!.absoluteString))\(t)装")
    }
}
