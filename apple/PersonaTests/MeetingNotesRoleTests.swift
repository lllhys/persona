import Foundation
import Testing
@testable import Persona
import PersonaWire

/// The notes role on the phone (#8851, #8731): a room meeting Persona isn't invited to starts as `notes` on a hub that
/// lists `meeting_notes`, so its room follows it (the live log, topics, highlights) while Persona answers nothing; Persona
/// invited answers; removed, the meeting goes back to `notes`. Pickers never offer notes and show it as Off.
@MainActor
struct MeetingNotesRoleTests {
    private func meeting(_ attendance: RecordingAttendance) -> RecordingEntry {
        var e = RecordingEntry(id: UUID(), createdAt: Date(timeIntervalSince1970: 1_800_000_000), source: .phone, title: "Weekly",
                               locale: "zh-CN", active: true)
        e.meeting = RecordingMeeting(title: "Weekly")
        e.attendance = attendance
        return e
    }

    @Test func aMeetingPersonaIsntInStartsAsNotesWhereTheHubTakesIt() {
        let a = RecordingAttendance.room(.off, private: false, photos: false, liveView: true, notes: true)
        #expect(a.role == .notes && a.sentRole == .notes && a.notes && a.outRole == .notes)
        #expect(a.role.attends && !a.role.answers, "the room follows; Persona answers nothing")
        let e = meeting(a)
        #expect(e.attended && !e.answered)
        #expect(!MeetingAsk.canAsk(e, roomMissing: false), "nobody to ask")
        #expect(MeetingHighlights.attending(e, room: nil, roomMissing: false, refused: false), "the highlights still come")
    }

    @Test func withoutTheFlagOrTheLiveViewItIsOff() {
        #expect(RecordingAttendance.room(.off, private: false, photos: false, liveView: true, notes: false).role == .off)
        let noLive = RecordingAttendance.room(.off, private: false, photos: false, liveView: false, notes: true)
        #expect(noLive.role == .off && !noLive.notes && noLive.outRole == .off, "notes only comes with the live view")
    }

    @Test func invitedPersonaAnswersAndOutIsNotes() {
        let a = RecordingAttendance.room(.listener, private: false, photos: false, liveView: true, notes: true)
        #expect(a.role == .listener && a.outRole == .notes)
        let e = meeting(a)
        #expect(e.answered && MeetingAsk.canAsk(e, roomMissing: false))
    }

    @Test func pickersShowNotesAsOff() {
        #expect(!MeetingAttendance.choices.contains(.notes))
        #expect(MeetingAttendance.notes.shown == .off && MeetingAttendance.listener.shown == .listener)
        #expect(MeetingAttendance.notes.title == MeetingAttendance.off.title)
    }

    @Test func theFlagIsKeptAndAnOlderFileHasNone() throws {
        let a = RecordingAttendance.room(.off, private: false, photos: false, liveView: true, notes: true)
        let back = try JSONDecoder().decode(RecordingAttendance.self, from: JSONEncoder().encode(a))
        #expect(back.notes && back.role == .notes)
        let older = try JSONDecoder().decode(RecordingAttendance.self, from: Data(#"{"role":"off","room":true,"liveView":true}"#.utf8))
        #expect(!older.notes && older.outRole == .off)
    }

    /// Persona coming in and going out while a room meeting records is noted with when it happened (the thread's joined /
    /// left rows, #8731); out loud or on screen is no coming or going, a recording that isn't a room meeting notes none,
    /// and an older file has none.
    @Test func comingInAndGoingOutIsNoted() throws {
        let t0 = Date(timeIntervalSince1970: 1_800_000_000)
        var a = RecordingAttendance.room(.listener, private: false, photos: false, liveView: true, notes: true)
        a.turn(.participant, at: 10, now: t0)
        #expect(a.presence.isEmpty, "out loud is no coming or going")
        a.turn(.notes, at: 20, now: t0.addingTimeInterval(20))
        a.turn(.listener, at: 30, now: t0.addingTimeInterval(30))
        #expect(a.presence == [RecordingPresence(at: t0.addingTimeInterval(20), answers: false),
                               RecordingPresence(at: t0.addingTimeInterval(30), answers: true)])
        #expect(a.role == .listener && a.periods.count == 1, "notes still attends: one period")
        let back = try JSONDecoder().decode(RecordingAttendance.self, from: JSONEncoder().encode(a))
        #expect(back.presence == a.presence)
        let older = try JSONDecoder().decode(RecordingAttendance.self, from: Data(#"{"role":"listener","room":true}"#.utf8))
        #expect(older.presence.isEmpty)
        var notRoom = RecordingAttendance(role: .listener, at: 0)
        notRoom.turn(.off, at: 5, now: t0)
        #expect(notRoom.presence.isEmpty)
    }

    @Test func theHubsFlagIsRemembered() {
        let store = UserDefaults(suiteName: "meeting-notes-\(UUID())")!
        MeetingAttendanceDefaults.remember(conversation: true, photos: false, liveView: true, notes: true, store)
        #expect(store.bool(forKey: MeetingAttendanceDefaults.notesKey))
        MeetingAttendanceDefaults.remember(conversation: true, photos: false, liveView: true, notes: false, store)
        #expect(!store.bool(forKey: MeetingAttendanceDefaults.notesKey))
    }
}
