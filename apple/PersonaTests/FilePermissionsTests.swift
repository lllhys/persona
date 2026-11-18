import Foundation
import Testing
@testable import Persona

/// Changing permissions (#5831): the confirmation says what changes for whom, and the request carries the bits.
@MainActor
struct FilePermissionsTests {
    @Test func saysWhatChangesForWhom() {
        let words = FileModeChange.words(from: 0o644, to: 0o755)
        #expect(words.contains(String(localized: "Owner")) && words.contains("→"))
        #expect(!FileModeChange.words(from: 0o644, to: 0o744).contains(String(localized: "Everyone else")))
        #expect(FileModeChange.words(from: 0o700, to: 0o700).isEmpty)
    }

    @Test func sendsTheBits() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/machines/m1/files/chmod", json: #"{"path":"~/a.sh","mode":493}"#)
        let mode = try await hub.client.changeMode(machine: "m1", path: "~/a.sh", cwd: "/w", mode: 0o755)
        #expect(mode == 0o755)
        let body = try #require(JSONSerialization.jsonObject(with: hub.sent("POST", "/api/v1/machines/m1/files/chmod").first?.body ?? Data()) as? [String: Any])
        #expect(body["mode"] as? Int == 0o755 && body["path"] as? String == "~/a.sh")
    }
}
