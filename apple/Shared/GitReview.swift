import Foundation

/// A review of a session's changes (#151, docs/web/review.md): `GET /api/v1/machines/{mid}/git/review`, what the files on
/// disk are compared with (HEAD, where the branch left the default branch, or a commit) and every changed file's hunks.
/// The hub has it when its hello's features list `git_review`.
nonisolated struct GitReview: Decodable, Sendable, Equatable {
    struct Base: Decodable, Sendable, Equatable {
        var hash: String
        var short: String?
        var subject: String?
        /// The branch it is where this one left (`origin/main`), for a review of the branch.
        var ref: String?
        /// The repository has no commit yet: every file is new.
        var empty: Bool?
    }

    struct Head: Decodable, Sendable, Equatable {
        var hash: String?
        var short: String?
        var branch: String?
    }

    struct Hunk: Decodable, Sendable, Equatable {
        var header: String
        var oldStart: Int
        var newStart: Int
        /// Each with its `" "`, `"+"`, `"-"` or `"\"` in front.
        var lines: [String]

        enum CodingKeys: String, CodingKey {
            case header, lines
            case oldStart = "old_start", newStart = "new_start"
        }
    }

    struct File: Decodable, Sendable, Equatable, Identifiable {
        var path: String
        var oldPath: String?
        /// `added`, `modified`, `deleted`, `renamed`…
        var status: String?
        var untracked: Bool?
        var added: Int?
        var deleted: Int?
        var binary: Bool?
        /// A lockfile or other generated file: folded at first.
        var generated: Bool?
        /// Holds secrets (a key): the node does not send it.
        var refused: Bool?
        /// Too big to come with the rest: asked for on its own (`Load Diff`).
        var big: Bool?
        var revision: String?
        var hunks: [Hunk]?

        var id: String { path }

        enum CodingKeys: String, CodingKey {
            case path, status, untracked, added, deleted, binary, generated, refused, big, revision, hunks
            case oldPath = "old_path"
        }
    }

    var root: String?
    var scope: String?
    var base: Base?
    var head: Head?
    var files: [File]
    /// Only the first files came.
    var truncated: Bool?
}

/// What a review compares the files on disk with.
nonisolated enum GitReviewScope: Codable, Hashable, Sendable {
    /// HEAD: what isn't committed yet.
    case uncommitted
    /// Where the branch left the default branch.
    case branch
    /// A commit (its hash).
    case commit(String)

    var query: [String: String] {
        switch self {
        case .uncommitted: ["scope": "uncommitted"]
        case .branch: ["scope": "branch"]
        case .commit(let hash): ["scope": "commit", "base": hash]
        }
    }
}

extension APIClient {
    /// The session's changes against `scope`; `path` asks for that file alone (a big one, with the base pinned).
    func gitReview(machine: String, cwd: String, scope: GitReviewScope, path: String? = nil) async throws -> GitReview {
        var query = scope.query.merging(["cwd": cwd]) { a, _ in a }
        if let path { query["path"] = path }
        return try await getDecodedOffMain("/api/v1/machines/\(machine)/git/review", query: query)
    }
}
