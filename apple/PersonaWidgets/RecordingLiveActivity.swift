import ActivityKit
import SwiftUI
import WidgetKit

/// Draws the recording activity (docs/apple/live-activity.md「Recording」), a meeting's as a meeting app's (#8731 V4): on the
/// Lock Screen its name with the waveform and the time, what it is (a meeting, bookmarks, paused by a call; with the live
/// view Persona's part and the current topic with its place), the third line (the latest line in quotes, or Persona's
/// answer in its box) and Bookmark · Ask Persona · End; in the Dynamic Island a red dot, the waveform and the time, opened
/// up the same. The time is the system's (`Text(timerInterval:)`), so a recording that runs on needs no updates.
/// Colors are fixed (`Palette`): the extension builds neither the app's Theme nor its style.
struct RecordingLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: RecordingActivityAttributes.self) { context in
            // A stale running recording (the app stopped confirming it: iOS ended the app, #9108) is drawn stopped.
            RecordingLockScreen(state: context.state.drawn(stale: context.isStale), id: context.attributes.recordingID)
                // The lock screen is cut at about 160 pt: larger text would push the buttons off it.
                .dynamicTypeSize(...DynamicTypeSize.large)
                .activitySystemActionForegroundColor(Palette.recordingRed)
                .environment(\.locale, LanguageStore.drawingLocale)
                .widgetURL(RecordingLink.page)
        } dynamicIsland: { context in
            let state = context.state.drawn(stale: context.isStale)
            let id = context.attributes.recordingID
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    RecordingIslandMark(state: state, size: IslandMetrics.expandedFace)
                        .padding(.leading, IslandMetrics.edge)
                }
                // The time in the corner, wide enough for "1:23:45": beside a title in the center region it was cut to "…".
                DynamicIslandExpandedRegion(.trailing) {
                    RecordingTime(state: state)
                        .font(.title3.weight(.semibold))
                        .foregroundStyle(state.phase == .recording ? Palette.recordingRed : .white)
                        .frame(minWidth: IslandMetrics.recordingExpandedTimerWidth, alignment: .trailing)
                        .padding(.trailing, IslandMetrics.edge)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(alignment: .leading, spacing: IslandMetrics.rowGap) {
                        VStack(alignment: .leading, spacing: IslandMetrics.smallGap) {
                            Text(state.title).font(.headline).lineLimit(1)
                            RecordingSubtitle(state: state).font(.caption.weight(.medium)).foregroundStyle(.white.opacity(0.8)).lineLimit(1)
                        }
                        .padding(.horizontal, IslandMetrics.gap)
                        RecordingAnswer(state: state).foregroundStyle(.white.opacity(0.85)).padding(.horizontal, IslandMetrics.gap)
                        RecordingButtons(id: id, onDark: true, meeting: state.meeting, attends: state.persona != nil,
                                         recording: state.phase == .recording, talking: state.talking, stopped: state.phase == .stopped)
                            .padding(.horizontal, IslandMetrics.recordingIslandButtonsInset)
                    }
                    .environment(\.locale, LanguageStore.drawingLocale)
                }
            } compactLeading: {
                IslandWidth { _ in
                    RecordingIslandMark(state: state, size: IslandMetrics.face)
                        .padding(.leading, IslandMetrics.edge)
                }
            } compactTrailing: {
                IslandWidth { limited in
                    if !limited {
                        RecordingTime(state: state)
                            .font(.system(size: IslandMetrics.timerNumber, weight: .semibold))
                            .foregroundStyle(state.phase == .recording ? Palette.recordingRed : .white)
                            // `Text(timerInterval:)` takes all the width it is offered.
                            .frame(width: IslandMetrics.recordingTimerWidth, alignment: .trailing)
                            .padding(.trailing, IslandMetrics.edge)
                    }
                }
            } minimal: {
                RecordingMark(phase: state.phase, size: IslandMetrics.limitedContent, talking: state.talking == true)
            }
            .widgetURL(RecordingLink.page)
            .keylineTint(Palette.recordingRed)
        }
        .supplementalActivityFamilies([.small])
    }
}

/// The recording's mark: a red disc with a waveform while it records, a pause sign while it doesn't, a stop sign once it
/// stopped without the app (`stopped`), and the talk mark while a talk window is open (#3862 phase 4; still, no repeating
/// animation: they don't run in Live Activities).
struct RecordingMark: View {
    var phase: RecordingActivityAttributes.ContentState.Phase
    var size: CGFloat
    var talking = false

    private var symbol: String {
        guard phase == .recording else { return phase == .stopped ? RecordingSymbol.stop : RecordingSymbol.paused }
        return talking ? RecordingSymbol.talking : RecordingSymbol.recording
    }

