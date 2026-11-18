#if os(iOS)
import Foundation

// The pure rules of Talk and the meeting call (#3862 phase 4, docs/assistant/meeting-minutes.md「Phase 4: realtime voice in
// the meeting」; the phone's side docs/apple/recording.md「Realtime voice (phase 4)」): who has a call, when Talk is
// offered, when a call that ended starts again, the Lock Screen's talk window, the words of a span asked without a call,
// and Persona's voice kept out of the transcript. `MeetingCall` acts on them; the tests read them directly.

/// Who has a meeting call, and where Talk is.
nonisolated enum MeetingCallRule {
    /// The hub's voice extension that takes meeting calls (`meeting` in its `/info` features, protocol 2, set up), from
    /// what `VoiceSupport` last heard; nil without one (none, an older extension, one that needs setup, not asked yet).
    static func extensionID(extensions: [HubExtension]?, info: VoiceInfoState?) -> String? {
        guard let extensions, let voice = VoiceAvailability.pick(extensions), case .info(let i)? = info,
              i.protocolVersion >= 2, i.setup.isEmpty, i.features.contains(VoiceMeeting.feature) else { return nil }
        return voice.id
    }

    /// The hub said for sure that it takes no meeting calls: it lists no voice extension, or the one it runs answered
    /// `/info` without `meeting` (from before phase 4, an older protocol, not set up), or `404` / `403`. An ask that
    /// failed, or a voice extension that isn't running at that moment (restarting, an upgrade), says nothing either way:
    /// a running call stays then (its own `ready` and close decide), and the phone asks again later (`probeAgain`).
    static func declines(extensions: [HubExtension]?, info: VoiceInfoState?) -> Bool {
        guard let extensions else { return false }
        guard extensions.contains(where: { $0.kind == "voice" }) else { return true }
        switch info {
        case .info?: return extensionID(extensions: extensions, info: info) == nil
        case .missing?, .forbidden?: return true
        case .failed?, nil: return false
        }
    }

    /// How long after the `tries`-th ask (from 1) whether the hub takes meeting calls the phone asks again, while a meeting
    /// that wants a call has none (P9: never once a minute for hours with the screen locked): not again once the hub
    /// answered either way (`extensionID`, `declines`), else after 30 seconds, 2 minutes and 5 minutes, then no more for
    /// this recording. Whatever else asks meanwhile (a spoken answer's route, a chat's voice button) is heard too.
    static func probeAgain(extensions: [HubExtension]?, info: VoiceInfoState?, tries: Int) -> TimeInterval? {
        guard extensionID(extensions: extensions, info: info) == nil, !declines(extensions: extensions, info: info),
              tries >= 1, tries <= probeRetries.count else { return nil }
        return probeRetries[tries - 1]
    }

    static let probeRetries: [TimeInterval] = [30, 120, 300]

    /// Whether a Talk span goes to the live meeting call (else it is asked without one, through `…/ask`, and its audio
    /// stays on the phone). From any source only once the owner has been told what Talk sends: they saw the Wording
    /// before (`privacySeen`, `MeetingTalkPrivacy`), or it is on the meeting page under Talk as this span opens
    /// (`privacyShown`, `MeetingTalkPrivacy.shown`). The Lock Screen and the island never show it, so a span they open
    /// needs `privacySeen` whatever the caller says. A first span from a page that showed a note in its place, or before
    /// the call was up, therefore stays on the phone.
    static func toCall(live: Bool, source: MeetingTalkSource, privacySeen: Bool, privacyShown: Bool) -> Bool {
        live && (privacySeen || (privacyShown && source != .activity))
    }

    /// What a tap on the Live Activity's Talk or Done does: the action the button showed (`open`), checked against what is
    /// open now. Talk opens a window only when no span is open, Done ends whatever span is (a window, or a hold the page
    /// lost); a tap that no longer matches (a Done tapped just after the window closed itself on silence, before the Lock
    /// Screen drew Talk again) does nothing, so it can never open a window that streams the room. `open` nil (an activity
    /// drawn before the button said which): a toggle, as it was.
    static func activityTap(open: Bool?, spanOpen: Bool) -> MeetingActivityTap {
        switch open {
        case true?: spanOpen ? .ignore : .open
        case false?: spanOpen ? .close : .ignore
        case nil: spanOpen ? .close : .open
        }
    }

    /// Talk is offered on the meeting page and on the Lock Screen: a room meeting this phone records that Persona can be
    /// asked in (`MeetingAsk.canAsk`: it attends, as listener or participant, and the hub took the room). With a call it
    /// goes to the call, otherwise it is asked through `…/ask`, transcribed on the phone.
    static func talkOffered(_ entry: RecordingEntry, roomMissing: Bool) -> Bool {
        entry.active && entry.roomMeeting && MeetingAsk.canAsk(entry, roomMissing: roomMissing)
    }

    /// The meeting being recorded wants a call: a room meeting Persona takes part in (`participant`, from the start or a
    /// later change), the hub took the room, and it is `ready` with its ids. It has one when the voice extension takes
    /// meeting calls too (`extensionID`). A listener, a room not up yet or gone, an older manager or no such extension:
    /// Talk without a call.
    static func wanted(_ entry: RecordingEntry, roomMissing: Bool, room: MeetingRoom?) -> Bool {
        guard talkOffered(entry, roomMissing: roomMissing), entry.attendance?.sentRole == .participant,
              let room, room.known == .ready, room.key(group: "") != nil else { return false }
        return true
    }

    /// Where Talk sends audio to the voice service, the privacy texts say so (the contract's「Wording」): a participant room
    /// meeting on a hub whose voice extension takes meeting calls. Elsewhere the audio stays on the phone.
    static func talkSendsAudio(conversation: Bool, role: MeetingAttendance, voice: String?) -> Bool {
        conversation && role == .participant && voice != nil
    }

    /// A meeting call that ended by itself starts again `VoiceMeeting.retryEvery` later while the meeting wants one, unless
    /// the end says it never can (the hub refused this device, the extension can't take meeting calls, the model can't be
    /// used, no hub): those wait for the next meeting.
    static func retries(after end: VoiceEnd) -> Bool {
        switch end {
        case .rejected(let status): status != 401 && status != 403
        case .incompatible, .modelUnavailable, .noHub: false
        case .user, .left, .replaced: false
        default: true
        }
    }
}

