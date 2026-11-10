import Foundation
import Observation

/// A comment on lines of a review, kept on the device until the review is sent (#151, docs/web/review.md): the file,
/// the lines (the old file's when only removed lines were picked), the diff rows with their marks, and the note.
nonisolated struct ReviewDraft: Codable, Hashable, Sendable, Identifiable {
    var id = UUID()
    var path: String
    var old = false
    var from: Int
    var to: Int
    /// The rows from the first picked line to the last, each with its `+`, `-` or space.
    var code: [String]
    var note: String
    /// The diff it was written on (`Diff:`): a draft on the old side of another diff no longer matches its numbers.
    var diff: MessageComment.Diff

    /// The comment the message carries: at most `MessageComments.maxCodeLines` rows, the rest counted.
    var comment: MessageComment {
        let shown = Array(code.prefix(MessageComments.maxCodeLines))
        return MessageComment(quoted: shown.joined(separator: "\n"), note: note,
                              code: .init(file: path, from: from, to: to, old: old, diff: diff, lang: "diff",
                                          omitted: code.count - shown.count))
    }
}

/// One row of a file's diff as the review draws it: a hunk's header or a line, with its index among the file's lines.
nonisolated struct ReviewRow: Hashable, Sendable {
    enum Kind: Hashable, Sendable { case hunk(String), line(CodeDiffLine) }
    var kind: Kind
    /// For a line: its place among the file's lines (headers left out), what a range is counted in.
    var index: Int
}

/// The lines a review comment is about: two taps on one file's line numbers, in either order.
nonisolated struct ReviewPick: Hashable, Sendable {
    var path: String
    var first: Int
    var last: Int

    var range: ClosedRange<Int> { min(first, last)...max(first, last) }
}

