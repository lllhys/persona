#if DEBUG && os(iOS)
import Foundation

/// The made-up tape `-showMeetingReplay sample` plays (#9417): a 30-second product chat in two voices, the hub's captions
/// and lines as they would come, the map's topics and the highlights following it, and its minutes. Its audio is
/// apple/PersonaTests/Fixtures/meeting-tape/audio.m4a (macOS `say`, the same words at the same times), and
/// apple/PersonaTests/Fixtures/meeting-tape/tape.json is this tape as JSON (`MeetingTapeTests` keeps the two equal).
/// Made up: no real people, meetings or recordings.
nonisolated enum MeetingTapeSample {
    /// The lines: where each is said in the audio, and its words.
    static let lines: [(start: Double, end: Double, text: String)] = [
        (0.5, 6.6, "好，我们开始。今天聊两件事：新版录音入口，还有发布时间。"),
        (7.2, 11.1, "录音入口已经合进主干了，开始录音只要一步。"),
        (11.7, 16.0, "锁屏和灵动岛也能看到时长，可以直接停止。"),
        (16.6, 20.6, "发布时间我建议定在下周三，先发测试版。"),
        (21.3, 25.6, "同意，下周三先发测试版，正式版再等一周。"),
        (26.2, 28.9, "那我周五前把发布说明写好。"),
    ]

    static let startedAt = Date(timeIntervalSince1970: 1_800_003_600)

    static var tape: MeetingTape {
        var events: [MeetingTape.Event] = [MeetingTape.Event(kind: .room, t: 1, at: 1, room: MeetingRoom(state: "ready"))]
        for (n, line) in lines.enumerated() {
            // Captions a second and two into the line (a third of its words, then two thirds), the line a second after
            // its end, as the hub's speech service gives them.
            let words = Array(line.text)
            for (share, after) in [(1.0 / 3, 1.0), (2.0 / 3, 2.2)] {
                let t = line.start + after
                events.append(.interim(MeetingInterim(start: line.start, text: String(words.prefix(Int(Double(words.count) * share)))),
                                       t: t, at: t))
            }
            let t = line.end + 1
            events.append(.line(MeetingHubLine(n: n, start: line.start, end: line.end, text: line.text, source: "hub"), t: t, at: t))
        }
        let entry = MeetingTopic(title: "录音入口", state: "current", at: 0.5, note: "一步开始录音，锁屏可停止")
        let release = MeetingTopic(title: "发布时间", state: "open", at: nil, note: nil)
        let notes = MeetingTopic(title: "发布说明", state: "current", at: 26.2, note: "周五前写好")
        let first = MeetingHighlight(text: "录音入口已合进主干，开始录音只要一步", at: 7.2, end: 11.1)
        let second = MeetingHighlight(text: "下周三先发测试版，正式版晚一周", at: 21.3, end: 25.6)
        events += [
            MeetingTape.Event(kind: .topics, t: 10.5, at: 10.5, topics: [entry, release]),
            MeetingTape.Event(kind: .highlights, t: 13, at: 13, highlights: [first]),
            MeetingTape.Event(kind: .topics, t: 22.5, at: 22.5, topics: [
                MeetingTopic(title: "录音入口", state: "done", at: 0.5, note: "一步开始录音，锁屏可停止"),
                MeetingTopic(title: "发布时间", state: "current", at: 16.6, note: "下周三先发测试版"),
            ]),
            MeetingTape.Event(kind: .highlights, t: 27, at: 27, highlights: [first, second]),
            MeetingTape.Event(kind: .topics, t: 30.5, at: 30.5, topics: [
                MeetingTopic(title: "录音入口", state: "done", at: 0.5, note: "一步开始录音，锁屏可停止"),
                MeetingTopic(title: "发布时间", state: "done", at: 16.6, note: "下周三先发测试版"),
                notes,
            ]),
        ]
        let minutes = RecordingMinutes(
            title: "产品周会",
            points: [.init(text: "新版录音入口已合进主干，开始录音只要一步", at: 7.2),
                     .init(text: "锁屏和灵动岛能看到时长，也能直接停止", at: 11.7)],
            decisions: [.init(text: "下周三先发测试版，正式版再等一周", at: 21.3)],
            actions: [.init(text: "写好发布说明", at: 26.2, by: "周五")])
        return MeetingTape(title: "产品周会", finalTitle: "产品周会", startedAt: startedAt, audio: "audio.m4a", locale: "zh-CN",
                           transcription: "hub", service: "volc", role: "listener", endedAfter: 32, duration: 31.5,
                           events: events, minutes: minutes, minutesState: "ready", minutesAfter: 50, phoneLines: nil)
    }
}
#endif
