import Foundation
import Testing
@testable import Persona

/// While the Feishu screens follow `messaging`, the app's hello says so (`lark_via_messaging`) and the hub stops sending
/// it the legacy `lark` event it would drop; a change is said again in a new hello (#9591).
@MainActor
struct LarkViaMessagingTests {
    private final class Hub: LocalSocket {
        var hellos: [[String]] = []
        func open(deliver: @escaping (Data) -> Void) { deliver(Data(#"{"type":"hello","features":["messaging"]}"#.utf8)) }
        func receive(_ message: [String: Any]) {
            if message["type"] as? String == "client_hello" { hellos.append(message["features"] as? [String] ?? []) }
        }
        func close() {}
    }

    @Test func theHelloSaysWhileTheAppFollowsMessaging() async throws {
        let hub = Hub()
        let live = LiveConnection()
        live.makeLocalForTests = { hub }
        live.connect(.demo)
        for _ in 0..<200 where hub.hellos.isEmpty { try await Task.sleep(for: .milliseconds(10)) }
        let feature = LiveConnection.larkViaMessagingFeature
        #expect(hub.hellos.count == 1)
        #expect(hub.hellos.last?.contains(feature) == false, "no source yet: the legacy event is still read")

        live.followsMessaging = true
        #expect(hub.hellos.count == 2)
        #expect(hub.hellos.last?.contains(feature) == true)
        live.followsMessaging = true
        #expect(hub.hellos.count == 2, "the same again says nothing")
        live.followsMessaging = false
        #expect(hub.hellos.count == 3)
        #expect(hub.hellos.last?.contains(feature) == false)
        live.disconnect()
    }
}
