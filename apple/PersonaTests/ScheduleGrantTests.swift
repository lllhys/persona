import Foundation
import Testing
@testable import Persona
import PersonaWire

/// A scheduled task's own start grant (#8045): a plan its run made carries the schedule and offers the option only when a
/// task in it changes files; Start sends `schedule_grant` only when it is ticked; a task's `start_grant` is read into
/// kinds (computer, folder, read or edit) and never sent back; Remove sends DELETE …/start-grant with that kind.
@MainActor
struct ScheduleGrantTests {
    private func object(_ data: Data?) throws -> [String: Any] {
        let bytes = try #require(data)
        return try #require(try JSONSerialization.jsonObject(with: bytes) as? [String: Any])
    }

    private func plan(_ json: String) throws -> TaskPlan {
        try JSON.decoder.decode(TaskPlan.self, from: Data(json.utf8))
    }

    @Test func aPlanFromAScheduledRunOffersTheGrantOnlyForWritableTasks() throws {
        let scheduled = try plan(#"{"id":"p1","conversation":"c1","state":"open","tasks":[{"title":"a","prompt":"…","machine":"m1","cwd":"/w","schedule":"t1","schedule_title":"Weekly report"}]}"#)
        #expect(scheduled.tasks[0].schedule == "t1")
        #expect(scheduled.grantableSchedule?.id == "t1")
        #expect(scheduled.grantableSchedule?.title == "Weekly report")

        let readOnly = try plan(#"{"id":"p1","conversation":"c1","state":"open","tasks":[{"title":"a","prompt":"…","machine":"m1","cwd":"/w","readonly":true,"schedule":"t1","schedule_title":"Weekly report"}]}"#)
        #expect(readOnly.grantableSchedule == nil)

        let plain = try plan(#"{"id":"p1","conversation":"c1","state":"open","tasks":[{"title":"a","prompt":"…","machine":"m1","cwd":"/w"}]}"#)
        #expect(plain.grantableSchedule == nil)

        // never sent back with an edit
        let edit = try object(JSON.encoder.encode(scheduled.tasks[0]))
        #expect(edit["schedule"] == nil)
        #expect(edit["schedule_title"] == nil)
    }

    @Test func choicesGrantOnlyWhenTickedAndOffered() throws {
        let scheduled = try plan(#"{"id":"p1","conversation":"c1","state":"open","tasks":[{"title":"a","prompt":"…","machine":"m1","cwd":"/w","schedule":"t1"}]}"#)
        let plain = try plan(#"{"id":"p1","conversation":"c1","state":"open","tasks":[{"title":"a","prompt":"…","machine":"m1","cwd":"/w"}]}"#)
        let choices = PlanChoices()
        #expect(!choices.grantsSchedule(scheduled))
        choices.scheduleGrant = true
        #expect(choices.grantsSchedule(scheduled))
        #expect(!choices.grantsSchedule(plain))
    }

    @Test func startSendsScheduleGrantOnlyWhenSet() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/task-plans/p1/start", json: #"{"plan":{"id":"p1","conversation":"c1","state":"started","tasks":[]}}"#)
        _ = try await hub.client.startPlan("p1")
        _ = try await hub.client.startPlan("p1", remember: true, scheduleGrant: true)
        let sent = hub.sent("POST", "/api/v1/task-plans/p1/start")
        #expect(sent.count == 2)
        let plain = try object(sent[0].body)
        #expect(plain["schedule_grant"] == nil)
        #expect(plain["remember"] as? Bool == false)
        let granted = try object(sent[1].body)
        #expect(granted["schedule_grant"] as? Bool == true)
        #expect(granted["remember"] as? Bool == true)
    }

    @Test func aTaskReadsItsGrantIntoKindsAndNeverSendsIt() throws {
        let json = #"{"id":"t1","title":"Weekly report","prompt":"p","repeat":"daily","time":"09:00","enabled":true,"start_grant":{"kinds":["m1|/home/me/notes|edit","m1|/home/me/notes|read","old|id|/srv|edit"],"basis":"x","granted_at":"2026-11-10T09:00:00Z"}}"#
        let task = try JSON.decoder.decode(APIClient.ScheduledTask.self, from: Data(json.utf8))
        let kinds = try #require(task.startGrant?.kinds)
        #expect(kinds.map(\.key) == ["m1|/home/me/notes|edit", "m1|/home/me/notes|read", "old|id|/srv|edit"])
        #expect(kinds[0].machine == "m1" && kinds[0].folder == "/home/me/notes" && !kinds[0].readonly)
        #expect(kinds[1].readonly)
        // a computer and folder that can't be told apart: the whole place, no computer
        #expect(kinds[2].machine.isEmpty && kinds[2].folder == "old|id|/srv" && !kinds[2].readonly)
        #expect(task.startGrant?.grantedAt != nil)

        let none = try JSON.decoder.decode(APIClient.ScheduledTask.self, from: Data(#"{"id":"t2","prompt":"p"}"#.utf8))
        #expect(none.startGrant == nil)

        let body = try object(JSON.encoder.encode(task))
        #expect(body["start_grant"] == nil)
    }

    @Test func removeSendsTheKindAndReadsTheTaskBack() async throws {
        let hub = FakeHub()
        hub.on("DELETE", "/api/v1/assistant/tasks/t1/start-grant",
               json: #"{"task":{"id":"t1","prompt":"p","repeat":"daily","start_grant":{"kinds":["m1|/w|read"]}}}"#)
        let task = try await hub.client.revokeStartGrant(task: "t1", kind: "m1|/w|edit")
        #expect(task.startGrant?.kinds.map(\.key) == ["m1|/w|read"])
        let sent = hub.sent("DELETE", "/api/v1/assistant/tasks/t1/start-grant")
        #expect(sent.count == 1)
        #expect(try object(sent[0].body)["kind"] as? String == "m1|/w|edit")
    }
}
