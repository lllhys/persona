#if os(iOS)
import AVFoundation
import Foundation
import os

// A meeting call's audio on the recorder's capture (#3862 phase 4, docs/assistant/meeting-minutes.md「One capture」): the
// recording keeps the microphone for the whole meeting and the call never claims it. The recorder's one input tap, already
// at 16 kHz, feeds the segment writer and, through `MeetingTalkTap`, the call's uplink, which sends only inside Talk
// spans (`VoiceUplink.talk`). The call's voice plays through the player on the recorder's own engine
// (`RecordingCapture.callPlayer`), so the system's echo cancellation, when the recording asked for it, takes it out of
// what the microphone records.

/// The recorder's audio as Talk hears it (#3862 phase 4): of every buffer of the tap (16 kHz mono float), the samples inside
/// the open Talk span go to the meeting call's uplink as PCM16, when there is a call, and its loudness is kept for the
/// talk window's silence, with the room's over the last seconds (`room(before:)`). A buffer reaches the tap only once it
/// is full (85 to 512 ms), so the span is placed on the
/// buffers' own clock (`RecordingCapture.listenPosition` at the press and the release), not on when they arrive: the
/// part of a buffer from before the press stays out, and the buffer that holds the release still sends its part before
/// `talk off` goes (`covers`). Called on the audio thread; the main actor connects, opens and closes it.
nonisolated final class MeetingTalkTap: @unchecked Sendable {
    private let lock = NSLock()
    private var uplink: VoiceUplink?
    private var db: Float = MeetingTalkTap.quiet
    /// The span on the buffers' clock: from `start`, up to `end` once it is let go; nil outside one.
    private var span: (start: Double, end: Double?)?
    /// Where the newest buffer it has sent from (or passed over) ends, on that clock.
    private var through = -Double.infinity
    /// The room as the tap heard it over the last `MeetingTalkWindow.roomSpan` seconds, in pieces of about `piece`, oldest
    /// first, on that clock.
    private var heard: [Piece] = []

    /// A short piece of a buffer: where it begins (on the buffers' clock, or within its buffer), how long, how loud.
    nonisolated struct Piece: Equatable, Sendable {
        var at: Double
        var seconds: Double
        var db: Float
    }

    /// The level of no sound at all.
    static let quiet: Float = -160
    /// The length of the pieces the room's level is taken from: about a syllable, so the pauses between words count.
    static let piece: Double = 0.1

    init() {
        heard.reserveCapacity(128)
    }

    /// The call's uplink, or nil when the call goes (its audio then goes nowhere).
    func connect(_ uplink: VoiceUplink?) {
        lock.withLock { self.uplink = uplink }
    }

    /// The newest buffer's level, dBFS.
    var level: Float { lock.withLock { db } }

    /// A span sent to the call opens at `start` (seconds on the buffers' clock).
    func open(at start: Double) {
        lock.withLock { span = (start, nil) }
    }

    /// The span is let go at `end`: nothing after it goes, whatever buffer it is in.
    func close(at end: Double) {
        lock.withLock {
            guard let open = span else { return }
            span = (open.start, max(open.start, end))
        }
    }

    /// No span any more (its `talk off` went).
    func clear() {
        lock.withLock { span = nil }
    }

    /// The buffers up to `position` have been through the tap (what of them was the span's has gone up).
    func covers(_ position: Double) -> Bool {
        lock.withLock { through >= position }
    }

    /// What the tap heard of the room in the `MeetingTalkWindow.roomSpan` seconds before `position` (a talk window's
    /// press), for the window's idea of speech.
    func room(before position: Double) -> MeetingTalkWindow.Room {
        lock.withLock {
            var room = MeetingTalkWindow.Room()
            for piece in heard where piece.at >= position - MeetingTalkWindow.roomSpan && piece.at < position {
                room.levels.append(piece.db)
                room.seconds += piece.seconds
            }
            return room
        }
    }

    /// Forgets the room heard so far (another recording, whose clock starts again).
    func forgetRoom() {
        lock.withLock { heard.removeAll(keepingCapacity: true) }
    }

    /// One buffer of the recording's audio, as the tap converted it, whose first sample lies at `position`.
    func feed(_ buffer: AVAudioPCMBuffer, at position: Double) {
        guard let channel = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return }
        let count = Int(buffer.frameLength)
        let rate = buffer.format.sampleRate
        let samples = UnsafeBufferPointer(start: channel, count: count)
        let (level, pieces) = Self.levels(samples, rate: rate)
        let (uplink, span) = lock.withLock { () -> (VoiceUplink?, (start: Double, end: Double?)?) in
            db = level
            for var piece in pieces {
                piece.at += position
                heard.append(piece)
            }
            // Only the last seconds: older pieces go (a few dozen are kept at any time).
            let from = position + Double(count) / rate - MeetingTalkWindow.roomSpan
            if let keep = heard.firstIndex(where: { $0.at >= from }) {
                if keep > 0 { heard.removeFirst(keep) }
            } else {
                heard.removeAll(keepingCapacity: true)
            }
            return (self.uplink, self.span)
        }
        // Converted only for a span (the uplink checks again under its own lock, and resamples it to the call's rate).
        if let uplink, let span, uplink.isTalking,
           let range = Self.inside(start: span.start, end: span.end, position: position, count: count, rate: rate) {
            uplink.push(Self.pcm16(UnsafeBufferPointer(rebasing: samples[range])), rate: Int(rate))
        }
        // After the push: whoever waits on `covers` to send `talk off` sends it behind this buffer's part.
        lock.withLock { through = max(through, position + Double(count) / rate) }
    }

    /// Which of a buffer's `count` samples at `rate`, the first at `position`, lie in [`start`, `end`) (no `end`: the span
    /// is still held); nil when none do. A sample belongs to the span from where it was captured, to the nearest sample.
    static func inside(start: Double, end: Double?, position: Double, count: Int, rate: Double) -> Range<Int>? {
        func index(_ at: Double) -> Int { min(count, max(0, Int(((at - position) * rate).rounded()))) }
        let lo = index(start)
        let hi = end.map(index) ?? count
        return lo < hi ? lo..<hi : nil
    }

    /// Float samples in [-1, 1] as little-endian PCM16, clipped.
    static func pcm16(_ samples: UnsafeBufferPointer<Float>) -> Data {
        var out = [Int16](repeating: 0, count: samples.count)
        for i in 0..<samples.count {
            out[i] = Int16(max(-32768, min(32767, (samples[i] * 32767).rounded())))
        }
        return VoicePCM.data(out)
    }

    /// Root mean square in dBFS (`quiet` for silence).
    static func level(_ samples: UnsafeBufferPointer<Float>) -> Float {
        guard !samples.isEmpty else { return quiet }
        var sum: Float = 0
        for x in samples { sum += x * x }
        return dbfs(sum, count: samples.count)
    }

    /// The level of a buffer at `rate`, and of its pieces: as many nearly equal ones as make about `piece` each (one for
    /// a buffer shorter than that), each `at` from the buffer's start. One pass over the samples.
    static func levels(_ samples: UnsafeBufferPointer<Float>, rate: Double) -> (level: Float, pieces: [Piece]) {
        guard !samples.isEmpty, rate > 0 else { return (quiet, []) }
        let count = samples.count
        let parts = max(1, Int((Double(count) / (rate * piece)).rounded()))
        var pieces: [Piece] = []
        pieces.reserveCapacity(parts)
        var total: Float = 0
        for part in 0..<parts {
            let lo = part * count / parts
            let hi = (part + 1) * count / parts
            var sum: Float = 0
            for i in lo..<hi { sum += samples[i] * samples[i] }
            total += sum
            pieces.append(Piece(at: Double(lo) / rate, seconds: Double(hi - lo) / rate, db: dbfs(sum, count: hi - lo)))
        }
        return (dbfs(total, count: count), pieces)
    }

    /// The level, in dBFS, of `count` samples whose squares sum to `sum` (`quiet` for silence).
    private static func dbfs(_ sum: Float, count: Int) -> Float {
        guard count > 0 else { return quiet }
        let rms = (sum / Float(count)).squareRoot()
        return rms > 0 ? max(quiet, 20 * log10(rms)) : quiet
    }
}

