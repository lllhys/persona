#if os(iOS)
import AVFoundation
import Foundation
import os
import Speech

/// The urgent call's voice, one turn at a time (docs/assistant/urgent-call.md "Flow"): Persona speaks, then listens.
/// It speaks in the hub's speech voice when an extension offers one (`MeetingVoiceRoutes`, the meeting answers' way),
/// else in the phone's own voice; it listens with the phone's own recognizer, on the device when it can, so the
/// owner's words leave the phone only as the text of their answer. It plays and records on the call's audio session,
/// which LiveCommunicationKit activated (`.playAndRecord`, `.voiceChat`): it never sets a category of its own.
/// What the call's turns speak and hear with: `UrgentCallVoice` on a call, a written-out one in the tests.
@MainActor protocol UrgentCallSpeech: AnyObject {
    /// Says `text` and returns once it was said, or as soon as `stop` cut it (while its speech was fetched or played).
    func say(_ text: String) async
    /// What the owner said once they stopped for a moment; nil when nothing was heard.
    func listen() async -> String?
    /// Stops whatever it is saying or hearing: a `say` or `listen` in progress returns.
    func stop()
    /// Stops hearing only (the call muted): a `listen` in progress returns, what is being said goes on.
    func stopListening()
    /// Whether it can hear at all (the microphone and speech recognition allowed); false: the call works by taps.
    var canHear: Bool { get }
}

@MainActor final class UrgentCallVoice: UrgentCallSpeech {
    /// The hub's speech voice saying `text` in `lang`, streamed; nil when no extension offers one (the phone's voice
    /// says it then).
    typealias Speech = @MainActor (_ text: String, _ lang: String) async throws -> VoiceSpeech?

    private static let log = Logger(subsystem: "persona", category: "urgent-call")
    private let device: any MeetingDeviceSpeech
    private let makePlayer: @MainActor () -> any MeetingVoicePlayback
    /// The tests' speech; nil: the hub's (`MeetingVoiceRoutes`).
    private let fetch: Speech?
    private var player: (any MeetingVoicePlayback)?
    private var route: MeetingVoiceRoute?
    private var routeRead = false
    private var hearing: Hearing?
    /// The line being said: its speech fetched, then played (`stop` cancels it).
    private var speaking: Task<Void, Never>?
    /// The hub's speech being fed to the player.
    private var feeding: Task<Void, Never>?
    /// Returns the `say` in progress: its line was said, or cut.
    private var sayOver: (@MainActor () -> Void)?
    /// Ends the wait for the player or the phone's voice to finish the line: neither calls back once stopped.
    private var playOver: (@MainActor () -> Void)?
    /// Moves on at every line, so an older one's end does not end the next.
    private var line = 0

    /// The language Persona speaks and hears in: the app's.
    let language: String

    init(language: String = LanguageStore.shared.launched, device: any MeetingDeviceSpeech = MeetingDeviceVoice(),
         player: @escaping @MainActor () -> any MeetingVoicePlayback = { MeetingVoicePlayer() }, speech: Speech? = nil) {
        self.language = language
        self.device = device
        makePlayer = player
        fetch = speech
    }

    /// Says `text` and returns once it was said, or at once when `stop` cuts it: while the hub's speech is still being
    /// fetched, played, or the phone's voice says it.
    func say(_ text: String) async {
        // One line at a time: one still on (its turn was replaced by a tap) is cut.
        if sayOver != nil { cutSpeech() }
        line += 1
        let line = line
        await withCheckedContinuation { (done: CheckedContinuation<Void, Never>) in
            var resumed = false
            let over: @MainActor () -> Void = {
                guard !resumed else { return }
                resumed = true
                done.resume()
            }
            sayOver = over
            // Main-actor isolation said, not inherited through the continuation's body (Swift 6.2 resolves that
            // call to another overload than 6.4).
            speaking = Task { @MainActor [weak self] in
                await self?.speakOut(text)
                over()
            }
        }
        if self.line == line {
            sayOver = nil
            speaking = nil
        }
    }

    /// The line in the hub's speech voice when an extension offers one, else (or when it could not) the phone's.
    private func speakOut(_ text: String) async {
        let lang = Self.speechLanguage(language)
        do {
            if let speech = try await hubSpeech(text, lang: lang) {
                guard !Task.isCancelled else { return }
                if await play(speech) { return }
            }
        } catch {
            guard !Task.isCancelled else { return }
            Self.log.notice("urgent call: the speech voice did not speak: \(error.localizedDescription, privacy: .public)")
        }
        guard !Task.isCancelled else { return }
        let voice = MeetingSpeaker.voice(lang: nil, text: text, fallback: lang)
        await withCheckedContinuation { (done: CheckedContinuation<Void, Never>) in
            var resumed = false
            let over: @MainActor () -> Void = {
                guard !resumed else { return }
                resumed = true
                done.resume()
            }
            playOver = over
            device.speak(text, voice: voice) { over() }
        }
    }

    private func hubSpeech(_ text: String, lang: String) async throws -> VoiceSpeech? {
        if let fetch { return try await fetch(text, lang) }
        if !routeRead {
            routeRead = true
            route = await MeetingVoiceRoutes.shared.route()
        }
        guard let route, let speech = route.speech, let api = route.api else { return nil }
        return try await api.speechSpeak(route.extensionID, contribution: speech, text: text, lang: lang)
    }

