import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Replays the section bar's shared cases (#9053, docs/assistant/inbox.md "Section bar"): spec/fixtures/inbox/bar.json,
/// bar_landing.json and bar_search.json, which the web app and Android replay too. The items go through the projection
/// and filters the Inbox page draws from (`InboxProjection`, `InboxBar.waiting`, `InboxBar.Rows`).
@MainActor
struct InboxBarFixtureTests {
    private static let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    private static func data(_ name: String) throws -> Data {
        try Data(contentsOf: repo.appending(path: "spec/fixtures/inbox/" + name))
    }

    // MARK: bar.json

    private struct BarFixture: Decodable {
        var operation: String
        var cases: [Case]

        struct Case: Decodable {
            var name: String
            var input: Input
            var expected: Expected
        }

        struct Input: Decodable {
            var items: [InboxItem]
            var interactions: [Interaction]
            var sessions: [FixtureSession]
            var mutes: Pins
            var settings: Settings
            var done_days_total: Int?
        }

        struct FixtureSession: Decodable {
            var machine_id: String
            var session_id: String
            var space_name: String?
        }

        struct Settings: Decodable {
            var source: String
            var kind: String
            var query: String
            var chosen: String
        }

        struct Expected: Decodable {
            var bar: [Entry]
            var shown_sections: [String]
            var empty: String?
        }

        struct Entry: Decodable, Equatable, CustomStringConvertible {
            var id: String
            var count: Int?
            var tone: String?
            var description: String { "\(id):\(count.map(String.init) ?? "-"):\(tone ?? "-")" }
        }
    }

    private static func name(_ empty: InboxPage.EmptyKind?) -> String? {
        switch empty {
        case nil: nil
        case .caughtUp: "caught_up"
        case .section: "section"
        case .filtered: "filtered"
        case .noMatch: "no_match"
        }
    }

    @Test func everyBarCaseMatches() throws {
        let fixture = try JSON.decoder.decode(BarFixture.self, from: Self.data("bar.json"))
        #expect(fixture.operation == "inbox_bar")
        #expect(!fixture.cases.isEmpty)
        for c in fixture.cases {
            let s = c.input.settings
            let chosen = try #require(InboxChoice(rawValue: s.chosen), "\(c.name): chosen \(s.chosen)")
            let filter = InboxProjection.Filter(source: s.source, kind: s.kind, query: s.query)
            let projection = InboxProjection(items: c.input.items, filter: filter)
            // The page's mute rule (`AppModel.isMuted`): the agent itself, or its space unless it was turned back on.
            let spaces = Dictionary(c.input.sessions.map { ("\($0.machine_id)/\($0.session_id)", $0) }, uniquingKeysWith: { a, _ in a })
            let muted = { (key: SessionKey) -> Bool in
                let pin = AppModel.pinKey(session: key)
                return c.input.mutes.mutes(session: pin, space: spaces[pin]?.space_name.map { "\(key.machineID)/\($0)" })
            }
            let agents = InboxBar.waiting(c.input.interactions, source: s.source, kind: s.kind, query: s.query, muted: muted)
            // The hub's day counts by day; only their sum matters to the bar.
            let days = c.input.done_days_total.map { ["2026-01-01": $0] }
            let rows = InboxBar.Rows(projection, agents: agents.count, filter: filter, history: [], doneDays: days)

            let bar = InboxBar.entries(rows, chosen: chosen).map {
                BarFixture.Entry(id: $0.id, count: $0.count, tone: $0.look.map { $0 == .needs ? "needs" : "neutral" })
            }
            #expect(bar == c.expected.bar, "\(c.name): bar \(bar)")
            #expect(InboxBar.shown(rows, chosen: chosen) == c.expected.shown_sections, "\(c.name): shown")
            let empty = InboxBar.empty(rows, chosen: chosen, source: s.source, kind: s.kind, query: s.query)
            #expect(Self.name(empty) == c.expected.empty, "\(c.name): empty \(String(describing: empty))")
        }
    }

    // MARK: bar_landing.json

    private struct LandingFixture: Decodable {
        var operation: String
        var cases: [Case]

        struct Case: Decodable {
            var name: String
            var input: Input
            var expected: Expected
        }

        struct Input: Decodable { var item: InboxItem }

        struct Expected: Decodable {
            var choice: String
            var open_collapsed: Bool
        }
    }

    @Test func everyLandingCaseMatches() throws {
        let fixture = try JSON.decoder.decode(LandingFixture.self, from: Self.data("bar_landing.json"))
        #expect(fixture.operation == "inbox_bar_landing")
        #expect(!fixture.cases.isEmpty)
        for c in fixture.cases {
            let landing = InboxBar.landing(c.input.item)
            #expect(landing.choice.rawValue == c.expected.choice, "\(c.name): choice \(landing.choice)")
            #expect(landing.openCollapsed == c.expected.open_collapsed, "\(c.name): open collapsed")
        }
    }

    // MARK: bar_search.json

    private struct SearchFixture: Decodable {
        var operation: String
        var cases: [Case]

        struct Case: Decodable {
            var name: String
            var input: Input
            var expected: Expected
        }

        struct State: Decodable {
            var chosen: String
            var saved: String?
            var query: String?
        }

        struct Step: Decodable {
            var query: String?
            var pick: String?
            var enter: String?
        }

        struct Input: Decodable {
            var start: State
            var steps: [Step]
        }

        struct Expected: Decodable { var after: [State] }
    }

    @Test func everySearchCaseMatches() throws {
        let fixture = try JSON.decoder.decode(SearchFixture.self, from: Self.data("bar_search.json"))
        #expect(fixture.operation == "inbox_bar_search")
        #expect(!fixture.cases.isEmpty)
        for c in fixture.cases {
            var state = InboxBarState()
            state.chosen = try #require(InboxChoice(rawValue: c.input.start.chosen), "\(c.name): start")
            state.saved = c.input.start.saved.flatMap(InboxChoice.init(rawValue:))
            state.query = c.input.start.query ?? ""
            try #require(c.input.steps.count == c.expected.after.count, "\(c.name): steps and expectations")
            for (i, (step, after)) in zip(c.input.steps, c.expected.after).enumerated() {
                if let text = step.query {
                    state.search(text)
                } else if let pick = step.pick {
                    state.pick(try #require(InboxChoice(rawValue: pick), "\(c.name) step \(i): pick \(pick)"))
                } else if let text = step.enter {
                    state.enter(text)
                } else {
                    Issue.record("\(c.name) step \(i): no query, pick or enter")
                }
                #expect(state.chosen.rawValue == after.chosen, "\(c.name) step \(i): chosen \(state.chosen)")
                #expect(state.saved?.rawValue == after.saved, "\(c.name) step \(i): saved \(String(describing: state.saved))")
            }
        }
    }
}
