import Foundation
import Testing
@testable import Persona

/// A computer's Browser Sign-ins model against a FakeHub (#5821): it folds more than three sites, keeps the list page's
/// search and order worked out, and clears several sites in one DELETE that names each of them.
@MainActor
struct BrowserLoginsModelTests {
    private static let path = "/api/v1/machines/mac/browser/cookies"

    private static func sites(_ names: [String]) -> String {
        "[" + names.enumerated().map { i, n in #"{"domain":"\#(n).example","count":\#(i + 1)}"# }.joined(separator: ",") + "]"
    }

    private func make(_ hub: FakeHub, toasts: Box) -> BrowserLoginsModel {
        BrowserLoginsModel(machine: "mac", deps: .init(api: { hub.client }, toast: { toasts.items.append($0.message) }))
    }

    private final class Box { var items: [String] = [] }

    @Test func foldsMoreThanThreeAndSearchesAndOrdersThem() async {
        let hub = FakeHub()
        hub.on("GET", Self.path, json: #"{"sites":\#(Self.sites(["delta", "alpha", "gamma", "beta", "alder"]))}"#)
        let page = make(hub, toasts: Box())
        await page.load(online: true)
        #expect(page.folded && page.summary.count == 5)
        #expect(page.shown.map(\.domain) == ["alder.example", "alpha.example", "beta.example", "delta.example", "gamma.example"])
        page.search("AL")
        #expect(page.shown.map(\.domain) == ["alder.example", "alpha.example"])
        page.search("")
        #expect(page.shown.count == 5)
    }

    @Test func listsThreeInPlaceAndReadsNothingOffline() async {
        let hub = FakeHub()
        hub.on("GET", Self.path, json: #"{"sites":\#(Self.sites(["a", "b", "c"]))}"#)
        let page = make(hub, toasts: Box())
        await page.load(online: false)
        #expect(page.sites == nil && hub.requests.isEmpty)
        await page.load(online: true)
        #expect(!page.folded && page.sites?.count == 3)
    }

    /// The section is rebuilt with its page's content (#6050): coming back does not read again, so an open list stays;
    /// Try Again and a change of connection do.
    @Test func comingBackKeepsWhatWasReadUntilTryAgainOrOffline() async {
        let hub = FakeHub()
        hub.on("GET", Self.path, json: #"{"sites":\#(Self.sites(["a", "b", "c", "d"]))}"#)
        let page = make(hub, toasts: Box())
        await page.appear(online: true)
        await page.appear(online: true)
        #expect(hub.sent("GET", Self.path).count == 1 && page.shown.count == 4)
        await page.load(online: true)
        #expect(hub.sent("GET", Self.path).count == 2)
        await page.appear(online: false)
        #expect(page.sites == nil && hub.sent("GET", Self.path).count == 2)
        await page.appear(online: true)
        #expect(hub.sent("GET", Self.path).count == 3 && page.sites?.count == 4)
    }

    @Test func clearsSeveralSitesInOneCallNamingEach() async {
        let hub = FakeHub()
        hub.on("GET", Self.path, json: #"{"sites":\#(Self.sites(["a", "b", "c", "d"]))}"#)
        hub.on("DELETE", Self.path, json: #"{"sites":\#(Self.sites(["a", "c"])),"removed":6}"#)
        let toasts = Box()
        let page = make(hub, toasts: toasts)
        await page.load(online: true)
        let done = await page.clear(["b.example", "d.example"])
        #expect(done)
        let sent = hub.sent("DELETE", Self.path)
        #expect(sent.count == 1)
        #expect(sent.first?.repeated == [URLQueryItem(name: "site", value: "b.example"), URLQueryItem(name: "site", value: "d.example")])
        #expect(sent.first?.query["all"] == nil)
        #expect(page.shown.map(\.domain) == ["a.example", "c.example"] && !page.folded)
        #expect(toasts.items.count == 1)
    }

    @Test func aFailedClearKeepsTheListAndSaysSo() async {
        let hub = FakeHub()
        hub.on("GET", Self.path, json: #"{"sites":\#(Self.sites(["a", "b", "c", "d"]))}"#)
        hub.on("DELETE", Self.path) { _ in throw APIError(status: 502, message: "browser busy") }
        let toasts = Box()
        let page = make(hub, toasts: toasts)
        await page.load(online: true)
        let done = await page.clear(["a.example"])
        #expect(!done && !page.busy)
        #expect(page.shown.count == 4)
        #expect(toasts.items.first?.contains("browser busy") == true)
    }

    @Test func theClientPutsRepeatedItemsAfterTheQuery() {
        let api = APIClient(config: ServerConfig(url: URL(string: "https://hub.invalid")!, token: "t"))
        let url = api.url("/api/v1/x", query: ["b": "2", "a": "1"], repeated: [URLQueryItem(name: "site", value: "z.example"), URLQueryItem(name: "site", value: "y.example")])
        #expect(url.query == "a=1&b=2&site=z.example&site=y.example")
    }
}
