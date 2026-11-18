import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Demo Mode's made-up hub (DemoHub, Resources/Demo.json): every page's request decodes into the app's models, so a
/// model that changes shape breaks here instead of in front of App Review. DemoModeTests walks the screens.
@MainActor struct DemoHubTests {
    private let api = APIClient(config: .demo)

    init() {
        DemoHub.install()
        DemoHub.shared.reset()
    }

    @Test func everyPageDecodes() async throws {
        let machines = try await api.machines()
        #expect(machines.count == 5)
        #expect(machines.contains { !$0.online }, "an offline machine")
        let sessions = try await api.sessions()
        #expect(sessions.count > 15)
        for state in [AgentState.needsYou, .working, .unread, .idle] {
            #expect(sessions.contains { $0.state == state && $0.assistant != true }, "no \(state) agent")
        }
        #expect(sessions.contains { $0.agent == "codex" })
        #expect(try await api.interactions().count == 2)
        for s in sessions {
            let page = try await api.items(s.key, beforeSeq: nil)
            #expect(!page.items.isEmpty, "\(s.title ?? s.sessionID) opens empty")
        }
        _ = try await api.sessionInfo(sessions[0].key)
        #expect(try await api.launcherList(machine: "macbook").launchers.count == 2)
        _ = try await api.dirs(machine: "macbook", path: "")

        #expect(try await api.metricsOverview().count == 5)
        let day = try await api.machineMetrics("server", range: .day)
        #expect(day.points.count > 300)
        #expect(day.latest != nil)
        let studio = try await api.machineMetrics("studio", range: .day)
        #expect(studio.points.contains { $0.gap == true }, "the Mac Studio's two hours away")
        // Token usage (#4973): the cards carry it, the machine's page has every range and filter.
        #expect(try await api.metricsOverview().contains { $0.usage?.today.total ?? 0 > 0 })
        for r in UsageRange.allCases {
            let u = try await api.machineUsage("server", range: r)
            #expect(u.total.total > 0 && u.models.count == 3 && u.launchers.count == 2, "\(r)")
        }
        let codex = try await api.machineUsage("server", range: .day, launcher: "codex")
        #expect(codex.models.map(\.name) == ["gpt-5.5"] && codex.total.cacheWrite == 0)

        let a = try await api.assistant()
        #expect(a.conversations.count >= 4)
        let tasks = try await api.scheduledTasks()
        #expect(Set(tasks.map(\.repeats)) == [.once, .daily, .interval], "every kind of task has a demo")
        #expect(tasks.contains { $0.permissionMode != nil }, "and one runs with its own permission mode")
        var patrol = try #require(tasks.first { $0.repeats == .interval })
        patrol.enabled = false
        patrol.effort = nil
        let saved = try await api.saveTask(patrol)
        #expect(!saved.enabled && saved.effort == nil && saved.nextRun == nil)
        #expect(!(try await api.scheduleRuns()).isEmpty)
        #expect(!(try await api.assistantMemory()).isEmpty)
        #expect(!(try await api.todos()).isEmpty, "task samples are bundled, not just scheduled routines")
        let projects = try await api.kbList(.projects)
        let project = try #require(projects.items.first)
        let found = try await api.kbFind(project.name)
        #expect(found.contains { $0.entry.id == project.id })
        #expect(!(try await api.kbSearch(project.name)).isEmpty)
        _ = try await api.notifyPrefs(device: "x")
        _ = try await api.pins()
        _ = try await api.deviceTokens()

        let inbox = try await api.inbox()
        #expect(inbox.count >= 6)
        for item in inbox {
            if let c = item.larkConversation { #expect(!(try await api.larkMessages(c.id)).messages.isEmpty) }
            if let m = item.mailSummary {
                _ = try await api.mailMessage(account: m.accountID, uid: m.uid, mailbox: m.mailbox ?? "INBOX")
            }
        }
        let conversations = try await api.larkConversations()
        #expect(conversations.conversations.count >= 2)
        for c in conversations.conversations {
            #expect(!(try await api.larkMessages(c.id)).messages.isEmpty)
        }
        _ = try await api.mailAccounts()
    }

    /// Times are moved to now: the demo never looks days old.
    @Test func timesAreCurrent() async throws {
        let sessions = try await api.sessions()
        let newest = sessions.compactMap(\.updatedAt).max()
        #expect(newest.map { abs($0.timeIntervalSinceNow) < 120 } == true)
    }

    /// What would reach a computer is refused with "Nothing is actually sent in demo mode" (shown as a note, not an error).
    @Test func sideEffectsAreRefused() async throws {
        let k = try #require(try await api.sessions().first { $0.assistant != true }).key
        do {
            try await api.interrupt(k)
            Issue.record("interrupt went through")
        } catch let e as APIError {
            #expect(e.code == DemoText.code)
            #expect(e.localizedDescription == DemoText.notSent)
        }
        await #expect(throws: APIError.self) {
            _ = try await api.start(.init(machineID: "macbook", cwd: "/Users/me/code/weather", launcher: "claude"))
        }
    }

    /// A message to an agent is echoed, and the demo answers it.
    @Test func aMessageGetsTheDemosAnswer() async throws {
        let k = try #require(try await api.sessions().first { $0.assistant != true }).key
        let before = try await api.items(k, beforeSeq: nil).items.count
        try await api.send(k, text: "看一下进度")
        let mine = try await api.items(k, beforeSeq: nil).items
        #expect(mine.count == before + 1)
        #expect(mine.last?.text == "看一下进度")
        try await Task.sleep(for: .seconds(2))
        #expect(try await api.items(k, beforeSeq: nil).items.last?.text == DemoHub.reply)
    }
}