/// `VoiceAudio` on the recorder (#3862 phase 4). Starting it connects the call's uplink to the tap; the voice goes to the
/// recorder's player through the same jitter buffer as a phone call's (`VoicePlayout`); stopping it lets go of both and
/// leaves the recording as it was. A made-up recording (`-showMeetingSample`) has no player: the voice then "plays" for
/// as long as its PCM lasts, as `VoiceSample.SilentAudio` does, so the call and the echo spans follow it.
@MainActor final class MeetingCallAudio: VoiceAudio {
    nonisolated static let log = Logger(subsystem: "persona", category: "meeting-call")

    private let capture: RecordingCapture
    private let tap: MeetingTalkTap
    /// The voice began (true) or stopped (false) playing: the stretch the microphone may hear it in (`MeetingCall`).
    private let onPlaying: @MainActor (Bool) -> Void
    private var events: (@MainActor (VoiceAudioEvent) -> Void)?
    private var format = VoiceAudioFormat.provisional
    private var playout = VoicePlayout<Piece>(rate: VoiceFrame.defaultOutputRate)
    private var holdTimer: Task<Void, Never>?
    /// The loudness of each piece still queued, the first one playing.
    private var queued: [Float] = []
    /// Bumped by `clear` and `stop`: completions of what was scheduled before are ignored.
    private var generation = 0
    /// Without a player: when what was given would be played out, and the wait that ends it (nil: quiet).
    private var silentUntil = Date.distantPast
    private var silentEnd: Task<Void, Never>?

