import Foundation
import Testing
@testable import Persona
import PersonaWire

@MainActor struct ChatUserNavigationTests {
    private func index(_ count: Int, history: String = "history-a") -> ChatUserInputIndex {
        ChatUserInputIndex(total: count, inputs: (1...max(1, count)).prefix(count).map {
            .init(id: "\($0 * 2)#0", seq: $0 * 2)
        }, history: history)
    }

    private func user(_ number: Int, part: Int = 0, image: Bool = false) -> ChatLine {
        let item = Item(id: "\(number * 2)#\(part)", seq: number * 2, ts: nil, role: "user", kind: image ? .image : .text,
                        text: image ? nil : "Input \(number)", toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
        return image ? .row(.image(item)) : .row(.user(item))
    }

    @Test func longHistoryWithTwoLoadedInputsStartsWithFifty() {
        let marks = ChatUserNavigation.marks(index: index(120), lines: [user(119), user(120)], history: "history-a", revealingOlder: false)
        #expect(marks.count == 50)
        #expect(marks.first?.ordinal == 71)
        #expect(marks.last?.ordinal == 120)
        #expect(marks.first?.seq == 142)
        #expect(marks.first?.text == "") // An unopened body is not downloaded for the navigator.
        #expect(marks.suffix(2).map(\.text) == ["Input 119", "Input 120"])
    }

    @Test(arguments: [0, 1, 3, 49, 50]) func shorterHistoriesShowEveryInput(count: Int) {
        let marks = ChatUserNavigation.marks(index: index(count), lines: [], history: "history-a", revealingOlder: false)
        #expect(marks.count == count)
        #expect(marks.map(\.ordinal) == (1...max(1, count)).prefix(count).map(Optional.some))
    }

    @Test func olderHistoryAddsMarksWithoutRenumbering() {
        let earlier = (51...120).map { user($0) }
        let initial = ChatUserNavigation.marks(index: index(120), lines: earlier, history: "history-a", revealingOlder: false)
        let expanded = ChatUserNavigation.marks(index: index(120), lines: earlier, history: "history-a", revealingOlder: true)
        #expect(initial.count == 50) // Even a warm cache does not change the initial window.
        #expect(expanded.count == 70)
        #expect(expanded.first?.ordinal == 51)
        #expect(expanded.suffix(50).map(\.id) == initial.map(\.id))
        #expect(expanded.suffix(50).map(\.ordinal) == initial.map(\.ordinal))
    }

    @Test func rewrittenHistoryRejectsOldIdentitiesAndOrdinals() {
        let marks = ChatUserNavigation.marks(index: index(120), lines: [user(1)], history: "history-b", revealingOlder: false)
        #expect(marks.count == 1)
        #expect(marks.first?.ordinal == nil)
    }

    @Test func invalidAndUnavailableIndexesKeepTheLoadedFallback() {
        var broken = index(3)
        broken.total = 120
        #expect(!broken.isValid)
        for value in [broken, nil] {
            let marks = ChatUserNavigation.marks(index: value, lines: [user(2), user(3)], history: "history-a", revealingOlder: false)
            #expect(marks.count == 2)
            #expect(marks.allSatisfy { $0.ordinal == nil })
        }
        broken = index(3)
        broken.inputs[1] = broken.inputs[0]
        #expect(!broken.isValid)
        broken = index(3)
        broken.inputs.reverse()
        #expect(!broken.isValid)
    }

    @Test func photosAndTextSiblingsHaveOneHistoryOrdinal() {
        let lines = [user(1), user(1, part: 1, image: true), user(2, image: true)]
        let marks = ChatUserNavigation.marks(index: index(2), lines: lines, history: "history-a", revealingOlder: false)
        #expect(marks.count == 2)
        #expect(marks.map(\.id) == ["2#0", "4#0"])
        #expect(marks.map(\.ordinal) == [1, 2])
        #expect(!marks[1].text.isEmpty)
        #expect(EdgeScrubState.marks(lines).count == 2)
    }

    @Test func imageGroupIDsResolveToTheActualChatLine() {
        let item = Item(id: "4#1", seq: 4, ts: nil, role: "user", kind: .image, text: nil,
                        toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
        let line = ChatLine.userImages(id: "img-4#1", items: [item])
        let marks = ChatUserNavigation.marks(index: index(2), lines: [line], history: "history-a", revealingOlder: false)
        #expect(marks[1].id == "img-4#1")
        #expect(!marks[1].text.isEmpty)
        #expect(ChatUserNavigation.lineID(for: marks[1], lines: [line]) == line.id)
        #expect(ChatUserNavigation.lineID(for: EdgeMark(id: "4#0", text: "", seq: 4, ordinal: 2), lines: [line]) == line.id)
        #expect(ChatUserNavigation.lineID(for: marks[0], lines: [line]) == nil)
    }

    @Test func metadataRequestNeverLoadsBodies() async throws {
        let hub = FakeHub()
        let path = "/api/v1/sessions/test/session/items"
        hub.on("GET", path, json: #"{"total":2,"inputs":[{"id":"2#0","seq":2},{"id":"4#0","seq":4}],"history":"history-a"}"#)
        let result = try await hub.client.userInputIndex(SessionKey(machineID: "test", sessionID: "session"))
        #expect(result.isValid)
        let request = try #require(hub.sent("GET", path).first)
        #expect(request.query["user_inputs"] == "1")
        #expect(request.query["lazy_images"] == "1")
        #expect(request.query["turns"] == nil)
        #expect(hub.sent("GET", path).count == 1)
    }

    @Test func malformedMetadataIsRetryable() async throws {
        let hub = FakeHub()
        let path = "/api/v1/sessions/test/session/items"
        let key = SessionKey(machineID: "test", sessionID: "session")
        hub.on("GET", path, json: #"{"total":120,"inputs":[],"history":"history-a"}"#)
        await #expect(throws: APIError.self) { try await hub.client.userInputIndex(key) }
        hub.on("GET", path, json: #"{"total":0,"inputs":[],"history":"history-a"}"#)
        #expect(try await hub.client.userInputIndex(key).total == 0)
    }
}
