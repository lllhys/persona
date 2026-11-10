import Foundation
import Testing
@testable import Persona

/// The To-dos page's status sections (docs/assistant/todo-status-sop.md, #3206): the same cases as
/// web/src/lib/todo-triage.test.ts, so the clients cannot drift.
@Suite(.serialized)
@MainActor
struct TodoTriageTests {
    private let now = ISO8601DateFormatter().date(from: "2026-11-10T12:00:00Z")!
    private func ago(_ h: Double) -> Date { now.addingTimeInterval(-h * 3600) }

    private func session(_ id: String, state: AgentState, updatedHoursAgo: Double = 1) -> Session {
        let json = """
        {"machine_id":"m","session_id":"\(id)","title":"\(id)","cwd":"/tmp","status":"\(state == .working ? "working" : "idle")",
         "state":"\(state.rawValue)","updated_at":"\(ISO8601DateFormatter().string(from: ago(updatedHoursAgo)))"}
        """
        do { return try JSON.decoder.decode(Session.self, from: Data(json.utf8)) }
        catch { fatalError("bad fixture: \(error)") }
    }

    private func agent(_ sid: String) -> TodoItem.Agent {
        TodoItem.Agent(machineID: "m", sessionID: sid, linkedAt: ago(200))
    }

    private func todo(_ id: String, title: String? = nil, updated: Double = 1, done: Double? = nil, agents: [String] = [],
                      origin: String? = "conv", build: (inout TodoItem) -> Void = { _ in }) -> TodoItem {
        var t = TodoItem(id: id, title: title ?? id, doneAt: done.map(ago), createdAt: ago(300), updatedAt: ago(updated),
                         origin: origin, agents: agents.map(agent))
        build(&t)
        return t
    }

    /// An accepted task the hub reopened when its agent was sent more work: eight criteria, the review judged five.
    private func acceptedReopened() -> TaskSupervision {
        let when = (1...8).map { "c\($0)" }
        return TaskSupervision(supervisor: "conv", doneWhen: when, state: "accepted", round: 1,
                               reviews: [.init(at: ago(5), verdict: "accepted", checks: when.prefix(5).map { .init(criterion: $0, met: true) })])
    }

    private var sessions: [Session] {
        [
            session("ask", state: .needsYou), session("run", state: .working), session("unseen", state: .unread),
            session("idle", state: .idle, updatedHoursAgo: 2), session("old", state: .idle, updatedHoursAgo: 100),
            session("old2", state: .idle, updatedHoursAgo: 120),
        ]
    }

    private var lookup: TodosLogic.SessionLookup { TodosLogic.makeSessionLookup(sessions) }

    private func at(_ t: TodoItem) -> TodoTriage { TodoTriageLogic.triage(t, lookup: lookup, now: now) }
    private func ask(_ outcome: String?, verdict: String = "v") -> TodoAsk {
        TodoAsk(outcome: outcome, verdict: verdict, recommend: nil, askedAt: ISO8601DateFormatter().string(from: ago(1)))
    }

    @Test func agentAskingIsWaitingWhateverElseHolds() {
        let t = at(todo("t", agents: ["ask", "run"]))
        #expect(t.section == .waiting && t.rule == .agentAsks)
    }

    @Test func ownerDecisionIsWaiting() {
        #expect(at(todo("t", agents: ["idle"]) { $0.supervision = .init(supervisor: "c", state: "needs_user") }).rule == .ownerDecides)
        #expect(at(todo("t") { $0.ask = ask("decide") }).section == .waiting)
    }

    @Test func pleaseVerifyOrDoneConclusionIsJustFinished() {
        let verify = at(todo("t", agents: ["idle"]) { $0.ask = ask(nil, verdict: "done, deployed: verify below") })
        #expect(verify.section == .justDone && verify.rule == .finishedUnseen)
        #expect(at(todo("t", agents: ["idle"]) { $0.ask = ask("done") }).section == .justDone)
    }

    @Test func runningAgentIsWorkingEvenOnAnOldTodo() {
        let t = at(todo("t", updated: 400, agents: ["run"]))
        #expect(t.section == .working && t.rule == .agentRunning)
    }

    @Test func closedIsJustFinishedForADayThenDone() {
        #expect(at(todo("t", done: 3)).section == .justDone)
        #expect(at(todo("t", updated: 30, done: 30)).section == .done)
    }

    @Test func acceptedTaskReopenedByMoreWorkIsFinishedNotFollowUp() {
        let td = todo("t", agents: ["idle"]) {
            $0.reopenedAt = ago(4)
            $0.supervision = acceptedReopened()
        }
        let t = at(td)
        #expect(t.section == .justDone && t.rule == .accepted)
        let status = TodosLogic.todoStatus(td, lookup: lookup)
        #expect(td.statusLabel(status) == "已验收")
        #expect(td.shownState(status) == .done)
        #expect(td.reviewState == .accepted && td.reviewBadge?.tone == .positive)
    }

