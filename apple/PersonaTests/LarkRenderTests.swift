import Foundation
import Testing
@testable import Persona
import PersonaWire

@MainActor
struct LarkRenderTests {
    private func parts(_ json: String) throws -> [LarkPart] {
        try JSON.decoder.decode([LarkPart].self, from: Data(json.utf8))
    }

    /// A run compared without `Color` identity: `LarkColor.text` builds a fresh dynamic provider on every call, so two
    /// equal-looking runs whose style carries a color never compare equal. The color's presence is compared instead.
    private struct ComparableRun: Equatable {
        var kind: String
        var text: String
        var bold = false
        var italic = false
        var underline = false
        var dotted = false
        var strike = false
        var code = false
        var hasColor = false
        var hasBackground = false
        var size: CGFloat?
        var link: URL?
    }

    private func comparable(_ runs: [LarkInline]) -> [ComparableRun] {
        runs.map { run in
            switch run {
            case .text(let text, let style):
                ComparableRun(kind: "text", text: text, bold: style.bold, italic: style.italic, underline: style.underline,
                              dotted: style.dotted, strike: style.strike, code: style.code, hasColor: style.color != nil,
                              hasBackground: style.background != nil, size: style.size, link: style.link)
            case .emoji(let key, let fallback):
                ComparableRun(kind: "emoji", text: key + "/" + fallback)
            case .symbol(let name, let color):
                ComparableRun(kind: "symbol", text: name, hasColor: color != nil)
            case .image(let url):
                ComparableRun(kind: "image", text: url)
            }
        }
    }

    @Test func decodesMessageWithPartsReplyAndReactions() throws {
        let json = """
        {"id":"m1","position":"9","sender_name":"A","is_mine":false,"type":"POST","text":"hi","recalled":false,"reply_count":0,
         "parts":[{"kind":"image","url":"/api/v1/lark/media/k","metadata":{"width":10,"height":20}}],
         "reply_to":{"id":"m0","sender_name":"B","text":"q"},
         "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["A","B"]}]}
        """
        let m = try JSON.decoder.decode(LarkMessage.self, from: Data(json.utf8))
        #expect(m.parts?.first?.meta("width")?.double == 10)
        #expect(m.replyTo?.senderName == "B")
        #expect(m.reactions?.first?.mine == true)
        #expect(LarkPartsView.isBare(m.parts ?? []))
    }

    @Test func rebuildsRichBlockTreeAndAdoptsNestedLists() throws {
        // OL > LI(a), OL(nested) , LI(b): the nested list moves into item a.
        let ps = try parts("""
        [{"kind":"text","text":"a","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"i1","type":"list-item"}]}},
         {"kind":"text","text":"a1","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"l2","type":"unordered-list"},{"id":"i3","type":"list-item"}]}},
         {"kind":"text","text":"b","metadata":{"blocks":[{"id":"l","type":"ordered-list"},{"id":"i2","type":"list-item"}]}},
         {"kind":"text","text":"p","metadata":{"blocks":[{"id":"p","type":"paragraph"}]}}]
        """)
        let tree = LarkRichNode.tree(ps)
        #expect(tree.count == 2)
        guard case .block(let list, let items) = tree[0] else { Issue.record("no list"); return }
        #expect(list.type == "ordered-list")
        #expect(items.count == 2)
        guard case .block(_, let first) = items[0] else { Issue.record("no item"); return }
        #expect(first.count == 2) // "a" and the adopted nested list
    }

    @Test func listMarkers() {
        #expect(LarkNodeView.marker(LarkRichBlock(id: "", type: "ordered-list", listType: 0, start: 3), index: 1) == "4.")
        #expect(LarkNodeView.marker(LarkRichBlock(id: "", type: "ordered-list", listType: 1, start: 1), index: 1) == "b.")
        #expect(LarkNodeView.marker(LarkRichBlock(id: "", type: "ordered-list", listType: 3, start: 4), index: 0) == "iv.")
        let items: [JSONValue] = [.object(["type": .string("ol"), "level": .number(0)]), .object(["type": .string("ol"), "level": .number(1)]),
                                  .object(["type": .string("ol"), "level": .number(1)]), .object(["type": .string("ol"), "level": .number(0)])]
        #expect(LarkCardList.markers(items) == ["1.", "1.", "2.", "2."])
    }

