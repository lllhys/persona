import Testing
@testable import Persona
import PersonaWire

/// The per-message memos are keyed by chat and id (#372): item ids repeat across sessions — a runtime's are
/// "<seq>#<block>", and two branches of one DSH session share theirs up to where they part.
@MainActor
struct RowMemoTests {
    private static func text(_ id: String, _ text: String) -> Item {
        Item(id: id, seq: 384, ts: nil, role: "user", kind: .text, text: text, toolUseID: nil, toolName: nil, toolInput: nil,
             isError: nil, image: nil)
    }

    @Test func twoBranchesWithTheSameItemIDEachShowTheirOwnMessage() {
        // Unique per run: the memos are shared by every test in the process.
        let a = SessionKey(machineID: "m-\(UInt64.random(in: 1...UInt64.max))", sessionID: "session-a")
        let b = SessionKey(machineID: a.machineID, sessionID: "session-b")
        let mine = Self.text("384#0", "continue: the same opening. Then branch A's own question")
        let theirs = Self.text("384#0", "continue: the same opening. Then branch B's other question")

        // Branch B is shown first, then branch A: A's row must not get B's text.
        let shownB = RowMemo.user(RowMemo.key(RowMemo.scope(b), theirs.id)) { _ in UserRowText(theirs.text ?? "") }
        let shownA = RowMemo.user(RowMemo.key(RowMemo.scope(a), mine.id)) { _ in UserRowText(mine.text ?? "") }
        #expect(shownB.shown == theirs.text)
        #expect(shownA.shown == mine.text)

        // Within one chat the memo still answers without parsing again.
        var parsed = 0
        let again = RowMemo.user(RowMemo.key(RowMemo.scope(a), mine.id)) { _ in parsed += 1; return UserRowText("not used") }
        #expect(again.shown == mine.text)
        #expect(parsed == 0)
    }

    @Test func prewarmFillsTheChatsOwnEntries() {
        let a = SessionKey(machineID: "m-\(UInt64.random(in: 1...UInt64.max))", sessionID: "session-a")
        let b = SessionKey(machineID: a.machineID, sessionID: "session-b")
        RowMemo.prewarm([Self.text("17#0", "in A")], scope: RowMemo.scope(a))
        RowMemo.prewarm([Self.text("17#0", "in B")], scope: RowMemo.scope(b))
        #expect(RowMemo.user(RowMemo.key(RowMemo.scope(a), "17#0")) { _ in UserRowText("?") }.shown == "in A")
        #expect(RowMemo.user(RowMemo.key(RowMemo.scope(b), "17#0")) { _ in UserRowText("?") }.shown == "in B")
    }

    @Test func aToolCallsInputIsTheChatsOwn() {
        let a = SessionKey(machineID: "m-\(UInt64.random(in: 1...UInt64.max))", sessionID: "session-a")
        let b = SessionKey(machineID: a.machineID, sessionID: "session-b")
        var call = Self.text("390#1", "")
        call.kind = .toolUse
        call.toolName = "Bash"
        call.toolInput = .object(["command": .string("echo A")])
        var other = call
        other.toolInput = .object(["command": .string("echo B")])
        #expect(ToolTextMemo.input(other, scope: RowMemo.scope(b)).contains("echo B"))
        #expect(ToolTextMemo.input(call, scope: RowMemo.scope(a)).contains("echo A"))
    }

    @Test func keysOutsideAChatAreTheIDAsBefore() {
        #expect(MemoScope.key("", "384#0") == "384#0")
        #expect(MemoScope.key(MemoScope.of(machine: "m", session: "s1"), "384#0") != MemoScope.key(MemoScope.of(machine: "m", session: "s2"), "384#0"))
        #expect(MemoScope.key(MemoScope.of(machine: "m1", session: "s"), "1#0") != MemoScope.key(MemoScope.of(machine: "m2", session: "s"), "1#0"))
    }
}
