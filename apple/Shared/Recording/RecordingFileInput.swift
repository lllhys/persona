#if DEBUG
import AVFoundation
import Foundation
import os

/// An audio file in place of the microphone, for development and tests only (`-openMeetingAudio <file>`, #9417;
/// docs/apple/recording.md "An audio file in place of the microphone"). It reads the file at real time and hands
/// `RecordingSegmentWriter` its buffers as the input tap would, with the host time each one's last sample counts as captured
/// at, so everything after the tap (the 16 kHz conversion, the segment files, the level, Talk's listener and the hub uplink)
/// runs as it does for a microphone. No virtual sound card and no change to the system's audio: the engine's input is
/// never opened. After the file's end silence comes at the same pace, as a quiet room would give, so the recording's
/// clock goes on until it is stopped.
nonisolated final class RecordingFileInput: @unchecked Sendable {
    /// The launch argument's key: the file's absolute path, or its name in the app's Documents folder.
    static let key = "openMeetingAudio"

    /// The file `-openMeetingAudio` names, when it exists; nil without the argument.
    static func requested(_ defaults: UserDefaults = .standard, documents: URL? = nil) -> URL? {
        guard let value = defaults.string(forKey: key)?.trimmingCharacters(in: .whitespacesAndNewlines), !value.isEmpty else { return nil }
        let url = value.hasPrefix("/") ? URL(fileURLWithPath: value)
            : (documents ?? URL.documentsDirectory).appending(path: value)
        guard FileManager.default.fileExists(atPath: url.path) else {
            log.error("file input: \(value, privacy: .public) is not there; recording from the microphone")
            return nil
        }
        return url
    }

    /// What one buffer covers: about what a microphone's tap hands over at 44.1 or 48 kHz.
    static let bufferSeconds = 0.1

    /// The file's processing format (float, its own rate and channels): what the writer converts from.
    let format: AVAudioFormat
    /// The file's length, seconds.
    let duration: Double

    private let file: AVAudioFile
    private let deliver: @Sendable (AVAudioPCMBuffer, _ startedAt: Date, _ capturedUntil: Double) -> Void
    private let queue = DispatchQueue(label: "persona.recording-file-input", qos: .userInitiated)

    // Only touched on `queue`.
    private var timer: DispatchSourceTimer?
    /// Frames handed over so far, the file's and the silence after it.
    private var delivered: AVAudioFramePosition = 0
    /// Where the running stretch began: the frames handed over then, and the host time (seconds) it began at.
    private var stretch: (frames: AVAudioFramePosition, host: Double)?
    private var stopped = false
    private var reachedEnd = false

    /// - Parameter deliver: called on the input's own queue with each buffer (the file's format), the wall-clock time of
    ///   its first sample, and the host time (seconds) of its last, as the input tap's block gives them.
    init(url: URL, deliver: @escaping @Sendable (AVAudioPCMBuffer, _ startedAt: Date, _ capturedUntil: Double) -> Void) throws {
        file = try AVAudioFile(forReading: url)
        format = file.processingFormat
        duration = Double(file.length) / file.processingFormat.sampleRate
        self.deliver = deliver
        Self.log.notice("file input: \(url.lastPathComponent, privacy: .public), \(Int(self.duration), privacy: .public) s at \(Int(self.format.sampleRate), privacy: .public) Hz")
    }

    /// Starts, or goes on after `pause`, from where it was.
    func start() {
        queue.sync {
            guard !stopped, timer == nil else { return }
            stretch = (delivered, Self.hostNow())
            let timer = DispatchSource.makeTimerSource(queue: queue)
            timer.schedule(deadline: .now(), repeating: .milliseconds(20), leeway: .milliseconds(5))
            timer.setEventHandler { [weak self] in self?.tick() }
            timer.resume()
            self.timer = timer
        }
    }

    /// Stops handing buffers over until `start`; the clock leaves the paused time out, as a paused microphone does.
    func pause() {
        queue.sync {
            timer?.cancel()
            timer = nil
            stretch = nil
        }
    }

    /// Stops for good. Once it returns no buffer comes any more.
    func stop() {
        queue.sync {
            timer?.cancel()
            timer = nil
            stretch = nil
            stopped = true
        }
    }

    /// The file has been handed over whole (silence comes since).
    var ended: Bool { queue.sync { reachedEnd } }

    /// Seconds handed over so far.
    var position: Double { queue.sync { Double(delivered) / format.sampleRate } }

    // MARK: - Queue

    private func tick() {
        guard let stretch, !stopped else { return }
        let rate = format.sampleRate
        let now = Self.hostNow()
        let due = stretch.frames + AVAudioFramePosition((now - stretch.host) * rate)
        let chunk = AVAudioFrameCount(max(1, (rate * Self.bufferSeconds).rounded()))
        // Whole buffers only, as a tap hands them over: what is due but less than one waits for the next tick.
        while due - delivered >= AVAudioFramePosition(chunk) {
            guard let buffer = next(chunk) else { return }
            let first = stretch.host + Double(delivered - stretch.frames) / rate
            let last = first + Double(buffer.frameLength) / rate
            delivered += AVAudioFramePosition(buffer.frameLength)
            let startedAt = Date().addingTimeInterval(-max(0, now - first))
            deliver(buffer, startedAt, last)
        }
    }

    /// The next `frames` of the file, or silence once it ended.
    private func next(_ frames: AVAudioFrameCount) -> AVAudioPCMBuffer? {
        guard let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: frames) else { return nil }
        if !reachedEnd {
            do {
                try file.read(into: buffer, frameCount: frames)
            } catch {
                Self.log.error("file input: read failed, silence from here: \(error.localizedDescription, privacy: .public)")
                buffer.frameLength = 0
            }
            if buffer.frameLength < frames {
                reachedEnd = true
                Self.log.notice("file input: the file ended at \(Int(Double(self.delivered + AVAudioFramePosition(buffer.frameLength)) / self.format.sampleRate), privacy: .public) s; silence from here")
            }
            if buffer.frameLength > 0 { return buffer }
        }
        buffer.frameLength = frames
        for channel in 0..<Int(format.channelCount) {
            buffer.floatChannelData?[channel].update(repeating: 0, count: Int(frames))
        }
        return buffer
    }

    private static func hostNow() -> Double { AVAudioTime.seconds(forHostTime: mach_absolute_time()) }

    private static let log = Logger(subsystem: "persona", category: "recording")
}
#endif
