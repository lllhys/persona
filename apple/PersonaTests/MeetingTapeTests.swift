import AVFoundation
import Foundation
import Testing
@testable import Persona

/// A meeting's tape and its replay (#9417, docs/apple/recording.md "A meeting's tape and its replay"), and the audio file in
/// place of the microphone (`RecordingFileInput`): the made-up tape (`MeetingTapeSample`) is the fixture
/// Fixtures/meeting-tape/tape.json and survives a round trip; what the room holds at a moment of the tape (`frame(at:)`);
/// the replay's schedule at 1×, 2× and 4×, its speed's bounds and that it holds only its own meeting; the file input's
/// launch argument, its real-time pace and pause; and a writer recording from the fixture's audio as from a microphone.
@MainActor struct MeetingTapeTests {
    private static let fixtures = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appending(path: "Fixtures/meeting-tape")

    // MARK: The tape

    @Test func theFixtureIsTheMadeUpTape() throws {
        let tape = try MeetingTape.decode(Data(contentsOf: Self.fixtures.appending(path: "tape.json")))
        let sample = MeetingTapeSample.tape
        #expect(tape.events == sample.events)
        #expect(tape.minutes == sample.minutes)
        #expect(tape == sample)
    }

    @Test func aTapeSurvivesARoundTrip() throws {
        let tape = MeetingTapeSample.tape
        #expect(try MeetingTape.decode(tape.encoded()) == tape)
    }