    /// Plays the hub's speech; false when it could not, so the phone's voice says the line instead. Cut by `stop`: true.
    private func play(_ speech: VoiceSpeech) async -> Bool {
        let player = makePlayer()
        self.player = player
        return await withCheckedContinuation { (done: CheckedContinuation<Bool, Never>) in
            var resumed = false
            let over: @MainActor (Bool) -> Void = { ok in
                guard !resumed else { return }
                resumed = true
                done.resume(returning: ok)
            }
            playOver = { over(true) }
            do {
                try player.start(rate: speech.rate, started: {}, ended: { over(true) })
            } catch {
                over(false)
                return
            }
            feeding = Task { @MainActor in
                do {
                    for try await chunk in speech.audio {
                        guard !Task.isCancelled else { return }
                        player.play(chunk)
                    }
                    if !Task.isCancelled { player.finish() }
                } catch {
                    guard !Task.isCancelled else { return }
                    player.stop()
                    over(false)
                }
            }
        }
    }

    /// The line being said stops: its fetch, its feed and its playing, and its `say` returns.
    private func cutSpeech() {
        speaking?.cancel()
        speaking = nil
        feeding?.cancel()
        feeding = nil
        device.stop()
        player?.stop()
        player = nil
        let playing = playOver, saying = sayOver
        playOver = nil
        sayOver = nil
        playing?()
        saying?()
    }

    func listen() async -> String? { await hear(quiet: 1.6, longest: 20) }

    var canHear: Bool { Self.allowed }

    /// What the owner said, once they stopped for `quiet` seconds; nil when nothing was heard (or listening is not allowed).
    func hear(quiet: TimeInterval, longest: TimeInterval) async -> String? {
        guard Self.allowed, let recognizer = SFSpeechRecognizer(locale: Locale(identifier: Self.speechLanguage(language))),
              recognizer.isAvailable else { return nil }
        let hearing = Hearing()
        self.hearing = hearing
        defer { self.hearing = nil }
        do {
            try hearing.start(recognizer)
        } catch {
            Self.log.notice("urgent call: could not listen: \(error.localizedDescription, privacy: .public)")
            return nil
        }
        let began = Date.now
        while !Task.isCancelled {
            try? await Task.sleep(for: .milliseconds(200))
            let (text, changed) = hearing.heard
            let now = Date.now
            if !text.isEmpty, now.timeIntervalSince(changed) >= quiet { break }
            if text.isEmpty, now.timeIntervalSince(began) >= 8 { break } // nothing at all for a while
            if now.timeIntervalSince(began) >= longest || hearing.over { break }
        }
        hearing.stop()
        let text = hearing.heard.text.trimmingCharacters(in: .whitespacesAndNewlines)
        return text.isEmpty ? nil : text
    }

    /// Stops whatever it is saying or hearing (the call ended, or went on hold).
    func stop() {
        cutSpeech()
        hearing?.stop()
    }

    /// Stops hearing (the call muted): what Persona says goes on.
    func stopListening() {
        hearing?.stop()
    }

    /// Whether the owner allowed the microphone and speech recognition (asked when urgent calls are turned on: on a
    /// locked phone mid-call the prompt cannot show).
    static var allowed: Bool {
        SFSpeechRecognizer.authorizationStatus() == .authorized && AVAudioApplication.shared.recordPermission == .granted
    }

    /// Asks for both, as turning urgent calls on does; true when both are allowed.
    static func requestAccess() async -> Bool {
        let speech = await withCheckedContinuation { (done: CheckedContinuation<Bool, Never>) in
            // Called on a queue of the system's: a closure of this main-actor class would be the main actor's and trap.
            SFSpeechRecognizer.requestAuthorization { @Sendable status in done.resume(returning: status == .authorized) }
        }
        let mic = await AVAudioApplication.requestRecordPermission()
        return speech && mic
    }

    /// The BCP 47 language for the app's language: what the speech recognizer and voices take.
    nonisolated static func speechLanguage(_ app: String) -> String {
        let language = DictationLanguage.language(of: app) // "zh-Hans", "ja", "en"
        if language.hasPrefix("zh") { return "zh-CN" }
        return language == "ja" ? "ja-JP" : "en-US"
    }
}

/// One turn of listening: the recognizer's request fed from the microphone, and the best words so far.
@MainActor private final class Hearing {
    private let engine = AVAudioEngine()
    private var task: SFSpeechRecognitionTask?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private(set) var heard: (text: String, changed: Date) = ("", .now)
    private(set) var over = false

    func start(_ recognizer: SFSpeechRecognizer) throws {
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        if recognizer.supportsOnDeviceRecognition { request.requiresOnDeviceRecognition = true }
        self.request = request
        let input = engine.inputNode
        nonisolated(unsafe) let feed = request
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { @Sendable buffer, _ in
            feed.append(buffer)
        }
        engine.prepare()
        try engine.start()
        task = recognizer.recognitionTask(with: request) { @Sendable [weak self] result, error in
            let text = result?.bestTranscription.formattedString
            let final = result?.isFinal == true || error != nil
            Task { @MainActor in
                guard let self else { return }
                if let text, text != self.heard.text { self.heard = (text, .now) }
                if final { self.over = true }
            }
        }
    }

    func stop() {
        // Over: the turn returns what it heard so far at its next look.
        over = true
        if engine.isRunning {
            engine.stop()
            engine.inputNode.removeTap(onBus: 0)
        }
        request?.endAudio()
        task?.cancel()
        task = nil
        request = nil
    }
}
#endif
