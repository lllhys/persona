import Foundation
import Testing
@testable import Persona
import PersonaWire

@MainActor
struct APISendTests {
    private let key = SessionKey(machineID: "test", sessionID: "codex")
    private let path = "/api/v1/sessions/test/codex/messages"

    /// The watch's ids (RetryIDs): a retry after a failure keeps the failed try's id, a message that went through
    /// leaves the next one a new id, another message has its own.
    @Test func retriesKeepTheFailedTrysID() {
        var ids = RetryIDs<String>()
        let first = ids.id(for: "继续")
        #expect(UUID(uuidString: first) != nil)
        #expect(ids.id(for: "继续") == first) // the send failed: tapped again
        #expect(ids.id(for: "停一下") != first)
        ids.sent("继续")
        #expect(ids.id(for: "继续") != first)
    }

    /// A failed request and its retry keep the caller's id so the hub can deduplicate delivery.
    @Test func preservesMessageIDOnRetry() async throws {
        let hub = FakeHub()
        let id = "watch-message-1185"
        hub.on("POST", path) { _ in throw URLError(.timedOut) }
        await #expect(throws: URLError.self) {
            try await hub.client.send(key, text: "继续", clientMsgID: id)
        }
        hub.on("POST", path, json: "{}")
        try await hub.client.send(key, text: "继续", clientMsgID: id)

        let requests = hub.sent("POST", path)
        #expect(requests.count == 2)
        for request in requests {
            let body = try #require(request.body)
            let fields = try JSONDecoder().decode([String: String].self, from: body)
            #expect(fields["client_msg_id"] == id)
        }
    }
}
