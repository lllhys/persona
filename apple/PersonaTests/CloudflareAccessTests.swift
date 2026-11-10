import Foundation
import Testing
@testable import Persona

/// Answers requests to access.invalid as a hub behind Cloudflare Access would (docs/operations/network.md "Behind
/// Cloudflare Access"), and keeps the headers each one carried.
private final class AccessProtocol: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var seen: [String: [String: String]] = [:]
    static func headers(_ path: String) -> [String: String]? { lock.withLock { seen[path] } }

    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "access.invalid" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let path = request.url!.path
        Self.lock.withLock { Self.seen[path] = request.allHTTPHeaderFields ?? [:] }
        var status = 200
        var headers = ["Content-Type": "application/json"]
        var body = Data(#"{"ok":true}"#.utf8)
        switch path {
        case "/api/v1/login": // no token: Access sends the client to its login
            status = 302
            headers = ["Location": "https://example-team.cloudflareaccess.com/cdn-cgi/access/login/access.invalid?redirect_url=%2Fapi"]
            body = Data()
        case "/api/v1/forbidden": // a refused token: Access's own page
            status = 403
            headers = ["Content-Type": "text/html; charset=utf-8"]
            body = Data(#"<html><body>Forbidden <a href="/cdn-cgi/access/logout">Sign out</a></body></html>"#.utf8)
        case "/api/v1/hub-forbidden": // the hub's own refusal stays the hub's
            status = 403
            body = Data(#"{"error":"forbidden"}"#.utf8)
        default: break
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor
struct CloudflareAccessTests {
    private let token = HubAccess(clientID: "example-id.access", clientSecret: "example-secret-value")!
    private let hub = URL(string: "https://access.invalid")!

    private func client(access: HubAccess?) -> APIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [AccessProtocol.self]
        return APIClient(config: ServerConfig(url: hub, token: "fixture-token", access: access), session: URLSession(configuration: configuration))
    }

    // MARK: Where the headers go

    @Test func restRequestsToTheHubCarryBothHeaders() async throws {
        _ = try await client(access: token).request("GET", "/api/v1/ok")
        let sent = try #require(AccessProtocol.headers("/api/v1/ok"))
        #expect(sent[CloudflareAccess.idHeader] == "example-id.access")
        #expect(sent[CloudflareAccess.secretHeader] == "example-secret-value")
        #expect(sent["Authorization"] == "Bearer fixture-token")
    }

    @Test func aHubWithoutATokenSendsNone() async throws {
        _ = try await client(access: nil).request("GET", "/api/v1/plain")
        let sent = try #require(AccessProtocol.headers("/api/v1/plain"))
        #expect(sent[CloudflareAccess.idHeader] == nil && sent[CloudflareAccess.secretHeader] == nil)
    }

    @Test func webSocketRequestsToTheHubCarryBothHeaders() {
        let config = ServerConfig(url: hub, token: "fixture-token", access: token)
        let forward = PortForwarder.connectRequest(.init(machine: "example-node", port: 3000), config: config)
        #expect(forward.url?.scheme == "wss")
        #expect(forward.value(forHTTPHeaderField: CloudflareAccess.idHeader) == "example-id.access")
        #expect(forward.value(forHTTPHeaderField: CloudflareAccess.secretHeader) == "example-secret-value")
        let voice = VoiceEndpoint.request(APIClient(config: config), extensionID: "voice")
        #expect(voice.url?.scheme == "wss")
        #expect(voice.value(forHTTPHeaderField: CloudflareAccess.secretHeader) == "example-secret-value")
    }

    @Test func otherOriginsNeverGetTheToken() {
        // Another hub, a mesh peer's address, a tunnel's loopback, a third party, another port or scheme of the host.
        for other in ["https://other.invalid/api/v1/health", "http://127.0.0.1:52011/api/v1/ws", "https://example.com/x",
                      "https://access.invalid:8443/api", "http://access.invalid/api"] {
            #expect(CloudflareAccess.headers(token, for: URL(string: other), hub: hub).isEmpty, "\(other)")
        }
        #expect(CloudflareAccess.headers(token, for: URL(string: "wss://access.invalid/api/v1/ws"), hub: hub).count == 2)
        #expect(CloudflareAccess.headers(token, for: URL(string: "https://ACCESS.invalid:443/api"), hub: hub).count == 2)
        // A config for another hub never carries this one's token.
        var other = URLRequest(url: URL(string: "https://other.invalid/api/v1/health")!)
        ServerConfig(url: URL(string: "https://other.invalid")!, token: "t").authorize(&other)
        #expect(other.value(forHTTPHeaderField: CloudflareAccess.secretHeader) == nil)
    }

    @Test func meshPeersAndTheWatchGetOnlyWhatIsTheirs() {
        var a = PairedHub(config: ServerConfig(url: hub, token: "phone", access: token))
        a.mesh = [MeshHub(hubID: "B", online: true, endpoints: ["https://peer.invalid"])]
        a.watch = ServerConfig(url: hub, token: "watch")
        var b = PairedHub(config: ServerConfig(url: URL(string: "https://second.invalid")!, token: "two", access: token))
        b.group = "g2"
        b.watch = ServerConfig(url: URL(string: "https://old-address.invalid")!, token: "watch2")
        let registry = HubRegistry(hubs: [a, b])
        let peer = HubRoute.candidates(registry, group: "").first { $0.hub == nil }
        #expect(peer?.config.url.host() == "peer.invalid" && peer?.config.access == nil)
        let watch = registry.watchCredentials().configs
        #expect(watch[""]?.access == token) // the same hub's address: the token as the phone has it now
        #expect(watch["g2"]?.token == "watch2" && watch["g2"]?.access == nil) // its watch token is for another address
    }

    // MARK: Access turning the app away

    @Test func aRedirectToTheAccessLoginIsItsOwnError() async {
        await #expect(throws: CloudflareAccessError(tokenSent: false)) { try await client(access: nil).request("GET", "/api/v1/login") }
        await #expect(throws: CloudflareAccessError(tokenSent: true)) { try await client(access: token).request("GET", "/api/v1/login") }
        await #expect(throws: CloudflareAccessError(tokenSent: true)) { try await client(access: token).request("GET", "/api/v1/forbidden") }
        // The hub's own JSON refusal is still the hub's.
        await #expect(throws: APIError.self) { try await client(access: token).request("GET", "/api/v1/hub-forbidden") }
        #expect(CloudflareAccessError(tokenSent: false).errorDescription != CloudflareAccessError(tokenSent: true).errorDescription)
        // A WebSocket handshake (no body): Access's HTML refusal, not the hub's JSON 401 (a revoked device).
        let handshake = { (status: Int, headers: [String: String]) in
            CloudflareAccess.blocked(HTTPURLResponse(url: hub, statusCode: status, httpVersion: nil, headerFields: headers))
        }
        #expect(handshake(403, ["Content-Type": "text/html", "CF-RAY": "0123456789abcdef-LHR"]))
        #expect(handshake(401, ["Content-Type": "text/html; charset=UTF-8", "Server": "cloudflare"]))
        #expect(!handshake(401, ["Content-Type": "application/json", "CF-RAY": "0123456789abcdef-LHR"]))
        // A hub without Access behind a reverse proxy or basic-auth gateway that answers in HTML: as before.
        #expect(!handshake(403, ["Content-Type": "text/html"]))
        #expect(!handshake(401, ["Content-Type": "text/html", "Server": "nginx", "WWW-Authenticate": "Basic realm=hub"]))
    }

    @Test func redirectsToTheLoginOrOffTheOriginAreNotFollowed() async {
        var original = URLRequest(url: URL(string: "https://access.invalid/api/v1/sessions")!)
        ServerConfig(url: hub, token: "t", access: token).authorize(&original)
        let login = URLRequest(url: URL(string: "https://example-team.cloudflareaccess.com/cdn-cgi/access/login/x")!)
        let elsewhere = URLRequest(url: URL(string: "https://example.com/api/v1/sessions")!)
        let same = URLRequest(url: URL(string: "https://access.invalid/api/v1/sessions/")!)
        #expect(CloudflareAccess.follow(login, from: original) == nil)
        #expect(CloudflareAccess.follow(elsewhere, from: original) == nil)
        #expect(CloudflareAccess.follow(same, from: original) == same)
        // Without the token a redirect off the origin is as before; Access's login never.
        let plain = URLRequest(url: URL(string: "https://access.invalid/api")!)
        #expect(CloudflareAccess.follow(elsewhere, from: plain) == elsewhere)
        #expect(CloudflareAccess.follow(login, from: plain) == nil)
        // The delegate every hub request goes with says the same.
        let task = URLSession.shared.dataTask(with: original)
        let redirect = HTTPURLResponse(url: original.url!, statusCode: 302, httpVersion: nil, headerFields: ["Location": login.url!.absoluteString])!
        let next: URLRequest? = await withCheckedContinuation { done in
            HubRedirects.shared.urlSession(.shared, task: task, willPerformHTTPRedirection: redirect, newRequest: login) { done.resume(returning: $0) }
        }
        #expect(next == nil)
    }

    // MARK: The secret stays out of logs

    @Test func theWatchKeepsItsConfigsInTheKeychain() throws {
        let defaults = try #require(UserDefaults(suiteName: "CloudflareAccessTests.watch"))
        defer {
            defaults.removePersistentDomain(forName: "CloudflareAccessTests.watch")
            Keychain.delete(account: WatchConfigs.keychainAccount)
        }
        Keychain.delete(account: WatchConfigs.keychainAccount)
        let all = WatchConfigs(configs: ["": ServerConfig(url: hub, token: "watch", access: token)], names: ["": "Home"])
        // An older build's plain copy moves into the keychain and leaves defaults.
        defaults.set(try JSONEncoder().encode(all), forKey: "configs")
        #expect(WatchConfigs.stored(defaults) == all)
        #expect(defaults.data(forKey: "configs") == nil)
        #expect(WatchConfigs.stored(defaults)?.configs[""]?.access == token)
        // From before several hubs: the single config.
        Keychain.delete(account: WatchConfigs.keychainAccount)
        defaults.set(try JSONEncoder().encode(ServerConfig(url: hub, token: "watch", access: token)), forKey: "config")
        #expect(WatchConfigs.stored(defaults)?.configs[""]?.access == token)
        #expect(defaults.data(forKey: "config") == nil)
        // New ones never touch defaults.
        all.store(defaults)
        #expect(defaults.dictionaryRepresentation().values.allSatisfy { !"\($0)".contains("example-secret-value") })
        #expect(defaults.data(forKey: "configs") == nil && defaults.data(forKey: "config") == nil)
    }

    @Test func descriptionsRedactTheSecret() {
        let config = ServerConfig(url: hub, token: "fixture-token", access: token)
        var request = URLRequest(url: hub.appending(path: "api/v1/ok"))
        config.authorize(&request)
        var dumped = ""
        dump(config, to: &dumped)
        for text in [String(describing: token), String(reflecting: token), String(describing: config), String(reflecting: config),
                     dumped, String(describing: request), String(reflecting: request), "\(PairedHub(config: config))"] {
            #expect(!text.contains("example-secret-value"), "\(text)")
        }
        #expect(String(describing: token).contains("example-id.access"))
    }

    @Test func emptyFieldsMeanNoToken() {
        #expect(HubAccess(clientID: " ", clientSecret: "x") == nil)
        #expect(HubAccess(clientID: "id", clientSecret: "") == nil)
        #expect(HubAccess(clientID: " id ", clientSecret: " s\n")?.clientID == "id")
    }

    @Test func storedWithThePairingAndReadByOlderBuilds() throws {
        let config = ServerConfig(url: hub, token: "t", access: token)
        let data = try JSONEncoder().encode(config)
        #expect(try JSONDecoder().decode(ServerConfig.self, from: data) == config)
        // A pairing written before the field: no token.
        let old = try JSONDecoder().decode(ServerConfig.self, from: Data(#"{"url":"https://access.invalid","token":"t"}"#.utf8))
        #expect(old.access == nil)
    }
}
