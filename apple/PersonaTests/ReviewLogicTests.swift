import Foundation
@testable import Persona
import Testing
import PersonaWire

/// The review page's pure parts (`ReviewLogic`, #151): rows from hunks, a picked range as the web's `diffPick` makes it,
/// the drafts' order, the message they make and the `Diff:` they name.
struct ReviewLogicTests {
    private func file(_ path: String, _ lines: [String], old: Int = 10, new: Int = 10) -> GitReview.File {
        GitReview.File(path: path, hunks: [GitReview.Hunk(header: "@@ -\(old),3 +\(new),3 @@", oldStart: old, newStart: new, lines: lines)])
    }

    @Test func rowsNumberTheLinesAfterEachHeader() {
        let rows = ReviewLogic.rows(file("a.swift", [" keep", "-gone", "+came", " tail"]))
        #expect(rows.count == 5)
        guard case .hunk = rows[0].kind else { Issue.record("no header first"); return }
        #expect(rows.dropFirst().map(\.index) == [0, 1, 2, 3])
    }

    @Test func pickTakesTheRowsBetweenTwoTapsInEitherOrder() throws {
        let f = file("a.swift", [" keep", "-gone", "+came", " tail"])
        let all = try #require(ReviewLogic.pick(f, ReviewPick(path: "a.swift", first: 3, last: 0)))
        #expect(all.from == 10 && all.to == 12 && !all.old)
        #expect(all.code == [" keep", "-gone", "+came", " tail"])
        let removed = try #require(ReviewLogic.pick(f, ReviewPick(path: "a.swift", first: 1, last: 1)))
        #expect(removed.old && removed.from == 11 && removed.to == 11)
    }

    @Test func draftsGoInFileThenLineOrderAsOneMessage() {
        let files = [file("b.swift", [" x"]), file("a.swift", [" y"])]
        let late = ReviewDraft(path: "a.swift", from: 9, to: 9, code: [" y"], note: "second", diff: .uncommitted)
        let early = ReviewDraft(path: "b.swift", from: 3, to: 4, code: ["+x", " z"], note: "first", diff: .uncommitted)
        let other = ReviewDraft(path: "a.swift", from: 2, to: 2, code: [" w"], note: "middle", diff: .uncommitted)
        #expect(ReviewLogic.ordered([late, other, early], files: files).map(\.note) == ["first", "middle", "second"])
        let message = ReviewLogic.message([late, early], files: files, summary: "  Looks good.\n")
        let expected = [
            "> Code comment 1", "> File: b.swift:3-4", "> Diff: uncommitted", "> ```diff", "> +x", ">  z", "> ```", "> ", "> first", "",
            "> Code comment 2", "> File: a.swift:9", "> Diff: uncommitted", "> ```diff", ">  y", "> ```", "> ", "> second", "",
            "Looks good.",
        ].joined(separator: "\n")
        #expect(message == expected)
    }

    @Test func aDraftCarriesAtMostSixtyRowsAndCountsTheRest() {
        let rows = (1...70).map { "+line \($0)" }
        let c = ReviewDraft(path: "big.swift", from: 1, to: 70, code: rows, note: "n", diff: .uncommitted).comment
        #expect(c.quoted.components(separatedBy: "\n").count == MessageComments.maxCodeLines)
        #expect(c.code?.omitted == 10)
    }

    @Test func theDiffNamesTheScopeAndItsBase() {
        let r = GitReview(base: .init(hash: "4f2a9c1aabbccdd", short: "4f2a9c1"), files: [])
        #expect(ReviewLogic.diff(.uncommitted, r) == .uncommitted)
        #expect(ReviewLogic.diff(.branch, r) == .branchSince("4f2a9c1"))
        #expect(ReviewLogic.diff(.commit("4f2a9c1aabbccdd"), r) == .since("4f2a9c1"))
        let noShort = GitReview(base: .init(hash: "0123456789abcdef"), files: [])
        #expect(ReviewLogic.diff(.branch, noShort) == .branchSince("01234567"))
    }

