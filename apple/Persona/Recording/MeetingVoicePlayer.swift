#if os(iOS)
import AVFoundation
import OSLog

/// Plays the realtime voice's answer in a meeting (`MeetingSpeaker`, #5004): pieces of mono PCM16 as they come, in the
/// app's audio session as the recording set it up (no category or activation of its own, so the recording goes on).
@MainActor protocol MeetingVoicePlayback: AnyObject {
    /// It plays on the recorder's own engine (a room meeting), which a pause of the recording stops too.
    var onRecorder: Bool { get }
    /// Gets ready at `rate`. `started` when the first audio begins to play; `ended` once everything given before
    /// `finish` has played, or it was cut (an interruption, the output gone). Never after `stop`.
    func start(rate: Int, started: @escaping @MainActor () -> Void, ended: @escaping @MainActor () -> Void) throws
    func play(_ pcm: Data)
    /// Nothing more comes: it ends once what it has is played.
    func finish()
    func stop()
}

extension MeetingVoicePlayback {
    var onRecorder: Bool { false }
}

/// An `AVAudioPlayerNode`: in a room meeting the recorder's answer player (`RecordingCapture.answerPlayer`, #3862 phase
/// 4), on the engine whose voice processing cancels the microphone's echo, so the answer is taken out of the recording
/// and not ducked as other audio is; elsewhere, or while that engine is paused, one on an engine of its own. It holds the
/// first `lead` seconds before it starts, so a service that sends at about the pace it speaks doesn't stutter at the
/// start (as the call's `VoicePlayout`); after that each piece goes straight behind the last.
final class MeetingVoicePlayer: MeetingVoicePlayback {
    static let lead: TimeInterval = 0.3

    /// The engine of its own; nil on the recorder's.
    private var engine: AVAudioEngine?
    private var node: AVAudioPlayerNode?
    private var format: AVAudioFormat?
    /// The rate the voice comes at (the player's format may be another: the recorder's is `RecordingCapture.callFormat`).
    private var rate = 0
    private(set) var onRecorder = false
    private var held: [AVAudioPCMBuffer] = []
    private var heldFrames = 0
    private var leadFrames = 0
    /// Buffers handed to the node and not yet played.
    private var queued = 0
    private var playing = false
    private var finishing = false
    /// A byte left over from a piece of odd length: the first half of the next sample.
    private var carry: UInt8?
    /// Moves on at every stop, so a buffer that finishes playing after it is not counted.
    private var generation = 0
    private var started: (@MainActor () -> Void)?
    private var ended: (@MainActor () -> Void)?
    private var observers: [NSObjectProtocol] = []
    private static let log = Logger(subsystem: "persona", category: "meeting-speaker")

    func start(rate: Int, started: @escaping @MainActor () -> Void, ended: @escaping @MainActor () -> Void) throws {
        stop()
        guard rate > 0, let own = AVAudioFormat(commonFormat: .pcmFormatFloat32, sampleRate: Double(rate), channels: 1, interleaved: false) else {
            throw MeetingVoicePlayerError.badRate(rate)
        }
        let capture = RecordingCapture.shared
        let format: AVAudioFormat
        if let recorder = capture.answerPlayer, capture.engineRunning {
            node = recorder
            format = RecordingCapture.callFormat
            onRecorder = true
        } else {
            let engine = AVAudioEngine()
            let node = AVAudioPlayerNode()
            engine.attach(node)
            engine.connect(node, to: engine.mainMixerNode, format: own)
            engine.prepare()
            try engine.start()
            self.engine = engine
            self.node = node
            format = own
        }
        self.format = format
        self.rate = rate
        self.started = started
        self.ended = ended
        leadFrames = max(1, Int(format.sampleRate * Self.lead))
        observe(self.engine)
    }

    func play(_ pcm: Data) {
        guard let format, let buffer = Self.buffer(pcm, carry: &carry, rate: rate, format: format) else { return }
        if playing {
            schedule(buffer)
        } else {
            held.append(buffer)
            heldFrames += Int(buffer.frameLength)
            if heldFrames >= leadFrames { begin() }
        }
    }

    func finish() {
        finishing = true
        if !playing, !held.isEmpty { begin() }
        endIfDone()
    }

    func stop() {
        generation += 1
        for o in observers { NotificationCenter.default.removeObserver(o) }
        observers = []
        node?.stop()
        engine?.stop()
        engine = nil
        node = nil
        format = nil
        rate = 0
        onRecorder = false
        held = []
        heldFrames = 0
        queued = 0
        playing = false
        finishing = false
        carry = nil
        started = nil
        ended = nil
    }

