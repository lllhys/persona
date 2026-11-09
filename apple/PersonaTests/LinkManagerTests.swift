import Foundation
import Testing
@testable import Persona

/// The document reads, recorded per host (each test reads from its own) and refused: the reader's reads go straight to
/// the network, past `FakeHub`'s transport.
private final class DocsStub: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var seen: [String: [URLRequest]] = [:]

    static func requests(to host: String) -> [URLRequest] { lock.withLock { seen[host] ?? [] } }

    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasSuffix(".docs.invalid") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let url = request.url!
        Self.lock.withLock { Self.seen[url.host ?? "", default: []].append(request) }
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: 404, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!,
                            cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(#"{"error":"not found"}"#.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

/// A link tapped in a chat or an inbox item reads through that chat's or item's manager (docs/assistant/managers.md
/// "Clients"): the native document reader and Moments post name it on every request and picture, the default's name
/// none, and what is kept of a picture or file is kept per manager. Made-up values.
@MainActor
struct LinkManagerTests {
    private static let header = "X-Persona-Manager"
    private static let doc = "https://open.feishu.cn/docx/ExampleDocument"
    private static let post = "https://applink.larkoffice.com/client/moments/detail?postId=7000000000000000001"

    private static let roster = ManagerRoster(managers: [
        HubManager(id: "assistant", name: "Home", online: true, isDefault: true),
        HubManager(id: "m-2", name: "Work", online: true),
    ])

    private func client(_ host: String) -> APIClient {
        var c = APIClient(config: ServerConfig(url: URL(string: "https://\(host)")!, token: "t"))
        let s = URLSessionConfiguration.ephemeral
        s.protocolClasses = [DocsStub.self]
        c.session = URLSession(configuration: s)
        return c
    }

    // MARK: which manager

    @Test func aLinkReadsThroughItsChatsManager() {
        #expect(ManagerRoster.reading("m-2", roster: Self.roster) == "m-2")
        #expect(ManagerRoster.reading("assistant", roster: Self.roster) == nil, "the default's names none")
        #expect(ManagerRoster.reading(nil, roster: Self.roster) == nil && ManagerRoster.reading("", roster: Self.roster) == nil)
        #expect(ManagerRoster.reading("gone", roster: Self.roster) == nil, "one the roster hasn't installed: the default's")
        let moved = ManagerRoster(managers: Self.roster.managers, defaultID: "m-2")
        #expect(ManagerRoster.reading("m-2", roster: moved) == nil && ManagerRoster.reading("assistant", roster: moved) == "assistant")
        // Before the roster is read: any but the hub's own assistant.
        #expect(ManagerRoster.reading("m-2", roster: nil) == "m-2" && ManagerRoster.reading("assistant", roster: nil) == nil)
    }

    @Test func onlyTheHubTheReadersReadThroughNamesOne() {
        let model = AppModel()
        model.managerRosters[model.hub?.group ?? ""] = Self.roster
        #expect(model.linkManager("m-2") == "m-2" && model.linkManager("m-2", group: model.hub?.group ?? "") == "m-2")
        #expect(model.linkManager("assistant") == nil && model.linkManager(nil) == nil)
        #expect(model.linkManager("m-2", group: "another-hub") == nil, "another group's chat: its managers aren't this hub's")
    }

    @Test func theRouteCarriesTheManager() throws {
        let doc = try #require(URL(string: Self.doc))
        let post = try #require(URL(string: Self.post))
        #expect(FeishuLinks.route(doc, lark: .all, manager: "m-2") == .doc(url: Self.doc, title: nil, manager: "m-2"))
        #expect(FeishuLinks.target(post, lark: .all, manager: "m-2") == .route(.momentsPost(id: "7000000000000000001", manager: "m-2")))
        #expect(FeishuLinks.route(doc, lark: .all)?.manager == nil, "the default's: nothing named")
        #expect(FeishuLinks.route(doc, lark: .all, manager: "m-2") != FeishuLinks.route(doc, lark: .all), "two managers' copies are two pages")
        #expect(FeishuRoute.profile(LarkPersonRef(id: "ou_1", name: "Ada")).manager == nil)

        #expect(LinkTarget(url: doc).id == Self.doc, "the default's sheet is the one it was")
        #expect(LinkTarget(url: doc, manager: "m-2").id != LinkTarget(url: doc).id)
    }

    // MARK: pictures and files

    @Test func aPictureNamesItsManager() {
        let path = "/api/v1/lark/raw/images/img_1"
        #expect(LarkMedia.url(path, manager: "m-2") == path + "?manager=m-2")
        #expect(LarkMedia.url(path + "?size=96", manager: "m-2") == path + "?size=96&manager=m-2")
        #expect(LarkMedia.url(path + "#top", manager: "m-2") == path + "?manager=m-2#top")
        #expect(LarkMedia.url(path + "?manager=m-3", manager: "m-2") == path + "?manager=m-3", "one that names a manager already")
        #expect(LarkMedia.url(path, manager: "m 2&x") == path + "?manager=m%202%26x")
        #expect(LarkMedia.url("https://cdn.example.com/a.png", manager: "m-2") == "https://cdn.example.com/a.png", "a CDN's: as it is")
        #expect(LarkMedia.url("//cdn.example.com/a.png", manager: "m-2") == "//cdn.example.com/a.png")
        #expect(LarkMedia.url(path, manager: nil) == path && LarkMedia.url(path, manager: "") == path, "the default's: as before")
    }

    @Test func whatIsKeptIsKeptPerManager() {
        let path = "/api/v1/lark/raw/images/img_1"
        let work = LarkMedia.cacheKey(LarkMedia.url(path, manager: "m-2"), size: 96)
        #expect(work != LarkMedia.cacheKey(path, size: 96), "another manager's picture at the same path is its own entry")
        #expect(work != LarkMedia.cacheKey(LarkMedia.url(path, manager: "m-3"), size: 96))
        #expect(LarkMedia.cacheKey(LarkMedia.url(path, manager: nil), size: 96) == LarkMedia.cacheKey(path, size: 96), "the default's keeps its entry")
    }

    // MARK: requests

    @Test func theReaderNamesItsManagerOnEveryRead() async {
        await FeishuDocsReaderModel().load(url: Self.doc, title: nil, api: client("work.docs.invalid").manager("m-2"))
        let reads = DocsStub.requests(to: "work.docs.invalid")
        #expect(Set(reads.compactMap { $0.url?.path }) == ["/api/v1/lark/raw/documents/header", "/api/v1/lark/raw/documents/reader/info",
                                                        "/api/v1/lark/raw/documents/ai-summary", "/api/v1/lark/raw/documents/read"])
        #expect(reads.map { $0.value(forHTTPHeaderField: Self.header) } == Array(repeating: "m-2", count: reads.count))

        let comments = FeishuDocsCommentsModel(url: Self.doc, api: client("work-comments.docs.invalid").manager("m-2"))
        await comments.load()
        #expect(DocsStub.requests(to: "work-comments.docs.invalid").map { $0.value(forHTTPHeaderField: Self.header) } == ["m-2"])

        // The app's client while the Persona page shows another manager: the default's document still names none.
        await FeishuDocsReaderModel().load(url: Self.doc, title: nil, api: client("home.docs.invalid").showing("m-2").manager(nil))
        let home = DocsStub.requests(to: "home.docs.invalid")
        #expect(home.count == 4 && home.allSatisfy { $0.value(forHTTPHeaderField: Self.header) == nil }, "the default's names none")
    }

    @Test func commentsAndPostsNameTheManager() async {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/lark/documents/comments", json: "{}")
        let write = FeishuDocsCommentWrite(url: Self.doc, content: "Looks good", mentions: [], quote: nil, blockID: nil, clientID: "c-1")
        try? await hub.client.manager("m-2").docComment(write)
        _ = try? await hub.client.manager("m-2").lark(nil).larkMomentsPost("7000000000000000001")
        _ = try? await hub.client.manager(nil).lark(nil).larkMomentsPost("7000000000000000002")
        #expect(hub.sent("POST", "/api/v1/lark/documents/comments").map { $0.headers[Self.header] } == ["m-2"])
        #expect(hub.sent("GET", "/api/v1/lark/moments/posts/7000000000000000001").map { $0.headers[Self.header] } == ["m-2"])
        #expect(hub.sent("GET", "/api/v1/lark/moments/posts/7000000000000000002").map { $0.headers[Self.header] } == [nil])
    }
}