/// Where a Talk span was opened (#3862 phase 4).
nonisolated enum MeetingTalkSource: Equatable, Sendable {
    /// Held on the meeting page.
    case hold
    /// A tap on the page's Talk (#9658), or VoiceOver's double tap (it can't hold): the page's talk window, ended by the
    /// next tap, its hint saying where the words go.
    case pageWindow
    /// The Live Activity's Talk, on the Lock Screen or in the island: a talk window.
    case activity
}

/// What a tap on the Live Activity's Talk / Done does (`MeetingCallRule.activityTap`).
nonisolated enum MeetingActivityTap: Equatable, Sendable { case open, close, ignore }

/// A talk window (#3862 phase 4): Talk opened by a tap rather than held. The Lock Screen's and the island's end on
/// another tap, 2 seconds of silence after speech, 20 seconds without speech, or 60 seconds in all. The meeting page's
/// (`onPage`, #9658) does not end 2 seconds after speech: Talk is in sight to tap when done, and a pause to think is
/// common in a meeting, so a quiet stretch ends it only after 20 seconds, as before any speech, or at 60 seconds. Pure:
/// the caller hands in the microphone's level and the time.
///
/// What counts as speech is the owner's voice, not the room's: the microphone hears the whole room, and people talking
/// at a normal level would otherwise keep a window open for its full 60 seconds, all of it going to the voice service.
/// So the window takes the room's level as it opens (`Room`, the tap's last seconds before the tap on Talk) and counts
/// a buffer as speech only `aboveRoom` louder than that, and never below `speechLevel`: the owner holds the phone and
/// speaks into it, others are further away. Without enough of the room heard yet (the recording just started or came
/// back from a pause), it counts `speechLevel` alone, as before.
nonisolated struct MeetingTalkWindow: Equatable, Sendable {
    nonisolated enum End: Equatable, Sendable {
        /// Another tap.
        case tapped
        /// A quiet stretch after speech: 2 seconds, or 20 on the page (`quietEnd`).
        case silence
        /// 20 seconds without speech.
        case noSpeech
        /// 60 seconds.
        case limit
    }

    /// The room's level as a window opens: the levels (dBFS) of the tap's short pieces of audio (`MeetingTalkTap.piece`)
    /// over the last `roomSpan` seconds before it, and how many seconds they cover.
    nonisolated struct Room: Equatable, Sendable {
        var levels: [Float] = []
        var seconds: Double = 0
    }

    static let silenceAfterSpeech: TimeInterval = 2
    static let noSpeechLimit: TimeInterval = 20
    static let limit: TimeInterval = 60
    /// A buffer below this level (dBFS of the 16 kHz audio) is never speech; a quiet room is well below it.
    static let speechLevel: Float = -40
    /// How far back the room's level is taken from, before the window opens.
    static let roomSpan: TimeInterval = 5
    /// The least of that the tap must have heard for the room's level to count (else `speechLevel` alone).
    static let roomHeard: TimeInterval = 2
    /// The room's level is this low percentile of its pieces' levels: the room between its louder moments. Low, so the
    /// owner's own words just before the tap (spoken into the phone, with pauses) hardly raise it.
    static let roomPercentile = 0.25
    /// How much louder than the room's level speech must be (dB): the owner speaks into the phone, others are further
    /// away, and the peaks of a room talking at a normal level stay under it. A starting value: the simulator can't
    /// measure rooms (docs/apple/recording.md「Realtime voice (phase 4)」).
    static let aboveRoom: Float = 15

    let opened: Date
    /// The level a buffer must reach to count as speech in this window.
    let speechThreshold: Float
    /// How long a quiet stretch after speech lasts before it ends the window: `silenceAfterSpeech`, or on the page
    /// `noSpeechLimit`.
    let quietEnd: TimeInterval
    /// When speech was last heard; nil before any.
    private(set) var lastSpeech: Date?
    private(set) var ended: End?

    init(opened: Date, room: Room = Room(), onPage: Bool = false) {
        self.opened = opened
        speechThreshold = Self.threshold(room)
        quietEnd = onPage ? Self.noSpeechLimit : Self.silenceAfterSpeech
    }

    var heardSpeech: Bool { lastSpeech != nil }

    /// The room's level (`roomPercentile` of what it heard), or nil when less than `roomHeard` of it was heard.
    static func roomLevel(_ room: Room) -> Float? {
        guard room.seconds >= roomHeard, !room.levels.isEmpty else { return nil }
        let sorted = room.levels.sorted()
        return sorted[Int(Double(sorted.count - 1) * roomPercentile)]
    }

    /// What counts as speech: `aboveRoom` over the room's level, never below `speechLevel`; `speechLevel` alone when the
    /// room's level isn't known.
    static func threshold(_ room: Room) -> Float {
        roomLevel(room).map { max(speechLevel, $0 + aboveRoom) } ?? speechLevel
    }

    /// The microphone's level `db` at `now`, then whether the window closes (and why). Nothing once it ended.
    mutating func heard(_ db: Float, at now: Date) -> End? {
        guard ended == nil else { return nil }
        if db >= speechThreshold { lastSpeech = now }
        return tick(now)
    }

    /// Whether the window closes by `now`, by time alone.
    mutating func tick(_ now: Date) -> End? {
        guard ended == nil else { return nil }
        let end: End? = if now.timeIntervalSince(opened) >= Self.limit {
            .limit
        } else if let lastSpeech {
            now.timeIntervalSince(lastSpeech) >= quietEnd ? .silence : nil
        } else {
            now.timeIntervalSince(opened) >= Self.noSpeechLimit ? .noSpeech : nil
        }
        ended = end
        return end
    }

    /// Another tap: it ends now.
    mutating func tap() -> End? {
        guard ended == nil else { return nil }
        ended = .tapped
        return .tapped
    }
}