    var body: some View {
        ZStack {
            Circle().fill(Palette.recordingRed.opacity(phase == .recording ? 1 : 0.6))
            Image(systemName: symbol)
                .font(.system(size: size * 0.48, weight: .bold))
                .foregroundStyle(.white)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// The island's mark (#8731 V4, as a call's): a red dot and the waveform while it records (the talk mark while a talk
/// window is open), the pause or stop sign in gray while it doesn't. Still: no repeating animation runs in Live Activities.
struct RecordingIslandMark: View {
    var state: RecordingActivityAttributes.ContentState
    var size: CGFloat

    var body: some View {
        HStack(spacing: IslandMetrics.gap) {
            if state.phase == .recording {
                Circle().fill(Palette.recordingRed).frame(width: IslandMetrics.dot, height: IslandMetrics.dot)
            }
            RecordingWave(state: state).font(.system(size: size * 0.6, weight: .bold))
        }
        .frame(height: size)
        .accessibilityHidden(true)
    }
}

/// The waveform: green while it records (the talk mark while a talk window is open), gray paused; the stop sign once it
/// stopped without the app.
struct RecordingWave: View {
    var state: RecordingActivityAttributes.ContentState

    private var symbol: String {
        switch state.phase {
        case .recording: state.talking == true ? RecordingSymbol.talking : RecordingSymbol.recording
        case .paused, .interrupted: RecordingSymbol.paused
        case .stopped: RecordingSymbol.stop
        }
    }

    var body: some View {
        Image(systemName: symbol)
            .foregroundStyle(state.phase == .recording ? Palette.green : Color.gray)
            .accessibilityHidden(true)
    }
}

/// The recorded time: counted by the system while recording, standing still while paused, and where the app last
/// confirmed it once it stopped without the app.
struct RecordingTime: View {
    var state: RecordingActivityAttributes.ContentState

    var body: some View {
        Group {
            if state.phase == .recording, let since = state.runningSinceDate {
                Text(timerInterval: since...Date.distantFuture, countsDown: false, showsHours: true)
            } else {
                Text(Duration.seconds(Int(state.elapsed)),
                     format: .time(pattern: state.elapsed >= 3600 ? .hourMinuteSecond : .minuteSecond))
            }
        }
        .monospacedDigit()
        .multilineTextAlignment(.trailing)
        .lineLimit(1)
    }
}

/// Under the name: a meeting, its bookmarks; or why it isn't recording.
struct RecordingSubtitle: View {
    var state: RecordingActivityAttributes.ContentState

    /// The role and the current topic (#3862 phase 2): while it records, Persona attends and the room has named one. On
    /// the Lock Screen they take up to two lines, so the third line takes one (`RecordingAnswer.lockLines`).
    static func showsTopic(_ state: RecordingActivityAttributes.ContentState) -> Bool {
        state.phase == .recording && state.persona != nil && state.topic?.isEmpty == false
    }

    /// "Topic 2 / 4: <topic>" (#8731 V4), or without its place where the state has none. The place comes first, so a long
    /// topic cut at the end of its lines keeps it.
    static func topicText(_ topic: String, _ state: RecordingActivityAttributes.ContentState) -> Text {
        if let number = state.topicNumber, let count = state.topicCount, count > 1 {
            return Text("Topic \(number) / \(count): \(topic) [Meeting Topic]")
        }
        return Text("Topic: \(topic) [Meeting Topic]")
    }

    var body: some View {
        switch state.phase {
        case .interrupted: Text("Paused by a call · Open Persona to continue")
        case .paused: Text("Paused")
        case .stopped: Text("Stopped · Open Persona to see the recording")
        case .recording:
            if let persona = state.persona {
                // Persona attends (#2981): said in place of what it is, so whoever sees the phone knows (the bookmarks
                // stay on the recording's page). With the live view (#3862 phase 2) the current topic follows it, so a long
                // one is what gets cut (on the Lock Screen after a second line, in the island after the first), never the
                // role.
                if let topic = state.topic, !topic.isEmpty {
                    Text("\(RecordingRole.text(persona)) · \(Self.topicText(topic, state))")
                } else {
                    RecordingRole(persona: persona)
                }
            } else if state.marks > 0 {
                Text(state.meeting ? "Meeting · \(state.marks) bookmarks" : "Recording · \(state.marks) bookmarks")
            } else {
                Text(state.meeting ? "Meeting recording" : "Recording on iPhone")
            }
        }
    }
}

/// Bookmark, Ask Persona (or Talk) and End (#8731 V4): neutral and red, white words on the red, each with its words.
/// - A meeting's End is a link that opens the meeting and asks "End the Meeting?" (`RecordingLink.end`). An ended meeting
///   can't go on recording, so a tap on the Lock Screen never ends it at once. Any other recording's Stop stops it there
///   (as a voice memo's).
/// - The three share the row equally, so none is cut beside Talk's words.
/// - Ask Persona comes only while Persona answers in the meeting (#2981). A room meeting with Talk (#3862 phase 4,
///   `talking` not nil) has Talk in its place: a tap opens a talk window, and while it's open the button reads Done and
///   ends it (each tap says which it was, `TalkInMeetingIntent.open`).
/// - Taking Persona out is the meeting room's Remove, not a button here.
struct RecordingButtons: View {
    var id: String
    var onDark = false
    /// A meeting: End asks in the app.
    var meeting = false
    /// Persona answers in the meeting: Ask Persona (or Talk) while it records.
    var attends = false
    var recording = true
    /// The meeting's Talk: nil without it, true while a talk window is open.
    var talking: Bool? = nil
    /// Stopped without the app (`ContentState.Phase.stopped`): End or Stop alone, whose tap opens the app, which ends the
    /// activity (nothing records any more).
    var stopped = false

    var body: some View {
        HStack(spacing: IslandMetrics.buttonGap) {
            if !stopped {
                Button(intent: AddRecordingBookmarkIntent(recordingID: id)) { label("Bookmark", RecordingSymbol.bookmark) }
                    .buttonStyle(.bordered)
                    .tint(onDark ? .white : .primary)
                if attends && recording { ask }
            }
            end
        }
        .buttonBorderShape(.capsule)
    }

    @ViewBuilder private var ask: some View {
        Group {
            if let talking {
                // What the button shows goes with the tap: a stale Done can't open a window.
                Button(intent: TalkInMeetingIntent(recordingID: id, open: !talking)) {
                    if talking { label("Done [Talk Window]", RecordingSymbol.talking) } else { label("Talk [Meeting Talk]", RecordingSymbol.talk) }
                }
            } else {
                Button(intent: AskPersonaInMeetingIntent(recordingID: id)) { label("Ask Persona", RecordingSymbol.ask) }
            }
        }
        .buttonStyle(.bordered)
        .tint(onDark ? .white : .primary)
    }

    @ViewBuilder private var end: some View {
        if meeting {
            Link(destination: RecordingLink.end(id)) {
                label("End [Meeting Room]", RecordingSymbol.stop)
                    .foregroundStyle(.white)
                    .frame(minHeight: IslandMetrics.recordingButtonHeight)
                    .background(Palette.recordingButton, in: .capsule)
            }
            .accessibilityIdentifier("recording-activity-end")
        } else {
            Button(intent: StopRecordingIntent(recordingID: id)) { label("Stop", RecordingSymbol.stop) }
                .buttonStyle(.borderedProminent)
                .tint(Palette.recordingButton)
                .foregroundStyle(.white)
        }
    }

    private func label(_ text: LocalizedStringKey, _ symbol: String) -> some View {
        Label(text, systemImage: symbol)
            .font(.subheadline.weight(.semibold))
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .frame(maxWidth: .infinity)
    }
}

/// "Persona taking part" / "Persona listening": the role, said wherever Persona attends.
struct RecordingRole: View {
    var persona: String

    var body: some View { Self.text(persona) }

    static func text(_ persona: String) -> Text { Text(persona == "participant" ? "Persona taking part" : "Persona listening") }
}

/// The third line, in a meeting Persona attends: its latest answer (the hub posts no notification for answers; with the
/// live view only for 60 s after it came, `RecordingActivity.content`), else the meeting's latest line (#3862 phase 2),
/// or, while the recording is paused and the subtitle says why, that Persona still attends. Two lines at most, and one
/// on the Lock Screen beside a topic (`lockLines`), so the Lock Screen keeps within its height (docs/apple/live-activity.md
/// 「Pitfalls」). Never Just me: it isn't in the state.
struct RecordingAnswer: View {
    var state: RecordingActivityAttributes.ContentState
    /// The most lines the answer or the line takes.
    var lines = 2

    /// Its lines on the Lock Screen: one while the subtitle carries the current topic (#3862 phase 2), which may take two
    /// lines: two lines each passed the ~160 pt. Not the subtitle cut to one line instead: beside the role (never cut)
    /// it left a character or two of a Chinese topic. Else two, as before the live view (#2981).
    static func lockLines(_ state: RecordingActivityAttributes.ContentState) -> Int {
        RecordingSubtitle.showsTopic(state) ? 1 : 2
    }

    var body: some View {
        // Stopped without the app (#9108): no answer and no role, which would read as still attending.
        if let persona = state.persona, state.phase != .stopped {
            if state.talking == true, state.phase == .recording {
                // A talk window is open (#3862 phase 4): what Persona hears now, in place of the answer or the line.
                Label { Text("Listening to you…").lineLimit(1) } icon: { Image(systemName: RecordingSymbol.talking) }
                    .font(.caption.weight(.medium))
            } else if let answer = state.answer, !answer.isEmpty {
                // Persona's answer in its box (#8731 V4), told from the meeting's own lines at a glance.
                Label { Text(answer).lineLimit(lines) } icon: { Image(systemName: RecordingSymbol.persona).foregroundStyle(Palette.purple) }
                    .font(.caption.weight(.medium))
                    .padding(IslandMetrics.recordingAnswerInset)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Palette.purple.opacity(0.18),
                                in: RoundedRectangle(cornerRadius: IslandMetrics.recordingAnswerRadius, style: .continuous))
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(Text("Persona: \(answer)"))
            } else if state.phase != .recording {
                Label { RecordingRole(persona: persona).lineLimit(1) } icon: { Image(systemName: RecordingSymbol.persona) }
                    .font(.caption.weight(.medium))
            } else if let line = state.line, !line.isEmpty {
                if state.fellBack == true {
                    // Hub transcription fell back (#3862 phase 5): the line is this iPhone's own, and says so in words.
                    Label { Text("On iPhone: \(line) [Recording Line]").lineLimit(lines) } icon: { Image(systemName: RecordingSymbol.phone) }
                        .font(.caption.weight(.medium))
                } else {
                    // The latest line, in quotes (#8731 V4).
                    Text("“\(line)” [Recording Line]").lineLimit(lines)
                        .font(.caption.weight(.medium))
                }
            }
        }
    }
}

/// The Lock Screen: mark, name and what it is, the time; under them the buttons. The watch's Smart Stack (`.small`) gets
/// a glance: mark, name, time, no buttons (they wouldn't fit).
struct RecordingLockScreen: View {
    var state: RecordingActivityAttributes.ContentState
    var id: String
    @Environment(\.activityFamily) private var family

    var body: some View {
        if family == .small { glance } else { full }
    }

    private var glance: some View {
        HStack(spacing: IslandMetrics.rowGap) {
            RecordingMark(phase: state.phase, size: IslandMetrics.face)
            VStack(alignment: .leading, spacing: IslandMetrics.smallGap) {
                Text(state.title).font(.footnote.weight(.semibold)).lineLimit(1)
                RecordingTime(state: state).font(.footnote).foregroundStyle(state.phase == .recording ? Palette.recordingRed : .secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(IslandMetrics.rowGap)
    }

    /// The name with the waveform and the time, what it is, the third line, the buttons (#8731 V4).
    private var full: some View {
        VStack(alignment: .leading, spacing: IslandMetrics.rowGap) {
            HStack(alignment: .firstTextBaseline, spacing: IslandMetrics.gap) {
                Text(state.title).font(.headline).lineLimit(1)
                Spacer(minLength: IslandMetrics.rowGap)
                RecordingWave(state: state).font(.subheadline.weight(.bold))
                RecordingTime(state: state)
                    .font(.title3.weight(.semibold))
                    .foregroundStyle(state.phase == .recording ? Palette.recordingRed : .secondary)
                    // It takes all the width it is offered: fixed, the name keeps the rest of the row.
                    .frame(width: IslandMetrics.recordingExpandedTimerWidth, alignment: .trailing)
            }
            RecordingSubtitle(state: state).font(.caption.weight(.medium)).foregroundStyle(.secondary).lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            RecordingAnswer(state: state, lines: RecordingAnswer.lockLines(state)).foregroundStyle(.secondary)
            RecordingButtons(id: id, meeting: state.meeting, attends: state.persona != nil, recording: state.phase == .recording,
                             talking: state.talking, stopped: state.phase == .stopped)
        }
        .padding(IslandMetrics.lockPadding)
    }
}

/// The recording activity's symbols (the extension doesn't build `Theme.Icon`): the app's own for the same meanings.
enum RecordingSymbol {
    /// The app's recording symbol (`Theme.Icon.recording`): the Control's.
    static let recordingAction = "waveform.circle"
    /// On the red mark the disc is drawn already: the bare waveform.
    static let recording = "waveform"
    static let paused = "pause.fill"
    /// The app's bookmark (`Theme.Icon.pin`).
    static let bookmark = "pin"
    static let stop = "stop.fill"
    /// The app's question (`Theme.Icon.question`): Ask Persona in a meeting it attends.
    static let ask = "questionmark.bubble"
    /// The app's Persona (`Theme.Icon.persona`): its answer.
    static let persona = "sparkle"
    /// The app's iPhone (`Theme.Icon.phone`): the line is this iPhone's own, hub transcription having fallen back (#3862
    /// phase 5).
    static let phone = "iphone"
    /// The app's Talk (`Theme.Icon.talk`, #3862 phase 4): the button that opens a talk window.
    static let talk = "mic.circle"
    /// The talk mark: on the red disc while a talk window is open, and its Done (the bare microphone, as the disc is drawn).
    static let talking = "mic.fill"
}
