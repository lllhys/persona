import AVFoundation
import Foundation
import Testing
@testable import Persona

struct RecordingSegmentWriterTests {
    private func silence(seconds: Double) throws -> AVAudioPCMBuffer {
        let format = try #require(AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: RecordingAudioSpec.sampleRate,
                                                channels: 1, interleaved: false))
        let frames = AVAudioFrameCount(seconds * RecordingAudioSpec.sampleRate)
        let buffer = try #require(AVAudioPCMBuffer(pcmFormat: format, frameCapacity: frames))
        buffer.frameLength = frames
        buffer.floatChannelData?[0].update(repeating: 0, count: Int(frames))
        return buffer
    }

    /// Segment start times come from the audio, not from when a segment is reported, so a pause or a late callback
    /// doesn't shift them.
    @Test func segmentsCarryTheWallClockOfTheirFirstSample() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: dir) }
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let writer = RecordingSegmentWriter(directory: dir)
        writer.segmentLength = 10
        let start = Date(timeIntervalSince1970: 1_800_000_000)
        writer.append(try silence(seconds: 25), startedAt: start)
        // Paused for a while: the third segment started before the pause and finishes after it.
        writer.append(try silence(seconds: 5), startedAt: start.addingTimeInterval(100))
        writer.append(try silence(seconds: 10), startedAt: start.addingTimeInterval(200))
        let segments = writer.finishedSegments
        #expect(segments.map(\.index) == [0, 1, 2, 3])
        #expect(segments.map(\.startedAt) == [0, 10, 20, 200].map { start.addingTimeInterval($0) })
        #expect(segments.map(\.start) == [0, 10, 20, 30])
        #expect(segments.allSatisfy { abs($0.duration - 10) < 0.001 })
    }

    /// Talk's press is never placed before it happened (#3862 phase 4): a 4096-frame buffer at a Bluetooth headset's
    /// 8 kHz is 0.512 s, delivered late it is more, and a stall more still; the old half-second clamp put the press that
    /// much early, so the room's words from before it went up. Only the release is held back.
    @Test func aPressIsNeverPlacedBeforeItHappened() {
        let buffer = 4096.0 / 8000
        let base = 100.0
        let margin = RecordingSegmentWriter.pressMargin
        // The newest buffer's last sample 0.53 s ago: a whole 0.512 s buffer filling, delivered 18 ms late.
        let press = RecordingSegmentWriter.placed(.press, base: base, since: 0.53, longestBuffer: buffer)
        #expect(press >= base + 0.53, "not clamped to half a second")
        #expect(abs(press - (base + 0.53 + margin)) < 1e-9)
        #expect(RecordingSegmentWriter.placed(.release, base: base, since: 0.53, longestBuffer: buffer) == base + 0.53,
                "a release in normal running is where it happened")
        // A stall: nothing came for 2 s.
        #expect(RecordingSegmentWriter.placed(.press, base: base, since: 2, longestBuffer: buffer) >= base + 2, "later, never earlier")
        let release = RecordingSegmentWriter.placed(.release, base: base, since: 2, longestBuffer: buffer)
        #expect(release < base + 2, "held back")
        #expect(abs(release - (base + buffer + RecordingSegmentWriter.deliveryMargin)) < 1e-9, "to the longest buffer and its delivery")
        // A clock that ran back, or no buffer yet: at `base` (the press its margin on).
        #expect(RecordingSegmentWriter.placed(.press, base: base, since: -0.01, longestBuffer: 0) == base + margin)
        #expect(RecordingSegmentWriter.placed(.release, base: base, since: -0.01, longestBuffer: 0) == base)
        #expect(RecordingSegmentWriter.placed(.release, base: base, since: 0.3, longestBuffer: 0) == base + RecordingSegmentWriter.deliveryMargin,
                "before the first buffer a release runs at most its delivery ahead")
    }
}