    @Test func anotherFormatIsNoTape() {
        let json = Data(#"{"format": "something-else", "version": 1, "title": "x", "startedAt": "2027-01-01T00:00:00Z", "events": []}"#.utf8)
        #expect(throws: (any Error).self) { try MeetingTape.decode(json) }
    }

    @Test func aFrameHoldsWhatCameUpToItsMoment() {
        let tape = MeetingTapeSample.tape
        #expect(tape.frame(at: 0.5) == MeetingTape.Frame())
        // A third of the first line's words, then two thirds, as captions.
        let first = MeetingTapeSample.lines[0].text
        #expect(tape.frame(at: 1.5).interim?.text == String(first.prefix(first.count / 3)))
        #expect(tape.frame(at: 1.5).room?.known == .ready)
        #expect(tape.frame(at: 2.7).interim?.text == String(first.prefix(first.count * 2 / 3)))
        // The line finishes its caption.
        let lined = tape.frame(at: 7.6)
        #expect(lined.lines.map(\.text) == [first])
        #expect(lined.interim == nil)
        #expect(tape.frame(at: 10.5).topics.map(\.title) == ["录音入口", "发布时间"])
        #expect(tape.frame(at: 12.9).highlights.isEmpty)
        #expect(tape.frame(at: 13).highlights.count == 1)
        let end = tape.frame(at: tape.length)
        #expect(end.lines.map(\.n) == Array(0..<6))
        #expect(end.lines.map(\.text) == MeetingTapeSample.lines.map(\.text))
        #expect(end.interim == nil)
        #expect(end.highlights.count == 2)
        #expect(end.topics.map(\.title) == ["录音入口", "发布时间", "发布说明"])
        #expect(MeetingTopic.current(end.topics)?.title == "发布说明")
    }

    @Test func aLineReadAgainTakesItsPlace() {
        var frame = MeetingTape.Frame()
        frame.apply(.line(MeetingHubLine(n: 1, start: 5, end: 8, text: "second"), t: 9, at: 9))
        frame.apply(.line(MeetingHubLine(n: 0, start: 0, end: 4, text: "first"), t: 10, at: 10))
        frame.apply(.line(MeetingHubLine(n: 1, start: 5, end: 8, text: "second, corrected"), t: 11, at: 11))
        #expect(frame.lines.map(\.text) == ["first", "second, corrected"])
    }

    // MARK: The replay

    @Test func theScheduleKeepsTheTapesOrderAtEverySpeed() {
        let tape = MeetingTapeSample.tape
        let once = tape.schedule(speed: 1)
        #expect(once.map(\.at) == once.map(\.at).sorted())
        #expect(once.map(\.event) == tape.ordered)
        for speed in [2.0, 4.0] {
            let fast = tape.schedule(speed: speed)
            #expect(fast.map(\.event) == once.map(\.event))
            #expect(zip(fast, once).allSatisfy { abs($0.at * speed - $1.at) < 1e-9 })
        }
    }

    @Test func theSpeedStaysWithinBounds() {
        #expect(MeetingReplay.clampedSpeed(0) == 1)
        #expect(MeetingReplay.clampedSpeed(-2) == 1)
        #expect(MeetingReplay.clampedSpeed(.nan) == 1)
        #expect(MeetingReplay.clampedSpeed(0.1) == 0.25)
        #expect(MeetingReplay.clampedSpeed(2) == 2)
        #expect(MeetingReplay.clampedSpeed(100) == 16)
    }

    @Test func theReplayHoldsOnlyItsOwnMeetingAndNeverSyncsIt() {
        #expect(MeetingReplay.holds(MeetingReplay.id))
        #expect(!MeetingReplay.holds(UUID()))
        var replayed = RecordingEntry(id: MeetingReplay.id, createdAt: .now, source: .phone, title: "产品周会", active: true)
        replayed.attendance = RecordingAttendance.room(.listener, private: false, photos: false, liveView: true, highlights: true)
        replayed.segments = [RecordingSegment(index: 0, start: 0, duration: 15, sentences: [RecordingSentence(start: 1, end: 4, text: "你好")])]
        #expect(RecordingSync.uploadable(replayed))
        #expect(RecordingSync.shared.status(replayed) == .waiting)
    }

    @Test func theReplayedMeetingShowsTheHubsLinesAndTheRoomsLists() {
        let id = UUID()
        let frame = MeetingTapeSample.tape.frame(at: 28)
        let transcription = MeetingTranscription(deps: .live)
        transcription.showReplay(id, frame: frame)
        #expect(transcription.meeting(id).lines == frame.lines)
        #expect(transcription.hubTranscribes(id))
        #expect(transcription.shown(id, role: .listener).hubLines)
        #expect(!transcription.shown(id, role: .listener).fellBack)
        let conversation = MeetingConversation(deps: .live)
        conversation.showReplay(id, frame: frame)
        #expect(conversation.meeting(id).topics == frame.topics)
        #expect(conversation.meeting(id).highlights == frame.highlights)
        #expect(conversation.meeting(id).room?.known == .ready)
        #expect(conversation.meeting(id).room?.key(group: "g") == nil)
    }

    // MARK: The audio file in place of the microphone

    @Test func theLaunchArgumentNamesAFileThatIsThere() throws {
        let defaults = try #require(UserDefaults(suiteName: "MeetingTapeTests.\(UUID().uuidString)"))
        #expect(RecordingFileInput.requested(defaults) == nil)
        let audio = Self.fixtures.appending(path: "audio.m4a")
        defaults.set(audio.path, forKey: RecordingFileInput.key)
        #expect(RecordingFileInput.requested(defaults)?.path == audio.path)
        defaults.set("audio.m4a", forKey: RecordingFileInput.key)
        #expect(RecordingFileInput.requested(defaults, documents: Self.fixtures)?.path == audio.path)
        defaults.set("missing.m4a", forKey: RecordingFileInput.key)
        #expect(RecordingFileInput.requested(defaults, documents: Self.fixtures) == nil)
    }

    @Test func theFileComesAtRealTimeAndPauses() async throws {
        let count = Counter()
        let input = try RecordingFileInput(url: Self.fixtures.appending(path: "audio.m4a")) { buffer, _, _ in
            count.add(Double(buffer.frameLength) / buffer.format.sampleRate)
        }
        #expect(abs(input.duration - 30) < 0.1)
        input.start()
        try await Task.sleep(for: .milliseconds(600))
        input.pause()
        let played = input.position
        #expect(played > 0.3 && played < 1.2, "\(played) s handed over in 0.6 s")
        #expect(abs(count.seconds - played) < 0.001)
        try await Task.sleep(for: .milliseconds(300))
        #expect(input.position == played, "it went on while paused")
        input.start()
        try await Task.sleep(for: .milliseconds(300))
        input.stop()
        let stopped = input.position
        #expect(stopped > played)
        try await Task.sleep(for: .milliseconds(200))
        #expect(input.position == stopped, "it went on after stop")
        #expect(!input.ended)
    }

    @Test func aWriterRecordsTheFileAsItWouldTheMicrophone() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "MeetingTapeTests-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: folder) }
        let writer = RecordingSegmentWriter(directory: folder)
        writer.fileInput = Self.fixtures.appending(path: "audio.m4a")
        let heard = Counter()
        writer.listener = { buffer, _ in heard.add(Double(buffer.frameLength) / RecordingAudioSpec.sampleRate) }
        try writer.start()
        #expect(writer.isRunning)
        try await Task.sleep(for: .milliseconds(1200))
        let segment = try #require(writer.stop())
        #expect(!writer.isRunning)
        #expect(segment.duration > 0.8 && segment.duration < 2, "\(segment.duration) s written")
        #expect(abs(writer.elapsed - segment.duration) < 0.05)
        #expect(abs(heard.seconds - writer.elapsed) < 0.05, "the listener heard \(heard.seconds) s")
        #expect(FileManager.default.fileExists(atPath: segment.url.path))
        // The file holds sound: the fixture speaks from 0.5 s.
        #expect(segment.level > -60)
        // Nothing after Stop.
        let stopped = writer.elapsed
        try await Task.sleep(for: .milliseconds(200))
        #expect(writer.elapsed == stopped)
    }
}

/// Seconds counted from the input's queue.
private final class Counter: @unchecked Sendable {
    private let lock = NSLock()
    private var total = 0.0
    func add(_ seconds: Double) { lock.withLock { total += seconds } }
    var seconds: Double { lock.withLock { total } }
}