/// Talk without a call (#3862 phase 4): the phone cuts its segment at the span's start and end, transcribes it on
/// device, and asks with those words (`…/ask`, `spoken: true`).
nonisolated enum MeetingTalkWords {
    /// What was said in the span: the transcript's sentences at least half inside it, in order, joined with a space;
    /// nil when it holds none with words.
    static func question(_ segments: [RecordingSegment], span: RecordingSpan) -> String? {
        let lines = segments.sorted { $0.index < $1.index }.flatMap { $0.sentences ?? [] }
            .filter { span.holds($0) }
            .map { $0.text.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        guard !lines.isEmpty else { return nil }
        let words = lines.joined(separator: " ")
        return String(String.UnicodeScalarView(words.unicodeScalars.prefix(MeetingAsk.maxQuestion)))
    }

    /// Every segment of the span has its text (or the recording has nothing more to transcribe).
    static func transcribed(_ segments: [RecordingSegment], span: RecordingSpan) -> Bool {
        !segments.contains { $0.sentences == nil && $0.start < span.end && $0.start + $0.duration > span.start }
    }
}

/// What the meeting call's voice says in a stretch it plays (#3862 phase 4 with #5905, docs/assistant/meeting-minutes.md
/// 「Persona's echo」): its answers, the voice's own captions, as `MeetingSpeaker` keeps the answer it says. A stretch
/// carries the answers not kept with an earlier one, the one being said included, whole as far as it came (a line of the
/// part said is still made of its words), in order; nil when none has words, and the stretch then keeps the time rule.
nonisolated enum MeetingCallEcho {
    static func said(_ captions: [VoiceCaption], kept: Set<String>) -> String? {
        let words = captions.filter { !$0.isUser && !kept.contains($0.id) }
            .map { $0.text.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        return words.isEmpty ? nil : words.joined(separator: " ")
    }

    /// The answers kept once a stretch closes: every final one the voice has said by then. One still being said goes on
    /// into the next stretch too; those the call no longer holds are let go.
    static func kept(_ captions: [VoiceCaption], before: Set<String>) -> Set<String> {
        before.intersection(captions.map(\.id)).union(captions.filter { !$0.isUser && $0.final }.map(\.id))
    }
}
#endif
