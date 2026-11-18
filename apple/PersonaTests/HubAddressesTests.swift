import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Answers health probes at addresses.invalid as hub "H" would, and at old.invalid as a server without a hub_id.
private final class HealthProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { ["addresses.invalid", "old.invalid"].contains(request.url?.host) }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let resp = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data((request.url?.host == "old.invalid" ? #"{"ok":true}"# : #"{"ok":true,"hub_id":"H"}"#).utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

/// A paired hub's added addresses (#4670, docs/operations/network.md "Client-added candidates").
struct HubAddressesTests {
    private let vpnToken = HubAccess(clientID: "vpn-id.access", clientSecret: "vpn-secret")!
    private let publicToken = HubAccess(clientID: "public-id.access", clientSecret: "public-secret")!

    /// Paired on the VPN address A; B (behind Access) and C added.
    private func hub() -> PairedHub {
        var h = PairedHub(config: ServerConfig(url: URL(string: "http://a.invalid:8080")!, token: "device", access: vpnToken), hubID: "H")
        h.addresses = [HubAddress(url: URL(string: "https://b.invalid")!, access: publicToken),
                       HubAddress(url: URL(string: "https://c.invalid")!)]
        return h
    }

    @Test func oldPairingsDecodeWithNoAddresses() throws {
        let old = PairedHub(config: ServerConfig(url: URL(string: "http://a.invalid:8080")!, token: "device"))
        let data = try JSONEncoder().encode(HubRegistry(hubs: [old]))
        #expect(!String(decoding: data, as: UTF8.self).contains("addresses")) // as a build before them wrote it
        let back = try JSONDecoder().decode(HubRegistry.self, from: data)
        #expect(back.hubs[0].addresses == nil && back.hubs[0].configs == [old.config])
        #expect(HubRoute.candidates(back, group: "").count == 1) // one place: no probing, as before
        let saved = try JSONDecoder().decode(HubRegistry.self, from: JSONEncoder().encode(HubRegistry(hubs: [hub()])))
        #expect(saved.hubs[0].addresses == hub().addresses)
    }

    @Test func candidatesFollowThePairedAddressInOrder() {
        let h = hub()
        let c = HubRoute.candidates(HubRegistry(hubs: [h]), group: "")
        #expect(c.map(\.url.host) == ["a.invalid", "b.invalid", "c.invalid"])
        #expect(c.map(\.address) == [0, 1, 2])
        #expect(c.map(\.hubID) == [nil, "H", "H"]) // added addresses must answer as the paired hub
        #expect(c.allSatisfy { $0.config.token == "device" && $0.hub == h.id })
        #expect(c.map(\.config.access) == [vpnToken, publicToken, nil])
    }

    @Test func accessHeadersGoOnlyToTheirOwnAddress() {
        var h = hub()
        h.mesh = [MeshHub(hubID: "P", online: true, endpoints: ["https://peer.invalid"])]
        let candidates = HubRoute.candidates(HubRegistry(hubs: [h]), group: "")
        let urls = candidates.map(\.url) + [URL(string: "https://peer.invalid/api/v1/ws")!, URL(string: "https://example.com/x")!]
        for c in candidates {
            for url in urls {
                var req = URLRequest(url: url.appending(path: "api/v1/health"))
                c.config.authorize(&req)
                let secret = req.value(forHTTPHeaderField: CloudflareAccess.secretHeader)
                let own = CloudflareAccess.sameOrigin(url, c.url)
                #expect(secret == (own ? c.config.access?.clientSecret : nil), "\(c.url) → \(url)")
            }
        }
    }

    @Test func pickPrefersTheEarlierAddress() async {
        let c = HubRoute.candidates(HubRegistry(hubs: [hub()]), group: "")
        // A answers a moment after B, within the grace: A.
        let slowA = await HubRoute.pick(c) { cand in
            if cand.address == 0 { try? await Task.sleep(for: .milliseconds(100)) }
            return true
        }
        #expect(slowA?.address == 0)
        // A unreachable (another VPN took the slot): B, without waiting out the grace.
        let start = ContinuousClock.now
        let down = await HubRoute.pick(c, grace: .seconds(30)) { $0.address != 0 }
        #expect(down?.url.host == "b.invalid")
        #expect(ContinuousClock.now - start < .seconds(5))
        // A and B hang: C once the grace is over.
        let hanging = await HubRoute.pick(c, grace: .milliseconds(50)) { cand in
            if cand.address < 2 { try? await Task.sleep(for: .seconds(10)) }
            return true
        }
        #expect(hanging?.url.host == "c.invalid")
        // B answers before C: B, the earlier of the two, even when C was quicker.
        let both = await HubRoute.pick(c, grace: .seconds(30)) { cand in
            if cand.address == 1 { try? await Task.sleep(for: .milliseconds(100)) }
            return cand.address != 0
        }
        #expect(both?.url.host == "b.invalid")
        // Nobody answers: the paired address, for the socket's own retries.
        #expect(await HubRoute.pick(c) { _ in false }?.address == 0)
    }

    @Test func anAddedAddressMustAnswerAsThePairedHub() async {
        URLProtocol.registerClass(HealthProtocol.self)
        let config = ServerConfig(url: URL(string: "https://addresses.invalid")!, token: "device")
        #expect(await HubRoute.healthProbe(.init(config: config, hub: "h", address: 1, hubID: "H")))
        #expect(await !HubRoute.healthProbe(.init(config: config, hub: "h", address: 1, hubID: "OTHER")))
        #expect(await HubRoute.healthProbe(.init(config: config))) // the paired address: not checked, as before
        // No hub_id in the answer: it can't be told apart from another server.
        let old = ServerConfig(url: URL(string: "https://old.invalid")!, token: "device")
        #expect(await !HubRoute.healthProbe(.init(config: old, hub: "h", address: 1, hubID: "H")))
        #expect(await HubRoute.healthProbe(.init(config: old, hub: "h", address: 1))) // hub_id not known yet
    }
}

/// What the Addresses page makes of the text typed: `https://` in front of an address without a scheme, and a reason
/// when there is nothing to add (docs/operations/network.md "Client-added candidates").
struct HubAddressInputTests {
    private let existing = [URL(string: "http://a.invalid:8080")!, URL(string: "https://b.invalid")!]

    private func input(_ text: String) -> HubAddressInput { HubAddressInput(text, existing: existing) }
    private func ready(_ url: String) -> HubAddressInput { .ready(URL(string: url)!) }

    @Test func nothingTypedSaysNothing() {
        #expect(input("") == .empty)
        #expect(input("   \n") == .empty)
    }

    @Test func aBareHostGetsHTTPS() {
        #expect(input("hub.example.com") == ready("https://hub.example.com"))
        #expect(input("  hub.example.com/ ") == ready("https://hub.example.com"))
        #expect(input("hub.example.com:8443") == ready("https://hub.example.com:8443"))
        #expect(input("hub.example.com/persona") == ready("https://hub.example.com/persona"))
    }

    /// No guessing http for a local-looking address: whoever wants http writes it.
    @Test func aBareIPOrLocalhostGetsHTTPSToo() {
        #expect(input("192.0.2.10:18660") == ready("https://192.0.2.10:18660"))
        #expect(input("localhost:18660") == ready("https://localhost:18660"))
        #expect(input("[fd00::1]:18662") == ready("https://[fd00::1]:18662"))
    }

    @Test func aTypedSchemeIsKept() {
        #expect(input("http://192.0.2.10:18660") == ready("http://192.0.2.10:18660"))
        #expect(input("https://hub.example.com") == ready("https://hub.example.com"))
        #expect(input("HTTPS://hub.example.com") == ready("HTTPS://hub.example.com"))
    }

    @Test func otherSchemesAreRefused() {
        #expect(input("ftp://hub.example.com") == .unsupportedScheme)
        #expect(input("wss://hub.example.com") == .unsupportedScheme)
    }

    @Test func whatIsNoAddressSaysSo() {
        #expect(input("https://") == .invalid)
        #expect(input("hub example.com") == .invalid)
        #expect(input("hub.example.com:99999") == .invalid)
        #expect(input("hub.example.com:port") == .invalid)
        #expect(input("://hub.example.com") == .invalid)
    }

    /// The same address written another way is still the one the hub has.
    @Test func anAddressTheHubHasIsADuplicate() {
        #expect(input("b.invalid") == .duplicate(URL(string: "https://b.invalid")!))
        #expect(input("https://B.invalid:443/") == .duplicate(URL(string: "https://B.invalid:443")!))
        #expect(input("http://a.invalid:8080") == .duplicate(URL(string: "http://a.invalid:8080")!))
        // Another scheme or port is another address.
        #expect(input("a.invalid:8080") == ready("https://a.invalid:8080"))
        #expect(input("http://b.invalid") == ready("http://b.invalid"))
    }
}
