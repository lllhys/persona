import SwiftUI

/// The watch's Recordings page (docs/apple/recording.md「Apple Watch」). Recording does not depend on the hub connection:
/// it saves without a network and sends back once near the phone. One start, as on the iPhone (#5493, #5243): **Record**,
/// always a meeting, which the phone names after the calendar, unless Note is picked under it for that one start; two
/// start buttons looked alike and owners couldn't tell which to press. `startsMeeting`: opened by the Record complication,
/// it starts a meeting at once. While all-day recording runs that marks the meeting in the day instead (one microphone),
/// and the page shows the all-day state in place of the clock and Record.
struct WatchRecordingView: View {
    var startsMeeting = false
    @State private var capture = RecordingCapture.shared
    @State private var startedMeeting = false
    /// Meeting unless Note is picked here, for the next start only (as the iPhone's Type row): never remembered.
    @State private var kind = WatchRecordingView.firstKind
    @State private var library = RecordingLibrary.shared
    @State private var transfer = WatchRecordingTransfer.shared
    /// Starts in the app's UI language; the button cycles through the three the recorder supports.
    @State private var locale = WatchRecordingView.defaultLocale
    @State private var confirmingRolling = false
    @AppStorage("recording.rollingConsent") private var rollingConsent = false
    @AppStorage(WatchSync.phoneRollingKey) private var phoneRolling = false
    @State private var allDay = WatchAllDay.shared
    /// The phone's `recordingDay`: absent from a phone that doesn't take day recordings, 0 until its user agrees.
    @AppStorage(WatchSync.phoneDayKey) private var phoneDay: Int?

    #if DEBUG
    /// `-showWatchNote YES`: the page with Note picked (screenshots).
    private static let firstKind: RecordingKindChoice = UserDefaults.standard.bool(forKey: "showWatchNote") ? .note : .meeting
    /// `-showRecordingEnd YES`: the page scrolled to its end (screenshots: a simulator's crown can't be turned from the
    /// command line).
    private static let showsEnd = UserDefaults.standard.bool(forKey: "showRecordingEnd")
    #else
    private static let firstKind = RecordingKindChoice.meeting
    #endif

    /// The recorder's language for the app's UI language (also all-day recording's).
    static var defaultLocale: String {
        switch LanguageStore.shared.launched {
        case "en": "en-US"
        case "ja": "ja-JP"
        default: "zh-CN"
        }
    }