    @Test func cardMarkdownMarkupAndEntities() {
        let runs = LarkFormat.cardMarkdown("a&nbsp;b<br>see <a href='https://x.com'>x</a> <font color='red'>hot</font> &#60;tag&#x3E;", style: LarkRunStyle())
        let texts = runs.compactMap { if case .text(let s, _) = $0 { s } else { nil } }
        #expect(texts.joined() == "a\u{00A0}b\nsee x hot <tag>")
        let link = runs.first { if case .text("x", let s) = $0 { s.link != nil } else { false } }
        #expect(link != nil)
        // javascript: links stay text
        let unsafe = LarkFormat.cardMarkdown("<a href=\"javascript:alert(1)\">x</a>", style: LarkRunStyle())
        if case .text(_, let s) = unsafe[0] { #expect(s.link == nil) }
    }

    @Test func cardMarkdownRepeatedCallsKeepRunsAndStyles() {
        var base = LarkRunStyle()
        base.bold = true
        base.underline = true
        var firstLink = base
        firstLink.link = URL(string: "https://example.com/a")
        var secondLink = base
        secondLink.link = URL(string: "https://example.com/b")
        var colored = base
        colored.color = LarkColor.text("red")
        let input = "😀前<A data-id=\"1\" HREF=\"https://example.com/a\">链</A>中<a title=next HREF=https://example.com/b>次</a><FONT title=x COLOR=red>色</FONT><BR>后"
        let expected: [LarkInline] = [
            .text("😀前", base), .text("链", firstLink), .text("中", base), .text("次", secondLink),
            .text("色", colored), .text("\n", base), .text("后", base),
        ]
        for _ in 0..<20 {
            #expect(comparable(LarkFormat.cardMarkdown(input, style: base)) == comparable(expected))
        }
    }

    @Test func cardMarkdownPreservesUnicodeAndMalformedMarkup() {
        var base = LarkRunStyle()
        base.strike = true
        var linked = base
        linked.link = URL(string: "https://example.com/ok")
        let input = "😀x<a href=\"https://example.com/ok\">ok</a>🧪<a href='https://bad.example'>broken"
        let expected: [LarkInline] = [
            .text("😀x", base), .text("ok", linked), .text("🧪<a href='https://bad.example'>broken", base),
        ]
        #expect(LarkFormat.cardMarkdown(input, style: base) == expected)
        #expect(LarkFormat.cardMarkdown("<a title='x'>plain</a>", style: base) == [.text("plain", base)])
    }

    @Test func cardMarkdownThroughNestedCardNodes() {
        let text = "前<font color=red>热</font><a href='https://example.com/nested'>链</a>"
        let node = LarkCardNode(.object([
            "tag": .string("markdown"),
            "children": .array([.object([
                "tag": .string("markdown_emphasis"),
                "children": .array([.object(["tag": .string("plain_text"), "text": .string(text)])]),
            ])]),
        ]))
        var style = LarkRunStyle()
        style.italic = true
        let expected = comparable(LarkFormat.cardMarkdown(text, style: style))
        #expect(comparable(LarkCardRuns.runs(node, inMarkdown: true)) == expected)
        #expect(comparable(LarkCardRuns.runs(node, inMarkdown: true)) == expected)
        #expect(comparable(LarkCardRuns.runs(node, inMarkdown: false)) == comparable([.text(text, style)]))
    }

    @Test func lingoTermsUseUTF16Offsets() {
        let runs = LarkPartRenderer.lingoRuns("😀 联调好了", lingo: [.object(["start": .number(3), "end": .number(5)])], style: LarkRunStyle())
        let dotted = runs.compactMap { if case .text(let s, let st) = $0, st.dotted { s } else { nil } }
        #expect(dotted == ["联调"])
    }

    @Test func systemTemplateHolders() throws {
        let p = try parts("""
        [{"kind":"text","text":"刘森 创建了话题群","metadata":{"system":true,"systemTemplate":"{from_user} 创建了话题群","systemHolders":[{"key":"from_user","text":"刘森"}]}}]
        """)[0]
        let texts = LarkSystemText.runs(p).compactMap { if case .text(let s, _) = $0 { s } else { nil } }
        #expect(texts == ["刘森", " 创建了话题群"])
    }

    @Test func colorsAndFormatting() {
        #expect(LarkColor.hex("#f8fafeff") != nil)
        #expect(LarkColor.hex("#abc") != nil)
        #expect(LarkColor.hex("red") == nil)
        #expect(LarkFormat.duration(ms: 19000) == "0:19")
        #expect(LarkFormat.duration(ms: 3_725_000) == "1:02:05")
        #expect(LarkGenericSummaryCard.stripHTML("<p>a</p><b>b</b>&amp;c") == "a\nb&c")
        #expect(larkThumbEdge(points: 34) == 120)
        #expect(larkThumbEdge(points: 400) == nil)
    }

    @Test func positionsCompareAsNumbers() {
        #expect(LarkPosition.less("99", "100"))
        #expect(!LarkPosition.less("100", "99"))
        #expect(!LarkPosition.less("7", "7"))
        #expect(LarkPosition.less("-3", "0"))
        #expect(LarkPosition.less("-10", "-3"))
        #expect(LarkPosition.next("68307") == "68308")
    }

    @Test func momentsLinks() {
        #expect(LarkMoments.postID("https://applink.larkoffice.com/client/moments/detail?postId=7000000000000000001&source=share") == "7000000000000000001")
        #expect(LarkMoments.postID("https://applink.larkoffice.com/client/moments/detail?postId=abc") == nil)
        #expect(LarkMoments.postID("https://example.com/a?postId=1") == nil)
        #expect(LarkMoments.postID(nil) == nil)
    }

    @Test func reactionToggle() {
        let m = LarkMessage(id: "m", position: "1", senderName: "A", isMine: false, type: "TEXT", text: "", recalled: false, replyCount: 0,
                            reactions: [LarkReaction(key: "OK", count: 2, mine: true, users: ["我", "B"])])
        let (a, removeA) = m.togglingReaction("ok", me: "我")
        #expect(removeA && a.reactions?.first?.count == 1 && a.reactions?.first?.mine == false && a.reactions?.first?.users == ["B"])
        let (b, removeB) = a.togglingReaction("THUMBSUP", me: "我")
        #expect(!removeB && b.reactions?.count == 2 && b.reactions?.last?.mine == true)
        let (c, _) = LarkMessage(id: "m", position: "1", senderName: "A", isMine: false, type: "TEXT", text: "", recalled: false, replyCount: 0,
                                 reactions: [LarkReaction(key: "OK", count: 1, mine: true, users: ["我"])]).togglingReaction("OK", me: "我")
        #expect(c.reactions?.isEmpty == true)
    }
}
