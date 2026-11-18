#if os(iOS)
import SwiftUI

// The Meetings home (#8731 v2, docs/design/DESIGN_SYSTEM.md "The Meetings home"): today's meetings from this phone's
// calendar, each one tap from starting, and the quick start's title, people and transcription lines. The page and the
// quick start (`RecordingCaptureControls`) lay them out; Start Meeting stays the page's one prominent button.

/// Today's meetings on now or still to come: one row each, whose Start begins the meeting named after it (the start's
/// options as picked on the quick start). Without calendar access, one row that asks for it; with access and nothing
/// left today, nothing.
struct MeetingUpcomingSection: View {
    var form: RecordingStartForm

    var body: some View {
        if !form.upcoming.isEmpty {
            Section {
                ForEach(form.upcoming, id: \.self) { event in
                    MeetingUpcomingRow(event: event, now: form.upcomingAt) { form.queued = event }
                }
            } header: {
                Text("Upcoming Meetings")
            }
        } else if form.calendarAskable {
            Section {
                Button {
                    Task {
                        _ = await MeetingCalendar.requestAccess()
                        form.calendarAskable = MeetingCalendar.canAsk
                        await form.readCalendar()
                    }
                } label: {
                    Label { Text("Name After Calendar Meetings").lineLimit(1) } icon: { ThemedIcon(Theme.Icon.calendarEvent) }
                }
                .accessibilityIdentifier("meeting-calendar-ask")
            }
        }
    }
}

/// One of today's meetings: when (red while it is on), its name, how many were invited and where, and Start.
struct MeetingUpcomingRow: View {
    var event: MeetingCalendar.Candidate
    var now: Date
    var start: () -> Void

    private var on: Bool { event.start.addingTimeInterval(-MeetingCalendar.lead) <= now }

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(event.start, format: .dateTime.hour().minute()).font(Theme.Font.captionStrong)
                    .foregroundStyle(on ? Palette.recordingRed : Color.primary)
                Text(event.end, format: .dateTime.hour().minute()).font(Theme.Font.caption).foregroundStyle(.secondary)
            }
            .monospacedDigit()
            .fixedSize()
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(event.title).font(Theme.Font.bodyStrong).lineLimit(1)
                Text(detail).font(Theme.Font.caption).foregroundStyle(on ? Palette.recordingRed : Color.secondary).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: start) {
                // Icon and word drawn as one line (a Label in a list row's glass button showed its icon alone), white
                // on the prominent one as the page's Start Meeting is.
                HStack(spacing: Theme.Space.xs) {
                    ThemedIcon(Theme.Icon.recording)
                    Text("Start [Upcoming Meeting]").lineLimit(1)
                }
                .font(Theme.Font.secondaryStrong)
                .foregroundStyle(on ? Theme.onSolid : Theme.accent)
            }
            .themedButton(prominent: on)
            .fixedSize()
            .accessibilityIdentifier("meeting-upcoming-start")
        }
        .accessibilityElement(children: .combine)
        .accessibilityAction(named: Text("Start [Upcoming Meeting]"), start)
        .accessibilityIdentifier("meeting-upcoming")
    }

    /// "On now · 8 people · Room 3": what is known, each part once.
    private var detail: String {
        var parts: [String] = []
        if on { parts.append(String(localized: "On Now [Upcoming Meeting]")) }
        if !event.attendees.isEmpty { parts.append(String(localized: "\(event.attendees.count) people")) }
        if let place = event.location { parts.append(place) }
        return parts.joined(separator: " · ")
    }
}

/// Who is in the next meeting (#8731): you, Persona once invited (answering on screen; the meeting room's speaker makes
/// it speak), and Invite. Persona's menu removes it again.
struct MeetingPeopleRow: View {
    @Binding var invited: Bool

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.l) {
            person(String(localized: "You [Meeting People]")) {
                Monogram(name: String(localized: "You [Meeting People]"), size: Theme.Size.avatar, fill: Theme.Fill.chip)
            }
            if invited {
                Menu {
                    Button(role: .destructive) { withAnimation(Theme.Motion.standard) { invited = false } } label: {
                        Label("Remove from Meeting", systemImage: Theme.Icon.close)
                    }
                } label: {
                    person("Persona", detail: String(localized: "Answers on Screen")) {
                        AssistantAvatar(size: Theme.Size.avatar, still: true)
                    }
                }
                .accessibilityIdentifier("meeting-persona-invited")
            } else {
                Button { Haptics.tap(); withAnimation(Theme.Motion.standard) { invited = true } } label: {
                    person(String(localized: "Invite Persona"), tint: Theme.accent) {
                        Circle().strokeBorder(Theme.accent, style: StrokeStyle(lineWidth: Theme.Size.rule, dash: [Theme.Space.xs]))
                            .frame(width: Theme.Size.avatar, height: Theme.Size.avatar)
                            .overlay { ThemedIcon(Theme.Icon.invite).foregroundStyle(Theme.accent) }
                    }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("meeting-invite-persona")
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, Theme.Space.xs)
    }

    private func person(_ name: String, detail: String? = nil, tint: Color = .primary, @ViewBuilder face: () -> some View) -> some View {
        VStack(spacing: Theme.Space.xs) {
            face()
            Text(name).font(Theme.Font.caption).foregroundStyle(tint).lineLimit(1)
            if let detail { Text(detail).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1) }
        }
        .fixedSize()
        .accessibilityElement(children: .combine)
    }
}