    init(capture: RecordingCapture, tap: MeetingTalkTap, onPlaying: @escaping @MainActor (Bool) -> Void) {
        self.capture = capture
        self.tap = tap
        self.onPlaying = onPlaying
    }

    /// The recording's own: asked for as the meeting started, never switched while it records.
    var cancelsEcho: Bool { capture.cancelsEcho }

    func start(_ format: VoiceAudioFormat, uplink: VoiceUplink, events: @escaping @MainActor (VoiceAudioEvent) -> Void) throws {
        guard capture.state == .recording || capture.state == .paused else { throw VoiceAudioError.engine }
        self.format = format
        self.events = events
        playout = VoicePlayout(rate: format.outputRate)
        tap.connect(uplink)
    }

    func reconfigure(_ format: VoiceAudioFormat) throws {
        guard format != self.format else { return }
        dropQueued()
        self.format = format
        playout = VoicePlayout(rate: format.outputRate)
    }

    func play(_ pcm: Data) { schedule(pcm, voice: true) }

    func cue(_ pcm: Data) { schedule(pcm, voice: false) }

    func clear() {
        let was = !queued.isEmpty || silentEnd != nil
        dropQueued()
        if let player = capture.callPlayer {
            player.stop()
            if capture.engineRunning { player.play() }
        }
        if was { stopped() }
    }

    func stop() {
        tap.connect(nil)
        let was = !queued.isEmpty || silentEnd != nil
        dropQueued()
        capture.callPlayer?.stop()
        if was { onPlaying(false) }
        events = nil
    }

    // MARK: Playing

    private struct Piece {
        let buffer: AVAudioPCMBuffer
        let level: Float
    }