    @Test func anOldSideDraftFromAnotherDiffIsOutdated() {
        let d = ReviewDraft(path: "a.swift", old: true, from: 3, to: 3, code: ["-x"], note: "n", diff: .branchSince("aaaa"))
        #expect(ReviewLogic.outdated(d, now: .uncommitted))
        #expect(!ReviewLogic.outdated(d, now: .branchSince("aaaa")))
        var newSide = d
        newSide.old = false
        #expect(!ReviewLogic.outdated(newSide, now: .uncommitted))
    }

    // MARK: The node's real answers (#8733)

    private struct Fixture: Decodable {
        struct Case: Decodable {
            var name: String
            var answer: JSONValue
        }
        var version: Int
        var cases: [Case]
    }

    private static let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    /// spec/fixtures/review: what git.review sends, written by the node's own test. A hunk with no function context has
    /// no `header` and a repository with no commit a base with no `hash`; one such hunk used to fail the whole review.
    private func answers() throws -> [String: GitReview] {
        let data = try Data(contentsOf: Self.repo.appending(path: "spec/fixtures/review/answers.json"))
        let fixture = try JSONDecoder().decode(Fixture.self, from: data)
        #expect(fixture.version == 1)
        var out: [String: GitReview] = [:]
        for c in fixture.cases {
            out[c.name] = try JSON.decoder.decode(GitReview.self, from: JSON.encoder.encode(c.answer))
        }
        return out
    }

    @Test func readsHunksWithoutAHeader() throws {
        let r = try #require(try answers()["branch_hunks_without_header"])
        #expect(r.files.map(\.path) == ["draft.txt", "notes.txt", "plan.md"])
        let headers = r.files.flatMap { $0.hunks ?? [] }.map(\.header)
        #expect(headers == ["", "", "item 18", ""])
        #expect(r.base?.ref == "origin/main" && r.base?.hash.isEmpty == false)
        let notes = try #require(r.files.first { $0.path == "notes.txt" })
        // only the named hunk has a header row: an empty header draws no band
        let rows = ReviewLogic.rows(notes)
        #expect(rows.count == 1 + 5 + 8)
        #expect(rows.compactMap { r -> String? in if case .hunk(let h) = r.kind { return h } else { return nil } } == ["item 18"])
        #expect(rows.filter { $0.index >= 0 }.map(\.index) == Array(0..<13))
    }

    @Test func readsARepositoryWithNoCommitYet() throws {
        let r = try #require(try answers()["no_commit_yet"])
        #expect(r.base?.empty == true && r.base?.hash == "" && r.head == nil)
        #expect(r.files.first?.hunks?.first?.lines == ["+hello"])
        // a big file there is asked for with the review's own scope: there is no commit to pin
        #expect(ReviewLogic.bigFileScope(r, current: .uncommitted) == .uncommitted)
        let pinned = GitReview(base: .init(hash: "4f2a9c1aabbccdd"), files: [])
        #expect(ReviewLogic.bigFileScope(pinned, current: .branch) == .commit("4f2a9c1aabbccdd"))
    }

    @Test func anUnreadableAnswerIsSaidInWordsNotSwiftsOwn() throws {
        let decoding: any Error
        do {
            _ = try JSON.decoder.decode(GitReview.self, from: Data(#"{"root":"/r"}"#.utf8))
            Issue.record("a review without files decoded")
            return
        } catch { decoding = error }
        let text = ReviewLogic.failureText(decoding)
        #expect(text == String(localized: "This version of the app can't read this computer's answer. Update the app and try again."))
        #expect(text != decoding.localizedDescription)
        let refused = APIError(status: 502, message: "git log failed", fields: ["code": "node_unreachable", "delivered": "none"])
        #expect(ReviewLogic.failureText(refused) == refused.localizedDescription)
    }
}
