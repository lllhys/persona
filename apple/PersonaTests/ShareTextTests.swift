import Foundation
import Testing
@testable import Persona

/// Share › Persona's text (#2282, `ShareText`): Safari's page as its title and link, the selection quoted, every piece
/// once; and when the sheet says files take a while.
struct ShareTextTests {
    private static func plist(_ results: [String: Any]) -> [String: Any] { [ShareText.resultsKey: results] }

    @Test func readsThePageSafarisScriptReturned() {
        let page = ShareText.page(Self.plist(["title": "  A Page Worth Reading\n", "url": "https://example.com/a", "selection": ""]))
        #expect(page == ShareText.Page(title: "A Page Worth Reading", url: "https://example.com/a"))
        // The dictionary as NSItemProvider hands it over.
        let bridged = NSDictionary(dictionary: [ShareText.resultsKey: NSDictionary(dictionary: ["title": "T", "url": "https://example.com"])])
        #expect(ShareText.page(bridged)?.title == "T")
    }

    @Test func anyOtherPropertyListIsNotAPage() {
        #expect(ShareText.page(nil) == nil)
        #expect(ShareText.page(URL(fileURLWithPath: "/tmp/Settings.plist")) == nil)
        #expect(ShareText.page(["CFBundleName": "Something"]) == nil)
        #expect(ShareText.page(Self.plist(["title": "No address", "url": ""])) == nil)
    }

    @Test func aLongSelectionIsCut() {
        let page = ShareText.page(Self.plist(["title": "T", "url": "https://example.com", "selection": String(repeating: "a", count: 5000)]))
        #expect(page?.selection.count == ShareText.maxSelection + 1)
        #expect(page?.selection.hasSuffix("…") == true)
    }

    @Test func titleThenLinkWithoutRepeats() {
        let page = ShareText.Page(title: "A Page Worth Reading", url: "https://example.com/a")
        // Safari also hands over the link itself and the title as the item's text.
        let text = ShareText.compose(page: page, pieces: ["A Page Worth Reading", "https://example.com/a/"])
        #expect(text == "A Page Worth Reading\nhttps://example.com/a")
    }

    @Test func theSelectionIsQuotedAfterTheLink() {
        let page = ShareText.Page(title: "Notes", url: "https://example.com/n", selection: "first line\nsecond line")
        let text = ShareText.compose(page: page, pieces: ["first line\nsecond line", "https://example.com/n"])
        #expect(text == "Notes\nhttps://example.com/n\n\n> first line\n> second line")
    }

    @Test func aPageWithoutATitleIsItsLink() {
        #expect(ShareText.compose(page: ShareText.Page(title: "", url: "https://example.com")) == "https://example.com")
        #expect(ShareText.compose(page: ShareText.Page(title: "https://example.com/", url: "https://example.com")) == "https://example.com")
    }

    @Test func otherPiecesFollowThePageOnce() {
        let page = ShareText.Page(title: "T", url: "https://example.com/t")
        let text = ShareText.compose(page: page, pieces: ["What I think", "https://example.com/other",
                                                                  "What I think", "  "])
        #expect(text == "T\nhttps://example.com/t\nWhat I think\nhttps://example.com/other")
    }

    @Test func withoutAPageTheTextComesFirstThenTheLink() {
        // Another app's share: its title as the item's text, then the link (no script ran).
        #expect(ShareText.compose(page: nil, pieces: ["A Page Worth Reading", "https://example.com/a",
                                                      "https://example.com/a"]) == "A Page Worth Reading\nhttps://example.com/a")
        #expect(ShareText.compose(page: nil) == "")
    }

    /// Regression (#7964 review): another app whose text provider comes before its link keeps text, then link; the
    /// pieces are not regrouped by kind.
    @Test func withoutAPageATextBeforeALinkStaysFirst() {
        #expect(ShareText.compose(page: nil, pieces: ["Read this before Friday", "https://example.com/doc"])
                == "Read this before Friday\nhttps://example.com/doc")
        #expect(ShareText.compose(page: nil, pieces: ["https://example.com/doc", "Read this before Friday"])
                == "https://example.com/doc\nRead this before Friday")
    }

    /// Regression (#7964 review): with several items each caption stays beside its own attachments, in the order shared,
    /// rather than every caption first.
    @Test func captionsStayBesideTheirOwnAttachments() {
        let pieces = ["First item's note", "https://example.com/one", "Second item's note", "Some selected text",
                      "https://example.com/two"]
        #expect(ShareText.compose(page: nil, pieces: pieces) == pieces.joined(separator: "\n"))
        // A page still goes first, the rest keeping their order.
        let page = ShareText.Page(title: "T", url: "https://example.com/t")
        #expect(ShareText.compose(page: page, pieces: pieces) == "T\nhttps://example.com/t\n" + pieces.joined(separator: "\n"))
    }

    @Test func largeUploadsSayTheyTakeAWhile() {
        #expect(!ShareTargets.takesAWhile(0))
        #expect(!ShareTargets.takesAWhile(ShareTargets.slowUpload - 1))
        #expect(ShareTargets.takesAWhile(ShareTargets.slowUpload))
        #expect(ShareTargets.slowUpload < ShareTargets.fileLimit)
    }
}