/// The next meeting's options, in the Meetings page's ⋯ (#8731 v2): the private mark (a room meeting's), the language,
/// and the microphone while a Bluetooth one is on the route. Only while nothing records: they are for the next start.
struct MeetingOptionsMenu: View {
    var form: RecordingStartForm
    @Environment(AppModel.self) private var model
    @State private var capture = RecordingCapture.shared
    @StoredValue(RecordingDefaults.localeKey) private var locale = RecordingDefaults.locale
    @StoredValue(RecordingInput.storageKey) private var inputRaw = RecordingInput.phone.rawValue

    var body: some View {
        if capture.state == .idle {
            Menu {
                if form.pickedKind == .meeting, MeetingAttendees.conversation(model) {
                    Toggle(isOn: Bindable(form).pickedPrivate) {
                        Text("Private Meeting")
                        Text("Its minutes are never used when Persona answers in later meetings.")
                    }
                    .accessibilityIdentifier("meeting-private-start")
                }
                Picker(selection: $locale) {
                    Text("Chinese").tag("zh-CN")
                    Text("English").tag("en-US")
                    Text("Japanese").tag("ja-JP")
                } label: { Label("Language", systemImage: Theme.Icon.language) }
                .pickerStyle(.menu)
                if form.hasBluetooth {
                    Picker(selection: $inputRaw) {
                        ForEach(RecordingInput.allCases, id: \.rawValue) { Text($0.title).tag($0.rawValue) }
                    } label: { Label("Microphone", systemImage: Theme.Icon.dictate) }
                    .pickerStyle(.menu)
                }
            } label: {
                ThemedIcon(Theme.Icon.moreMenu)
            }
            .accessibilityLabel(Text("Meeting Options"))
            .accessibilityIdentifier("meeting-options")
        }
    }
}

/// Who transcribes the next meeting, where it can be seen before it starts (#8731): this iPhone, or the hub's speech
/// service by name. A menu switches it when the hub can transcribe now; the hub's default that can't run now says so
/// in the warning tint and runs on this iPhone.
struct MeetingTranscriptionLine: View {
    /// The mode it starts in (nil: the hub names none, which is this iPhone).
    var mode: MeetingTranscriptionMode?
    /// The hub's default is the hub, which can't transcribe now.
    var hubDown: Bool
    /// The speech service chosen on the hub, by name (nil: the built-in one).
    var service: String?
    /// The hub can transcribe now: the line is a menu.
    var offersHub: Bool
    var pick: (MeetingTranscriptionMode) -> Void

    var body: some View {
        if offersHub {
            Menu {
                ForEach(MeetingTranscriptionMode.allCases, id: \.self) { m in
                    Button { pick(m) } label: {
                        if m == (mode ?? .device) { Label(Self.name(m, service: service), systemImage: Theme.Icon.done) } else { Text(Self.name(m, service: service)) }
                    }
                }
            } label: { line }
            .accessibilityIdentifier("meeting-transcription-line")
        } else {
            line.accessibilityIdentifier("meeting-transcription-line")
        }
    }

    private var line: some View {
        let device = (mode ?? .device) == .device
        return HStack(spacing: Theme.Space.s) {
            ThemedIcon(device ? Theme.Icon.phone : Theme.Icon.meetingTranscription)
                .foregroundStyle(hubDown ? Theme.cautionText : Color.secondary)
            Text("Transcription [Meeting Transcription]").foregroundStyle(.secondary)
            Text(hubDown ? String(localized: "This iPhone · the hub can't transcribe now") : Self.name(mode ?? .device, service: service))
                .font(Theme.Font.secondaryStrong)
                .foregroundStyle(hubDown ? Theme.cautionText : Color.primary)
                .lineLimit(1)
                .minimumScaleFactor(0.8)
            Spacer(minLength: 0)
            if offersHub { ThemedIcon(Theme.Icon.choices).font(Theme.Font.captionStrong).foregroundStyle(.tertiary) }
        }
        .font(Theme.Font.secondary)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
    }

    /// "This iPhone", or "Cloud · <service>" (the built-in one: "Cloud · Persona").
    static func name(_ mode: MeetingTranscriptionMode, service: String?) -> String {
        switch mode {
        case .device: String(localized: "This iPhone [Meeting Transcription]")
        case .hub: String(localized: "Cloud · \(service ?? "Persona")")
        }
    }
}
#endif