    private func schedule(_ pcm: Data, voice: Bool) {
        guard !pcm.isEmpty else { return }
        guard let player = capture.callPlayer else { return voice ? playSilently(pcm) : () }
        guard capture.engineRunning else { return } // paused with the recording: the voice is gone, not saved for later
        // The player runs at the recorder's call rate: the voice at another rate is resampled on its way in.
        let rate = Int(RecordingCapture.callFormat.sampleRate)
        let samples = VoicePCM.floats(format.outputRate == rate ? pcm : VoicePCM.resample(pcm, from: format.outputRate, to: rate))
        guard !samples.isEmpty,
              let buffer = AVAudioPCMBuffer(pcmFormat: RecordingCapture.callFormat, frameCapacity: AVAudioFrameCount(samples.count)),
              let channel = buffer.floatChannelData?[0]
        else { return }
        buffer.frameLength = AVAudioFrameCount(samples.count)
        samples.withUnsafeBufferPointer { channel.update(from: $0.baseAddress!, count: samples.count) }
        guard voice else { return enqueue(buffer, level: nil, on: player) }
        let ready = playout.add(Piece(buffer: buffer, level: VoicePCM.rms(samples)), frames: samples.count, playing: !queued.isEmpty)
        if playout.isHolding {
            waitForLead()
        } else {
            holdTimer?.cancel()
            holdTimer = nil
        }
        for piece in ready { enqueue(piece.buffer, level: piece.level, on: player) }
    }

    /// Plays what is held once the first of it has waited `VoicePlayout.wait`, lead or not.
    private func waitForLead() {
        guard holdTimer == nil else { return }
        let generation = generation
        holdTimer = Task { [weak self] in
            try? await Task.sleep(for: .seconds(VoicePlayout<Piece>.wait))
            guard !Task.isCancelled, let self else { return }
            holdTimer = nil
            guard generation == self.generation, let player = capture.callPlayer, capture.engineRunning else { return playout.clear() }
            for piece in playout.release() { enqueue(piece.buffer, level: piece.level, on: player) }
        }
    }

    private func enqueue(_ buffer: AVAudioPCMBuffer, level: Float?, on player: AVAudioPlayerNode) {
        let generation = generation
        let voice = level != nil
        if let level {
            if queued.isEmpty { began(level) }
            queued.append(level)
        }
        player.scheduleBuffer(buffer, completionCallbackType: .dataPlayedBack) { @Sendable [weak self] _ in
            Task { @MainActor in self?.played(generation, voice: voice) }
        }
        if !player.isPlaying { player.play() }
    }

    private func played(_ generation: Int, voice: Bool) {
        guard voice, generation == self.generation, !queued.isEmpty else { return }
        queued.removeFirst()
        if let next = queued.first {
            events?(.outputLevel(next))
        } else {
            stopped()
        }
    }

    /// No player (a made-up recording): the voice takes as long as its PCM lasts.
    private func playSilently(_ pcm: Data) {
        let now = Date.now
        if silentEnd == nil { began(0.5) }
        silentUntil = max(silentUntil, now).addingTimeInterval(Double(pcm.count / 2) / Double(max(format.outputRate, 1)))
        silentEnd?.cancel()
        // As long as the jitter buffer would wait for the next piece: a voice streamed in 20 ms pieces is one stretch.
        let left = silentUntil.timeIntervalSince(now) + VoicePlayout<Piece>.wait
        silentEnd = Task { @MainActor [weak self] in
            try? await Task.sleep(for: .seconds(left))
            guard !Task.isCancelled, let self else { return }
            self.silentUntil = .distantPast
            self.silentEnd = nil
            self.stopped()
        }
    }

    private func began(_ level: Float) {
        events?(.playing(true))
        events?(.outputLevel(level))
        onPlaying(true)
    }

    private func stopped() {
        events?(.outputLevel(0))
        events?(.playing(false))
        onPlaying(false)
    }

    private func dropQueued() {
        generation += 1
        holdTimer?.cancel()
        holdTimer = nil
        playout.clear()
        queued = []
        silentEnd?.cancel()
        silentEnd = nil
        silentUntil = .distantPast
    }
}
#endif