    @Test func acceptedTaskWhoseAgentRunsAgainIsWorking() {
        #expect(at(todo("t", agents: ["run"]) { $0.supervision = acceptedReopened() }).section == .working)
    }

    @Test func deployedDeliveryIsFinished() {
        #expect(at(todo("t") { $0.delivery = .init(state: "live", at: ago(2)) }).rule == .deployed)
    }

    @Test func finishedTurnNobodySawIsJustFinished() {
        let t = at(todo("t", agents: ["unseen"]))
        #expect(t.section == .justDone && t.rule == .finishedUnseen)
    }

    @Test func openPRIsPRToMergeHoweverOldMergedIsNot() {
        #expect(at(todo("t", updated: 300, agents: ["old"]) { $0.delivery = .init(state: "pr") }).section == .pr)
        #expect(at(todo("t", updated: 300, agents: ["old"]) { $0.prURL = "https://example.com/pull/1" }).section == .pr)
        #expect(at(todo("t", updated: 300, agents: ["old"]) {
            $0.prURL = "https://example.com/pull/1"
            $0.delivery = .init(state: "merged")
        }).section == .shelved)
    }

    @Test func noMoveFor48HoursIsShelvedANewerAgentMoveKeepsIt() {
        let t = at(todo("t", updated: 100, agents: ["old"]))
        #expect(t.section == .shelved && t.rule == .stale)
        #expect(at(todo("t", updated: 100, agents: ["idle"])).section == .other)
    }

    /// #3331: the assistant's Progress entry counts by the same bucket as the To-dos page's "Not done".
    @Test func bucketMatchesTheHeaderCountShelvedAndAcceptedAreNotOpen() {
        let stale = todo("stale", updated: 100, agents: ["old"])
        let accepted = todo("accepted", agents: ["idle"]) { $0.reopenedAt = ago(4); $0.supervision = acceptedReopened() }
        let open = todo("open", agents: ["idle"])
        let done = todo("done", done: 3)
        #expect(TodoTriageLogic.bucket(stale, lookup: lookup, now: now) == .shelved)
        #expect(TodoTriageLogic.bucket(stale, lookup: lookup, now: now, restoredAt: ago(1)) == .open)
        #expect(TodoTriageLogic.bucket(accepted, lookup: lookup, now: now) == .closed)
        #expect(TodoTriageLogic.bucket(open, lookup: lookup, now: now) == .open)
        #expect(TodoTriageLogic.bucket(done, lookup: lookup, now: now) == .closed)
        let all = [stale, accepted, open, done]
        let header = TodoTriageLogic.triageTodos(all, lookup: lookup, now: now, restored: [:])
        // the action bar's two entries count the same open to-dos, the shelved one folded and not counted (#3387)
        let entries = AssistantEntries.split(conversation: "conv", main: "conv", todos: all, sessions: sessions, interactions: [],
                                             plans: [:], feed: [], now: now)
        let tray = ActionTrayContent(entries)
        // (its other rows are the sessions here that belong to no task: Agents opened by hand)
        let tasks = (tray.needsYou + tray.running).map(\.id).filter { $0.hasPrefix("task:") }
        #expect(tasks.count == header.open)
        #expect(tasks == ["task:open"])
        #expect(entries.shelved.map(\.id) == ["task:stale"])
        #expect(!(tray.needsYou + tray.running).contains { $0.id == "task:accepted" || $0.id == "task:done" })
    }

    @Test func ownTodoWithoutAgentsOrDueAheadNeverShelves() {
        #expect(at(todo("t", updated: 100, origin: nil)).section == .other)
        #expect(at(todo("t", updated: 100, agents: ["old"]) { $0.dueDate = "2026-12-01" }).section == .other)
    }

    @Test func restoreCountsAsAMove() {
        let td = todo("t", updated: 100, agents: ["old"])
        #expect(TodoTriageLogic.triage(td, lookup: lookup, now: now, restoredAt: ago(1)).section == .other)
        #expect(TodoTriageLogic.triage(td, lookup: lookup, now: now,
                                       restoredAt: now.addingTimeInterval(-TodoTriageLogic.shelveAfter - 1)).section == .shelved)
    }

    @Test func lastActivityTakesTheLatestOfTodoSupervisionAndAgents() {
        let td = todo("t", updated: 50, agents: ["idle"]) {
            $0.supervision = .init(supervisor: "c", progress: .init(kind: "progress", text: "x", at: ago(1)))
        }
        #expect(TodoTriageLogic.lastActivity(td, lookup: lookup) == ago(1))
    }

    @Test func criteriaAddedAfterAStandingAcceptanceAreUncheckedAndNotCounted() {
        let sup = acceptedReopened()
        #expect(sup.checklist.map(\.mark) == [.met, .met, .met, .met, .met, .unchecked, .unchecked, .unchecked])
        #expect(sup.checkCount == "5/5")
    }

    @Test func beforeAnyAcceptanceCriteriaStayOpenAndCount() {
        var sup = acceptedReopened()
        sup.state = "rework"
        sup.reviews = [.init(at: ago(5), verdict: "rework", checks: [.init(criterion: "c1", met: true)])]
        #expect(sup.checklist.filter { $0.mark == .open }.count == 7)
        #expect(sup.checkCount == "1/8")
    }

    @Test func mergesSameIssuePRTitleOrPlanLedByTheMostUrgent() {
        let list = [
            todo("fix", title: "Fix #412 lost pushes", agents: ["run"]),
            todo("test", title: "#412 lost pushes: regression test", updated: 100, agents: ["old"]),
            todo("pr-a", title: "A", updated: 120, agents: ["old2"]) { $0.prURL = "https://example.com/pull/9" },
            todo("pr-b", title: "B", updated: 121, agents: ["old2"]) { $0.prURL = "https://example.com/pull/9/" },
            todo("lone", title: "Something else"),
        ]
        let r = TodoTriageLogic.triageTodos(list, lookup: lookup, now: now)
        #expect(r.groups(.working).map { [$0.lead.id] + $0.related.map(\.id) } == [["fix", "test"]])
        #expect(r.groups(.pr).map { $0.members.map(\.id) } == [["pr-a", "pr-b"]])
        #expect(r.groups(.other).map(\.lead.id) == ["lone"])
    }

    @Test func neverMergesAClosedTodoIntoAnOpenRow() {
        let r = TodoTriageLogic.triageTodos([todo("a", title: "Same title"), todo("b", title: "Same title", done: 1)], lookup: lookup, now: now)
        #expect(r.groups(.other).count == 1)
        #expect(r.groups(.justDone).count == 1)
    }

    @Test func failedStartWaitingOnTheOwnerCountsAsOpen() {
        let r = TodoTriageLogic.triageTodos([todo("f", done: 1) { $0.startError = "no launcher" }], lookup: lookup, now: now)
        #expect(r.groups(.waiting).map(\.lead.id) == ["f"])
        #expect(r.open == 1)
    }

    @Test func quietTasksListedOnThePageLeftOutOtherwise() {
        let list = [todo("q") { $0.quiet = true }]
        #expect(TodoTriageLogic.triageTodos(list, lookup: lookup, now: now).open == 1)
        #expect(TodoTriageLogic.triageTodos(list, lookup: lookup, now: now, quiet: false).open == 0)
    }

    @Test func countsShelvedAndFinishedApartFromOpen() {
        let r = TodoTriageLogic.triageTodos([
            todo("run", agents: ["run"]),
            todo("stale", updated: 100, agents: ["old"]),
            todo("closed", done: 2),
            todo("accepted", agents: ["old"]) { $0.supervision = acceptedReopened() },
            todo("quiet") { $0.quiet = true },
        ], lookup: lookup, now: now, quiet: false)
        // Each its own: the array literal against the three counts no longer type-checks (ambiguous `==`, Xcode 27.1).
        #expect(r.open == 1)
        #expect(r.shelved == 1)
        #expect(r.finished == 2)
    }

    @Test func relatedKeysIgnoreCaseSpacesPunctuationAndATrailingSlash() {
        let t = todo("a", title: "Fix: the  Widget!") { $0.prURL = "https://Example.com/pull/3/" }
        #expect(TodoTriageLogic.relatedKeys(t) == ["pr:https://example.com/pull/3", "title:fixthewidget"])
    }

    @Test func restoresKeptOnThisDeviceDroppingOnesThatWouldHaveShelvedAgain() {
        let defaults = UserDefaults(suiteName: "TodoTriageTests")!
        defaults.removePersistentDomain(forName: "TodoTriageTests")
        TodoRestores.restore(["old"], defaults, now: now.addingTimeInterval(-TodoTriageLogic.shelveAfter - 1))
        TodoRestores.restore(["new"], defaults, now: now)
        #expect(TodoRestores.load(defaults, now: now) == ["new": now])
    }

    @Test func decodesDeliveryAndReopenedAt() throws {
        let json = #"{"id":"t","title":"x","reopened_at":"2026-11-10T08:00:00Z","delivery":{"state":"pr","gate":"review","at":"2026-11-10T09:00:00Z"}}"#
        let t = try JSON.decoder.decode(TodoItem.self, from: Data(json.utf8))
        #expect(t.delivery?.state == "pr" && t.delivery?.gate == "review" && t.reopenedAt != nil)
        let bad = try JSON.decoder.decode(TodoItem.self, from: Data(#"{"id":"t","title":"x","delivery":"live"}"#.utf8))
        #expect(bad.delivery == nil)
    }
}

extension TodoTriageTests {
    @Test func issueNumberMatchesTheWebPattern() {
        #expect(TodoTriageLogic.issueNumber("Fix #412 lost pushes") == "412")
        #expect(TodoTriageLogic.issueNumber("#7 too short, #12345678 too long, then #99") == "99")
        #expect(TodoTriageLogic.issueNumber("#123abc") == nil)
        #expect(TodoTriageLogic.issueNumber("修复 #3206。") == "3206")
    }
}
