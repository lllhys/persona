import Foundation
@testable import Persona
import Testing

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
}
