import Foundation
import Observation

/// A parent's side sessions as its chat shows them (#7944, `AppModel.sides(of:)`): one row per side, in the sheet's order
/// (`SideFold.ordered`), and the rollup its chip counts and dots by. Equatable on what the chip and the sheet draw, so a
/// push that changes nothing of it draws neither again.
struct SideSessions: Equatable {
    struct Row: Equatable, Identifiable {
        var key: SessionKey
        /// The session's title (`Session.displayTitle`), never an id, a path or an engine.
        var title: String
        var state: AgentState
        /// Its state's word (`Session.stateLabel`), the way every list says it.
        var label: String
        var waitsInBackground: Bool
        var id: SessionKey { key }
    }

    var rows: [Row] = []
    var rollup = SideFold.Rollup()

    init() {}

    init(_ fold: SideFold.Result, parent: SessionKey) {
        rows = (fold.sides[parent] ?? []).map {
            Row(key: $0.key, title: $0.displayTitle, state: $0.state, label: $0.stateLabel, waitsInBackground: $0.waitsInBackground)
        }
        rollup = fold.rollups[parent] ?? SideFold.Rollup()
    }

    /// How many: the sides held, else (a paged row whose sides are not held) the hub's count.
    var count: Int { max(rows.count, rollup.sides) }
    /// One of them, or a row of one's chat, needs the user: the chip's dot.
    var needsYou: Bool { rollup.needsYou > 0 }
    var isEmpty: Bool { count == 0 }
}

/// `AppModel.sides(of:)` as it hands it out.
@Observable final class SidesCell {
    var value: SideSessions
    init(_ value: SideSessions) { self.value = value }
}

/// A side session to show over its parent's chat (`AppModel.sideLanding`): a notification, a Needs You card or a link
/// named the side, and its parent's chat opened with it shown.
struct SideLanding: Equatable {
    var parent: SessionKey
    var side: SessionKey
}
