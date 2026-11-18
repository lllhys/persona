import Foundation
import Network
import Testing
@testable import Persona
import PersonaWire

/// A forwarded port follows the group's hub (#1886): each browser connection dials the hub, route and token the group has
/// at that moment, not the ones the forward started with, and a group that goes takes its forwards with it. The forwards
/// listen on the loopback for real; the hubs are `.invalid` addresses, so nothing leaves the machine.
@MainActor
@Suite(.serialized)
struct PortForwarderTests {
    /// What the group's link would answer with: the test changes it as the app would on a switch.
    final class Live {
        var config: ServerConfig?
        init(_ config: ServerConfig?) { self.config = config }
    }

    let hubA = URL(string: "https://forward-a.invalid/persona")!
    let hubB = URL(string: "http://forward-b.invalid:18660")!
    let tunnel = URL(string: "http://127.0.0.1:52186")!

    /// Opens one browser connection to the forward and returns the request it dialed the hub with.
    private func dial(_ forward: PortForward) async throws -> URLRequest {
        let port = try #require(NWEndpoint.Port(rawValue: UInt16(forward.localPort)))
        let conn = NWConnection(host: "127.0.0.1", port: port, using: .tcp)
        defer {
            conn.cancel()
            forward.dialedForTests = nil
        }
        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<URLRequest, Error>) in
            let once = RunOnce()
            forward.dialedForTests = { req in once.run { cont.resume(returning: req) } }
            conn.start(queue: .global())
            Task {
                try? await Task.sleep(for: .seconds(10))
                once.run { cont.resume(throwing: URLError(.timedOut)) }
            }
        }
    }

    @Test func eachConnectionDialsTheGroupsCurrentHubRouteAndToken() async throws {
        defer { HubTransport.route(hubA, via: nil) }
        let forwarder = PortForwarder()
        defer { forwarder.closeAll() }
        let live = Live(ServerConfig(url: hubA, token: "fixture-token-one"))
        let key = PortForwarder.Key(group: "g1", machine: "example-node", port: 41886)
        let port = try await forwarder.localPort(key) { live.config }
        let forward = try #require(forwarder.forward(key))

        // Direct, as the forward was opened.
        var req = try await dial(forward)
        #expect(req.url?.absoluteString == "wss://forward-a.invalid/persona/api/v1/machines/example-node/ports/41886/connect")
        #expect(req.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token-one")

        // The direct address is lost and the app goes through the tunnel: the next connection takes the tunnel.
        HubTransport.route(hubA, via: tunnel)
        req = try await dial(forward)
        #expect(req.url?.absoluteString == "ws://127.0.0.1:52186/persona/api/v1/machines/example-node/ports/41886/connect")

        // The tunnel closes once the hub answers directly again: straight to the hub again, not to the closed tunnel.
        HubTransport.unroute(hubA, from: tunnel)
        req = try await dial(forward)
        #expect(req.url?.host() == "forward-a.invalid")

        // The group's socket moves to another hub, then the device pairs again with a new token.
        live.config = ServerConfig(url: hubB, token: "fixture-token-two")
        req = try await dial(forward)
        #expect(req.url?.absoluteString == "ws://forward-b.invalid:18660/api/v1/machines/example-node/ports/41886/connect")
        #expect(req.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token-two")

        // Tapping the link again keeps the page's port; the forward is the same one and still follows.
        #expect(try await forwarder.localPort(key) { live.config } == port)
        #expect(forwarder.forward(key) === forward)

        // The group is gone: nothing to dial.
        live.config = nil
        #expect(forward.currentRequest() == nil)
    }

    @Test func askingAgainReplacesWhereTheForwardLooksForItsHub() async throws {
        let forwarder = PortForwarder()
        defer { forwarder.closeAll() }
        let key = PortForwarder.Key(group: "g1", machine: "example-node", port: 0)
        _ = try await forwarder.localPort(key) { ServerConfig(url: hubA, token: "fixture-token-one") }
        _ = try await forwarder.localPort(key) { ServerConfig(url: hubB, token: "fixture-token-two") }
        let req = try #require(forwarder.forward(key)?.currentRequest())
        #expect(req.url?.host() == "forward-b.invalid")
        #expect(req.value(forHTTPHeaderField: "Authorization") == "Bearer fixture-token-two")
    }

    @Test func machinesWithOneIdInTwoGroupsGetAForwardEach() async throws {
        let forwarder = PortForwarder()
        defer { forwarder.closeAll() }
        let a = PortForwarder.Key(group: "g1", machine: "example-node", port: 0)
        let b = PortForwarder.Key(group: "g2", machine: "example-node", port: 0)
        _ = try await forwarder.localPort(a) { ServerConfig(url: hubA, token: "fixture-token-one") }
        _ = try await forwarder.localPort(b) { ServerConfig(url: hubB, token: "fixture-token-two") }
        let fa = try #require(forwarder.forward(a))
        let fb = try #require(forwarder.forward(b))
        #expect(fa !== fb && fa.localPort != fb.localPort)
        #expect(fa.currentRequest()?.url?.host() == "forward-a.invalid")
        #expect(fb.currentRequest()?.url?.host() == "forward-b.invalid")

        // Disconnecting one group stops its forwards only.
        forwarder.close(groups: ["g1"])
        #expect(forwarder.forward(a) == nil && !fa.isReady)
        #expect(forwarder.forward(b) === fb && fb.isReady)

        // Signing out of everything stops the rest.
        forwarder.closeAll()
        #expect(forwarder.forward(b) == nil && !fb.isReady)
    }
}