nonisolated enum ReviewLogic {
    /// A file's hunks as rows: each hunk's header, then its lines numbered from where it starts.
    static func rows(_ file: GitReview.File) -> [ReviewRow] {
        var out: [ReviewRow] = []
        var i = 0
        for h in file.hunks ?? [] {
            out.append(ReviewRow(kind: .hunk(h.header), index: -1))
            for l in CodeDiffModel.lines(oldStart: h.oldStart, newStart: h.newStart, raw: h.lines) {
                out.append(ReviewRow(kind: .line(l), index: i))
                i += 1
            }
        }
        return out
    }

    /// The picked lines of a file: the first and last line numbers (the new file's, the old file's when only removed
    /// lines are picked), and the rows with their marks; nil when no code row is in the range. The web's `diffPick`.
    static func pick(_ file: GitReview.File, _ p: ReviewPick) -> (from: Int, to: Int, old: Bool, code: [String])? {
        let lines = rows(file).compactMap { r -> CodeDiffLine? in
            guard case .line(let l) = r.kind, p.range.contains(r.index), l.kind != .note else { return nil }
            return l
        }
        guard !lines.isEmpty else { return nil }
        let old = lines.allSatisfy { $0.kind == .del }
        let nums = lines.compactMap { old ? $0.old : ($0.new ?? $0.old) }.filter { $0 > 0 }
        guard let from = nums.min(), let to = nums.max() else { return nil }
        let code = lines.map { (($0.kind == .add) ? "+" : ($0.kind == .del) ? "-" : " ") + $0.text }
        return (from, to, old, code)
    }

    /// What a comment on this review's lines names as its diff.
    static func diff(_ scope: GitReviewScope, _ review: GitReview) -> MessageComment.Diff {
        let base = review.base.map { ($0.short?.isEmpty == false ? $0.short! : String($0.hash.prefix(8))) } ?? ""
        switch scope {
        case .uncommitted: return .uncommitted
        case .branch: return .branchSince(base)
        case .commit: return .since(base)
        }
    }

    /// The drafts in the order the review shows them: by the file's place in the review, then its path, then its first line.
    static func ordered(_ drafts: [ReviewDraft], files: [GitReview.File]) -> [ReviewDraft] {
        let place = Dictionary(files.enumerated().map { ($1.path, $0) }, uniquingKeysWith: { a, _ in a })
        return drafts.sorted {
            let a = place[$0.path] ?? Int.max, b = place[$1.path] ?? Int.max
            if a != b { return a < b }
            if $0.path != $1.path { return $0.path < $1.path }
            return $0.from < $1.from
        }
    }

    /// The review as one message: every draft as a `Code comment N` block in order, then the summary.
    static func message(_ drafts: [ReviewDraft], files: [GitReview.File], summary: String) -> String {
        MessageComments.message(ordered(drafts, files: files).map(\.comment), text: summary.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// A draft on old-side lines written against another diff: its numbers belong to that diff.
    static func outdated(_ d: ReviewDraft, now: MessageComment.Diff) -> Bool { d.old && d.diff != now }
}

/// The review of one session's changes: what is compared, the answer, the drafts (kept per session in `StateCache`,
/// not UserDefaults) and the lines being picked.
@MainActor @Observable final class ReviewModel {
    enum Phase: Equatable {
        case loading
        case loaded(GitReview)
        /// `code` is the hub's (`not_git`, `node_outdated`, `no_default_branch`, `no_merge_base`, `bad_base`…).
        case failed(code: String?, message: String)
    }

    let key: SessionKey
    let cwd: String
    @ObservationIgnored private let api: () -> APIClient?
    var scope: GitReviewScope {
        didSet { ReviewStore.saveScope(scope, for: key) }
    }
    var phase: Phase = .loading
    /// The files changed on disk since this was read.
    var stale = false
    var drafts: [ReviewDraft] {
        didSet { ReviewStore.saveDrafts(drafts, for: key) }
    }
    var picking: ReviewPick?
    /// For Since a Commit.
    var commits: [GitCommit] = []
    /// Big files asked for on their own, by path.
    var loadedBig: [String: GitReview.File] = [:]

    init(key: SessionKey, cwd: String, scope: GitReviewScope? = nil, api: @escaping () -> APIClient?) {
        self.key = key
        self.cwd = cwd
        self.api = api
        self.scope = scope ?? ReviewStore.scope(for: key) ?? .uncommitted
        self.drafts = ReviewStore.drafts(for: key)
    }

    var review: GitReview? { if case .loaded(let r) = phase { r } else { nil } }

    /// The files, a big one replaced by its own answer once loaded.
    var files: [GitReview.File] { (review?.files ?? []).map { loadedBig[$0.path] ?? $0 } }

    var diff: MessageComment.Diff? { review.map { ReviewLogic.diff(scope, $0) } }

    func load() async {
        guard let api = api() else { return }
        if review == nil { phase = .loading }
        do {
            let r = try await api.gitReview(machine: key.machineID, cwd: cwd, scope: scope)
            loadedBig = [:]
            phase = .loaded(r)
            stale = false
        } catch {
            phase = .failed(code: (error as? APIError)?.code, message: error.localizedDescription)
        }
    }

    func select(_ s: GitReviewScope) async {
        guard s != scope || review == nil else { return }
        scope = s
        phase = .loading
        picking = nil
        await load()
    }

    /// A big file, asked for on its own with the base pinned.
    func loadBig(_ path: String) async {
        guard let api = api(), let base = review?.base?.hash else { return }
        if let r = try? await api.gitReview(machine: key.machineID, cwd: cwd, scope: .commit(base), path: path),
           let f = r.files.first(where: { $0.path == path }) {
            loadedBig[path] = f
        }
    }

    func loadCommits() async {
        guard commits.isEmpty, let api = api() else { return }
        commits = (try? await api.gitLog(machine: key.machineID, cwd: cwd, skip: 0, limit: 30)) ?? []
    }

    /// A tap on a line's number: the first starts a pick; another in the same file moves the range's other end.
    func tap(_ path: String, _ index: Int) {
        if let p = picking, p.path == path {
            picking = ReviewPick(path: path, first: p.first, last: index)
        } else {
            picking = ReviewPick(path: path, first: index, last: index)
        }
    }

    /// The picked lines as a draft with `note`; nil when nothing to comment on is picked.
    func draft(note: String) -> ReviewDraft? {
        guard let p = picking, let diff, let file = files.first(where: { $0.path == p.path }), let at = ReviewLogic.pick(file, p) else { return nil }
        return ReviewDraft(path: p.path, old: at.old, from: at.from, to: at.to, code: at.code, note: note.trimmingCharacters(in: .whitespacesAndNewlines), diff: diff)
    }

    func add(_ d: ReviewDraft) {
        drafts.append(d)
        picking = nil
    }

    func remove(_ d: ReviewDraft) { drafts.removeAll { $0.id == d.id } }

    func message(summary: String) -> String { ReviewLogic.message(drafts, files: files, summary: summary) }
}

/// Where a session's review is kept on the device: its drafts and the scope it was last opened at.
@MainActor enum ReviewStore {
    private static let draftsName = "review-drafts"
    private static let scopesName = "review-scopes"

    nonisolated private struct Saved<T: Codable & Sendable>: Codable, Sendable {
        var key: SessionKey
        var value: T
    }

    static func drafts(for key: SessionKey) -> [ReviewDraft] {
        (StateCache.load(draftsName, as: [Saved<[ReviewDraft]>].self) ?? []).first { $0.key == key }?.value ?? []
    }

    static func saveDrafts(_ drafts: [ReviewDraft], for key: SessionKey) {
        var all = (StateCache.load(draftsName, as: [Saved<[ReviewDraft]>].self) ?? []).filter { $0.key != key }
        if !drafts.isEmpty { all.append(Saved(key: key, value: drafts)) }
        StateCache.save(draftsName, all)
    }

    static func scope(for key: SessionKey) -> GitReviewScope? {
        (StateCache.load(scopesName, as: [Saved<GitReviewScope>].self) ?? []).first { $0.key == key }?.value
    }

    static func saveScope(_ scope: GitReviewScope, for key: SessionKey) {
        var all = (StateCache.load(scopesName, as: [Saved<GitReviewScope>].self) ?? []).filter { $0.key != key }
        all.append(Saved(key: key, value: scope))
        StateCache.save(scopesName, Array(all.suffix(50)))
    }
}