    var body: some View {
        ScrollView {
            VStack(spacing: Theme.Space.m) {
                // Still asking for the microphone: the clip view's wait and Cancel, whichever mode is starting.
                if capture.allDay, capture.state != .starting {
                    WatchAllDaySummary()
                } else {
                    clip
                }
                if let error = capture.error ?? transfer.error ?? library.error {
                    Text(error).font(Theme.Font.caption).foregroundStyle(Theme.caution)
                }
                Text("Once sent back to the phone it's transcribed on device; the text goes into Knowledge automatically and the audio isn't uploaded.")
                    .font(Theme.Font.caption).foregroundStyle(.secondary)
                // A day recording has its own page and summary; listed here it would wait to send all day.
                ForEach(Array(library.entries.filter { $0.source == .watch && !$0.isDay }.prefix(10))) { entry in
                    VStack(alignment: .leading, spacing: Theme.Space.xs) {
                        Text(entry.displayTitle).font(Theme.Font.secondaryMedium).lineLimit(2)
                        Text("\(entry.isRolling ? String(localized: "Rolling") + " · " : "")\(RecordingFormat.clock(entry.duration)) · \(transfer.pending(entry.id) == 0 && entry.endDelivered ? String(localized: "Sent to iPhone") : String(localized: "Waiting to send"))")
                            .font(Theme.Font.caption).foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Theme.Space.s)
                    .background(Theme.Watch.cardFill, in: .rect(cornerRadius: Theme.Radius.card))
                }
            }.padding(Theme.Space.m)
        }
        #if DEBUG
        .defaultScrollAnchor(Self.showsEnd ? .bottom : nil)
        #endif
        // The page's name, as on the home row that opens it and on the iPhone (in zh-Hans and ja the same word as Record).
        .navigationTitle("Recordings")
        .onAppear { transfer.start() }
        .task {
            // The complication's Record: one tap from the watch face; once, and never over a recording running. While
            // all-day recording runs, the same tap marks the meeting in the day.
            guard startsMeeting, !startedMeeting else { return }
            if capture.allDay {
                startedMeeting = true
                allDay.markMeeting()
                return
            }
            guard capture.state == .idle else { return }
            startedMeeting = true
            await capture.start(locale: locale, meeting: RecordingKindChoice.meeting.watchMeeting)
        }
        .confirmationDialog("Rolling recording", isPresented: $confirmingRolling) {
            Button("Start Rolling") {
                rollingConsent = true
                Task { await capture.start(locale: locale, rolling: true) }
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Keeps the last 6 hours on this watch and your iPhone. Audio stays on your devices. Ask others before recording them.")
        }
    }

    /// Every recording but all-day's: the clock, Record with the next start's settings under it (type, language) and the
    /// other modes (rolling, all-day), and the controls of the one running.
    @ViewBuilder private var clip: some View {
        Text(RecordingFormat.clock(capture.elapsed))
            .font(Theme.Font.display.monospacedDigit())
            .foregroundStyle(capture.state == .recording ? Theme.danger : .primary)
        if let id = capture.recordingID, library.entry(id)?.isMeeting == true {
            Label("Meeting", systemImage: Theme.Icon.meeting).font(Theme.Font.caption).foregroundStyle(Theme.accent)
                .accessibilityIdentifier("watch-recording-meeting")
        }
        if capture.state == .idle {
            Button(action: record) {
                Label("Record [Recordings Section ef2821]", systemImage: Theme.Icon.recording).lineLimit(1)
            }
            .buttonStyle(.borderedProminent)
            .recordingPrimaryGesture()
            .accessibilityIdentifier("watch-recording-start")
            // Meeting or Note for this start, under Record as its qualifier: a text button with the kind's mark (the
            // iPhone's Type row shows it too), never a second start button; one tap switches it.
            Button { kind = kind == .meeting ? .note : .meeting } label: {
                Label(String(localized: "Type: \(kind.title)"), systemImage: kind.symbol).font(Theme.Font.secondary).lineLimit(1)
            }
            .buttonStyle(.borderless)
            .accessibilityIdentifier("watch-recording-kind")
            if kind == .note {
                Text("No minutes, and Persona stays out.").font(Theme.Font.caption).foregroundStyle(.secondary)
                    .accessibilityIdentifier("watch-recording-kind-detail")
            }
            Button {
                let languages = ["zh-CN", "en-US", "ja-JP"]
                let index = languages.firstIndex(of: locale) ?? 0
                locale = languages[(index + 1) % languages.count]
            } label: {
                Text("Language: \(locale == "zh-CN" ? String(localized: "Chinese") : locale == "en-US" ? String(localized: "English") : String(localized: "Japanese"))")
                    .font(Theme.Font.secondary).lineLimit(1)
            }
            .buttonStyle(.bordered)
            if phoneRolling {
                Button {
                    if rollingConsent { Task { await capture.start(locale: locale, rolling: true) } }
                    else { confirmingRolling = true }
                } label: {
                    Label("Rolling", systemImage: Theme.Icon.recurring).lineLimit(1)
                }
                .buttonStyle(.bordered)
            }
            allDayEntry
        } else if capture.state == .starting {
            ProgressView(String(localized: "Waiting for microphone access")).font(Theme.Font.secondary)
            Button("Cancel") { capture.cancelStarting() }.lineLimit(1)
        } else {
            Text(recordingStatus)
                .font(Theme.Font.caption).foregroundStyle(.secondary)
            HStack(spacing: Theme.Space.s) {
                Button { capture.togglePause() } label: {
                    Label(capture.state == .paused ? String(localized: "Continue") : String(localized: "Pause"), systemImage: capture.state == .paused ? Theme.Icon.run : Theme.Icon.pause)
                        .labelStyle(.iconOnly)
                }
                Button { capture.addMark() } label: { Label("Bookmark", systemImage: Theme.Icon.pin).labelStyle(.iconOnly) }
                    .recordingPrimaryGesture()
                if capture.rolling {
                    Button { capture.eraseRecent() } label: { Label("Erase 10m", systemImage: Theme.Icon.delete).labelStyle(.iconOnly) }
                }
            }
            Button { capture.stop(); transfer.pump(force: true) } label: {
                Label("Stop & Save", systemImage: Theme.Icon.interrupt).lineLimit(1)
            }.tint(Theme.danger)
        }
    }

    /// Record: a meeting, or a note when Note is picked. The pick is for this start only.
    private func record() {
        let picked = kind
        kind = .meeting
        Task { await capture.start(locale: locale, meeting: picked.watchMeeting) }
    }

    /// All-Day Recording once the phone takes it: one tap starts it, or, until the user agreed on the iPhone, opens
    /// its page, which says so.
    @ViewBuilder private var allDayEntry: some View {
        let access = allDay.access(phoneDay: phoneDay)
        if access != .hidden {
            // Stopped by itself earlier: say so above the button that continues it.
            if allDay.state == .stopped { WatchAllDayStatus(state: .stopped, since: nil, startable: access == .ready) }
            Group {
                if access == .ready {
                    Button { allDay.requestStart() } label: { allDayLabel }
                } else {
                    NavigationLink { WatchAllDayView() } label: { allDayLabel }
                }
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("watch-recording-allday")
        }
    }

    private var allDayLabel: some View {
        Label("All-Day Recording", systemImage: Theme.Icon.allDay).lineLimit(1)
    }

    private var recordingStatus: String {
        if capture.state == .paused { return String(localized: "Paused") }
        return capture.rolling
            ? String(localized: "Rolling · keeps the last 6 hours")
            : String(localized: "Recording · keeps going with your wrist down")
    }
}

extension View {
    /// The double-tap gesture (watchOS 11) for a recording page's main action: start, continue, or a bookmark.
    @ViewBuilder func recordingPrimaryGesture() -> some View {
        if #available(watchOS 11.0, *) { handGestureShortcut(.primaryAction) } else { self }
    }
}
