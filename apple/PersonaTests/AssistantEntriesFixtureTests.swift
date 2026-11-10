import XCTest
@testable import Persona

/// Replays the shared fixture spec/fixtures/entries/cases.json (#3387) through `AssistantEntries.split`: the iPhone, iPad
/// and Mac action bar splits the same input into Needs You / In Progress / shelved exactly as the web
/// (`assistant-entries.test.ts`) and Android (`AssistantEntriesFixtureTest`) do.
@MainActor
final class AssistantEntriesFixtureTests: XCTestCase {
    private static let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    private struct Fixture: Decodable {
        var version: Int
        var cases: [Case]
    }

    private struct Case: Decodable {
        var name: String
        var input: Input
        var expected: Expected
    }

    private struct Input: Decodable {
        var conversation: String
        var main: String?
        var now: String
        var todos: [TodoItem]
        var sessions: [Session]
        var interactions: [Interaction]
        var plans: [String: TaskPlan]
        var feed: [FeedItem]
    }

    private struct Expected: Decodable {
        var needs_you: [String]
        var running: [String]
        var shelved: [String]
    }

    func testEveryCaseSplitsAsTheWebAndAndroid() throws {
        let data = try Data(contentsOf: Self.repo.appending(path: "spec/fixtures/entries/cases.json"))
        let fixture = try JSON.decoder.decode(Fixture.self, from: data)
        XCTAssertEqual(fixture.version, 1)
        XCTAssertGreaterThanOrEqual(fixture.cases.count, 13)
        for c in fixture.cases {
            let i = c.input
            let now = try XCTUnwrap(JSON.parseDate(i.now), c.name)
            let got = AssistantEntries.split(conversation: i.conversation, main: i.main, todos: i.todos, sessions: i.sessions,
                                             interactions: i.interactions, plans: i.plans, feed: i.feed, now: now)
            XCTAssertEqual(got.needsYou.map(\.id), c.expected.needs_you, "\(c.name): needs you")
            XCTAssertEqual(got.running.map(\.id), c.expected.running, "\(c.name): running")
            XCTAssertEqual(got.shelved.map(\.id), c.expected.shelved, "\(c.name): shelved")
            // each thing once, and each count is its rows
            let all = (got.needsYou + got.running + got.shelved).map(\.id)
            XCTAssertEqual(Set(all).count, all.count, "\(c.name): a thing in two places: \(all)")
            let content = ActionTrayContent(got)
            XCTAssertEqual(content.count(.needsYou), c.expected.needs_you.count, c.name)
            XCTAssertEqual(content.count(.running), c.expected.running.count, c.name)
        }
    }
}
