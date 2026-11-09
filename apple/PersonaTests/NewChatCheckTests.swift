import Foundation
@testable import Persona
import Testing

/// New Chat's check (#2189): a miss is written only when no new-conversation page appeared within the grace, with labels
/// only; a page that appeared in time writes nothing.
@MainActor struct NewChatCheckTests {
    private func check() -> NewChatCheck { NewChatCheck(grace: .milliseconds(100), more: .milliseconds(400)) }

    @Test func aPageThatComesInTimeWritesNothing() async {
        let c = check()
        var lines: [String] = []
        c.watch(from: "main", current: { "none" }, miss: { lines.append($0) })
        c.homeAppeared()
        try? await Task.sleep(for: .milliseconds(800))
        #expect(lines.isEmpty)
    }

    @Test func noPageWritesOneLineOfLabels() async {
        let c = check()
        var lines: [String] = []
        c.watch(from: "main", current: { "none" }, miss: { lines.append($0) })
        try? await Task.sleep(for: .milliseconds(800))
        #expect(lines == ["new_chat_check from=main current_early=none current_late=none drawn_late=chat"])
    }

    @Test func aPageThatComesLateSaysSo() async {
        let c = check()
        var lines: [String] = []
        c.watch(from: "main", current: { "main" }, miss: { lines.append($0) })
        try? await Task.sleep(for: .milliseconds(250))
        c.homeAppeared()
        try? await Task.sleep(for: .milliseconds(550))
        #expect(lines == ["new_chat_check from=main current_early=main current_late=main drawn_late=home"])
    }

    @Test func aSecondTapStartsOver() async {
        let c = check()
        var lines: [String] = []
        c.watch(from: "main", current: { "none" }, miss: { lines.append("first " + $0) })
        c.watch(from: "other", current: { "none" }, miss: { lines.append("second " + $0) })
        try? await Task.sleep(for: .milliseconds(800))
        #expect(lines.count == 1)
        #expect(lines.first?.hasPrefix("second new_chat_check from=other") == true)
    }

    @Test func namesAreMainOtherOrNone() {
        let main = SessionKey(machineID: "server", sessionID: "a")
        let other = SessionKey(machineID: "server", sessionID: "b")
        #expect(NewChatCheck.name(nil, main: main) == "none")
        #expect(NewChatCheck.name(main, main: main) == "main")
        #expect(NewChatCheck.name(other, main: main) == "other")
    }
}