    /// The lead is in (or nothing more comes): it starts, with what it held.
    private func begin() {
        guard let node, !playing else { return }
        // The recorder's engine paused meanwhile: a player can't start on it.
        if onRecorder, !RecordingCapture.shared.engineRunning { return cut("the recording pausing") }
        playing = true
        let buffers = held
        held = []
        heldFrames = 0
        for b in buffers { schedule(b) }
        node.play()
        started?()
    }

    private func schedule(_ buffer: AVAudioPCMBuffer) {
        guard let node else { return }
        queued += 1
        let generation = generation
        node.scheduleBuffer(buffer, completionCallbackType: .dataPlayedBack) { @Sendable [weak self] _ in
            Task { @MainActor in self?.played(generation) }
        }
    }

    private func played(_ generation: Int) {
        guard generation == self.generation else { return }
        queued -= 1
        endIfDone()
    }

    private func endIfDone() {
        guard finishing, held.isEmpty, queued <= 0 else { return }
        end()
    }

    /// Over: played out, or cut. `ended` once, then all of it goes.
    private func end() {
        let ended = ended
        stop()
        ended?()
    }

    /// A call or Siri takes the audio, or the output changes under the engine (it stops itself; the recorder's starts
    /// again on its own): the answer is cut, not finished later, out of place in the meeting.
    private func observe(_ engine: AVAudioEngine?) {
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: nil, queue: .main) { @Sendable [weak self] note in
            let began = (note.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt).flatMap(AVAudioSession.InterruptionType.init) == .began
            guard began else { return }
            MainActor.assumeIsolated { self?.cut("an interruption") }
        })
        if let engine {
            observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange, object: engine, queue: .main) { @Sendable [weak self] _ in
                MainActor.assumeIsolated { self?.cut("the output changed") }
            })
        } else {
            observers.append(center.addObserver(forName: .AVAudioEngineConfigurationChange, object: nil, queue: .main) { @Sendable [weak self] note in
                let changed = note.object.map { ObjectIdentifier($0 as AnyObject) }
                MainActor.assumeIsolated {
                    guard RecordingCapture.shared.ownsEngine(changed) else { return }
                    self?.cut("the output changed")
                }
            })
        }
    }

    private func cut(_ why: String) {
        guard node != nil else { return }
        Self.log.notice("meeting: the answer's voice was cut by \(why, privacy: .public)")
        end()
    }

    /// `pcm` at `rate` (PCM16, little-endian, after the odd byte `carry` held from the piece before) as a float buffer in
    /// `format`, resampled when that is another rate (the recorder's player is at `RecordingCapture.callFormat`).
    nonisolated static func buffer(_ pcm: Data, carry: inout UInt8?, rate: Int, format: AVAudioFormat) -> AVAudioPCMBuffer? {
        let to = Int(format.sampleRate)
        guard rate > 0, rate != to else { return buffer(pcm, carry: &carry, format: format) }
        var bytes = Data()
        if let c = carry { bytes.append(c) }
        bytes.append(pcm)
        carry = bytes.count % 2 == 1 ? bytes.removeLast() : nil
        var whole: UInt8?
        return buffer(VoicePCM.resample(bytes, from: rate, to: to), carry: &whole, format: format)
    }

    /// `pcm` (PCM16, little-endian, after the odd byte `carry` held from the piece before) as a float buffer in `format`.
    nonisolated static func buffer(_ pcm: Data, carry: inout UInt8?, format: AVAudioFormat) -> AVAudioPCMBuffer? {
        var bytes = [UInt8]()
        bytes.reserveCapacity(pcm.count + 1)
        if let c = carry { bytes.append(c) }
        bytes.append(contentsOf: pcm)
        carry = bytes.count % 2 == 1 ? bytes.removeLast() : nil
        let frames = bytes.count / 2
        guard frames > 0, let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: AVAudioFrameCount(frames)),
              let out = buffer.floatChannelData?[0] else { return nil }
        buffer.frameLength = AVAudioFrameCount(frames)
        for i in 0..<frames {
            let sample = Int16(bitPattern: UInt16(bytes[2 * i]) | UInt16(bytes[2 * i + 1]) << 8)
            out[i] = Float(sample) / 32768
        }
        return buffer
    }
}

enum MeetingVoicePlayerError: LocalizedError {
    case badRate(Int)

    var errorDescription: String? {
        switch self {
        case .badRate(let rate): "No audio format at \(rate) Hz"
        }
    }
}
#endif
