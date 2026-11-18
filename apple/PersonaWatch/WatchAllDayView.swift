import SwiftUI

/// All-day recording's page (docs/assistant/day-recap.md): its state in words and a mark, the time it has recorded
/// since it last started, one tap to continue when it is paused or stopped, and pause, bookmark, meeting mark, erase
/// and stop while it records. Before the user agreed on the iPhone (docs/assistant/day-recap.md「Retention and
/// privacy」) it explains the mode in short and points there: the consent sheet is the phone's, never the watch's.
struct WatchAllDayView: View {
    @State private var allDay = WatchAllDay.shared
    @State private var capture = RecordingCapture.shared
    /// The phone's `recordingDay`: absent from a phone that doesn't take day recordings, 0 until its user agrees.
    @AppStorage(WatchSync.phoneDayKey) private var phoneDay: Int?
    /// Absent until the phone says; only a phone that says its hub can't write the recap shows the line.
    @AppStorage(WatchSync.phoneDayHubKey) private var phoneDayHub = true
    /// Erasing waits for its confirmation: it can't be undone, and it reaches the iPhone and Knowledge too.
    @State private var erasing = false

    var body: some View {
        // Opened from the face or a link while the phone offers nothing yet, the page points to the phone too.
        let access = allDay.access(phoneDay: phoneDay)
        let startable = access == .ready
        ScrollView {
            VStack(spacing: Theme.Space.m) {
                WatchAllDayStatus(state: allDay.state, since: allDay.since, startable: startable)
                if allDay.deletedOnPhone, allDay.state == .off {
                    Label("Today's all-day recording was deleted on your iPhone.", systemImage: Theme.Icon.delete)
                        .font(Theme.Font.caption).foregroundStyle(.secondary)
                        .accessibilityIdentifier("watch-allday-deleted")
                }
                if startable { controls } else { waiting(access) }
                WatchAllDayFeedbackLine(feedback: allDay.feedback)
                if allDay.notificationsOff, access != .hidden {
                    Label("Notifications are off for Persona, so you won't be told when recording pauses or stops.",
                          systemImage: Theme.Icon.muted)
                        .font(Theme.Font.caption).foregroundStyle(Theme.cautionText)
                        .accessibilityIdentifier("watch-allday-notifications-off")
                }
                if allDay.waiting > 0 {
                    Label(String(localized: "\(allDay.waiting) waiting for iPhone"), systemImage: Theme.Icon.phone)
                        .font(Theme.Font.caption).foregroundStyle(.secondary)
                }
                if !phoneDayHub {
                    Label("The recap needs a hub update.", systemImage: Theme.Icon.info)
                        .font(Theme.Font.caption).foregroundStyle(.secondary)
                }
                if let error = capture.error {
                    Text(error).font(Theme.Font.caption).foregroundStyle(Theme.cautionText)
                }
                WatchAllDayExplanation()
            }
            .padding(Theme.Space.m)
        }
        .navigationTitle("All Day [All-Day Recording Page]")
        .accessibilityIdentifier("watch-allday-page")
        // Offered: ask for notifications now, so the phone's start and a pause reach the wrist before a first start here.
        .task(id: access) { await allDay.askForNotifications(phoneDay: phoneDay, onPage: true) }
        .confirmationDialog("Erase the last 10 minutes?", isPresented: $erasing, titleVisibility: .visible) {
            Button("Erase", role: .destructive) { allDay.eraseRecent() }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("What was said in the last 10 minutes is deleted from this watch now, and from your iPhone and Knowledge after sync.")
        }
    }

    /// No start until the user agrees on the iPhone (or, with a phone that doesn't take day recordings, until it is
    /// updated); what needs no start stays: erasing what an open day still holds, and ending a wish to record left from
    /// before (it stopped by itself).
    @ViewBuilder private func waiting(_ access: AllDayAlertPolicy.Access) -> some View {
        WatchAllDayConsentLine(access: access)
        if allDay.dayOpen { erase }
        if allDay.state == .stopped { stop }
    }

    @ViewBuilder private var controls: some View {
        if capture.state == .starting {
            ProgressView(String(localized: "Waiting for microphone access")).font(Theme.Font.secondary)
            Button("Cancel") { capture.cancelStarting() }.lineLimit(1)
        } else {
            switch allDay.state {
            case .off:
                Button { start() } label: { Label("Start", systemImage: Theme.Icon.allDay).lineLimit(1) }
                    .buttonStyle(.borderedProminent)
                    .recordingPrimaryGesture()
                    .accessibilityIdentifier("watch-allday-start")
                if allDay.dayOpen { erase }
            case .recording:
                actions(recording: true)
                stop
            case .paused, .interrupted:
                resume
                actions(recording: false)
                stop
            case .stopped:
                // No capture runs to mark a meeting in; what it recorded can still be erased.
                resume
                if allDay.dayOpen { erase }
                stop
            }
        }
    }

    /// Icon-only, each named for VoiceOver. Pause and Bookmark only while recording: a paused day has no audio to
    /// mark; a meeting mark and erasing the last 10 minutes still make sense.
    private func actions(recording: Bool) -> some View {
        VStack(spacing: Theme.Space.s) {
            if recording {
                HStack(spacing: Theme.Space.s) {
                    Button { allDay.pause() } label: { Label("Pause", systemImage: Theme.Icon.pause).labelStyle(.iconOnly) }
                        .accessibilityIdentifier("watch-allday-pause")
                    Button { allDay.addBookmark() } label: { Label("Bookmark", systemImage: Theme.Icon.pin).labelStyle(.iconOnly) }
                        .recordingPrimaryGesture()
                }
            }
            HStack(spacing: Theme.Space.s) {
                Button { allDay.markMeeting() } label: {
                    Label("Mark Meeting", systemImage: Theme.Icon.meetingMark).labelStyle(.iconOnly)
                }
                .accessibilityIdentifier("watch-allday-meeting")
                Button { erasing = true } label: {
                    Label("Erase last 10 minutes", systemImage: Theme.Icon.delete).labelStyle(.iconOnly)
                }
                .accessibilityIdentifier("watch-allday-erase-recent")
            }
        }
    }

    private var resume: some View {
        Button { start() } label: { Label("Continue", systemImage: Theme.Icon.run).lineLimit(1) }
            .buttonStyle(.borderedProminent)
            .recordingPrimaryGesture()
            .accessibilityIdentifier("watch-allday-continue")
    }

    /// With the microphone off, on its own and so with its words.
    private var erase: some View {
        Button { erasing = true } label: { Label("Erase 10m", systemImage: Theme.Icon.delete).lineLimit(1) }
            .accessibilityIdentifier("watch-allday-erase")
    }

    private var stop: some View {
        Button { allDay.stopForToday() } label: { Label("Stop for Today", systemImage: Theme.Icon.interrupt).lineLimit(1) }
            .tint(Theme.danger)
            .accessibilityIdentifier("watch-allday-stop")
    }

    private func start() {
        allDay.requestStart()
    }
}

/// Where a start waits: the consent sheet on the iPhone, or (a phone that doesn't take day recordings, reached from
/// the face) an update of the iPhone app.
struct WatchAllDayConsentLine: View {
    var access: AllDayAlertPolicy.Access = .needsConsent

    var body: some View {
        Group {
            if access == .hidden {
                Label("To use All-Day Recording, update Persona on your iPhone.", systemImage: Theme.Icon.phone)
            } else {
                Label("Agree on your iPhone to start", systemImage: Theme.Icon.phone)
            }
        }
        .font(Theme.Font.secondaryMedium)
        .multilineTextAlignment(.center)
        .accessibilityIdentifier(access == .hidden ? "watch-allday-update-phone" : "watch-allday-consent")
    }
}

/// The short version of the iPhone's consent sheet, on the page whatever the state: what is recorded, whom to ask,
/// where the text goes and how long the audio stays.
struct WatchAllDayExplanation: View {
    var body: some View {
        Text("Records all the sound around you, other people included, so ask them first. Your iPhone turns it into text for your hub's recap of your day; the audio stays on your watch and iPhone and is deleted within 7 days.")
            .font(Theme.Font.caption).foregroundStyle(.secondary)
    }
}

/// The home list's All-Day Recording card, shown once the phone takes day recordings (or while the mode is on): one
/// tap starts or continues it; while it records, or until the user agreed on the iPhone, the tap opens its page.
/// Reads only the capture's state, never the library's entries, which change with every chunk.
struct WatchAllDayCard: View {
    @State private var allDay = WatchAllDay.shared
    /// The phone's `recordingDay`: absent from a phone that doesn't take day recordings, 0 until its user agrees.
    @AppStorage(WatchSync.phoneDayKey) private var phoneDay: Int?

    var body: some View {
        let access = allDay.access(phoneDay: phoneDay)
        if access != .hidden {
            Group {
                if access != .ready || allDay.state == .recording {
                    NavigationLink { WatchAllDayView() } label: { label(startable: access == .ready) }
                } else {
                    Button(action: start) { label(startable: true) }
                }
            }
            .watchCard()
            .accessibilityIdentifier("watch-allday")
            // Once the user agreed on the iPhone, the first look at the card asks for notifications.
            .task(id: access) { await allDay.askForNotifications(phoneDay: phoneDay, onPage: false) }
        }
    }

    private func start() {
        // The phone took its word back since this was drawn: the page says what is missing.
        if !allDay.requestStart() { allDay.pageRequested = true }
    }

    private func label(startable: Bool) -> some View {
        let state = allDay.state
        return VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
            Label(state.word(startable: startable) ?? String(localized: "All-Day Recording"), systemImage: state.symbol)
                .labelStyle(.watchRow).font(Theme.Font.secondaryMedium).foregroundStyle(state.tone).lineLimit(2)
            if state == .recording, let since = allDay.since {
                Text(timerInterval: since...Date.distantFuture, countsDown: false)
                    .font(Theme.Font.caption.monospacedDigit()).foregroundStyle(.secondary)
            } else if !startable {
                Text("Agree on your iPhone to start").font(Theme.Font.caption).foregroundStyle(.secondary)
            } else if allDay.deletedOnPhone, state == .off {
                Text("Today's all-day recording was deleted on your iPhone.").font(Theme.Font.caption).foregroundStyle(.secondary)
            }
        }
    }
}

/// The recording page while all-day recording runs: its state, a meeting mark (the page's Record can't start a second
/// recording), and the way to its page.
struct WatchAllDaySummary: View {
    @State private var allDay = WatchAllDay.shared

    var body: some View {
        VStack(spacing: Theme.Space.m) {
            WatchAllDayStatus(state: allDay.state, since: allDay.since)
            Button { allDay.markMeeting() } label: {
                Label("Mark Meeting", systemImage: Theme.Icon.meetingMark).lineLimit(1)
            }
            .buttonStyle(.bordered)
            .accessibilityIdentifier("watch-recording-mark-meeting")
            WatchAllDayFeedbackLine(feedback: allDay.feedback)
            NavigationLink { WatchAllDayView() } label: {
                Label("All-Day Recording", systemImage: Theme.Icon.allDay).lineLimit(1)
            }
        }
    }
}

/// The state's words and mark (never its color alone), and while recording the time since it last started, counted
/// by the system rather than redrawn by the app.
struct WatchAllDayStatus: View {
    let state: AllDaySnapshot.State
    let since: Date?
    /// A tap would start or continue it; false while a start waits for the user to agree on the iPhone.
    var startable = true

    var body: some View {
        VStack(spacing: Theme.Space.xs) {
            if let word = state.word(startable: startable) {
                Label(word, systemImage: state.symbol)
                    .font(Theme.Font.secondaryMedium).foregroundStyle(state.tone).multilineTextAlignment(.center)
            }
            if state == .recording, let since {
                Text(timerInterval: since...Date.distantFuture, countsDown: false)
                    .font(Theme.Font.display.monospacedDigit()).foregroundStyle(Theme.danger)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("watch-allday-status")
    }
}

/// The line confirming the tap just made; it goes after a few seconds.
struct WatchAllDayFeedbackLine: View {
    let feedback: WatchAllDay.Feedback?

    var body: some View {
        if let feedback {
            Label(feedback.text, systemImage: feedback.symbol)
                .font(Theme.Font.caption)
                .foregroundStyle(feedback.failed ? Theme.cautionText : .secondary)
        }
    }
}

extension AllDaySnapshot.State {
    /// The state in words on the watch's page, card and Record page; nil when off, where the control says it. The
    /// complication says the same in fewer words (`AllDaySnapshot.Face.title` and `hint`). `startable` false: a tap
    /// can't continue it yet (the iPhone's consent is missing), so a stopped day doesn't say it would.
    func word(startable: Bool = true) -> String? {
        switch self {
        case .off: nil
        case .recording: String(localized: "Recording all day")
        case .paused: String(localized: "Paused")
        case .interrupted: String(localized: "Paused — tap to continue")
        case .stopped: startable ? String(localized: "Stopped — tap to continue") : String(localized: "Stopped")
        }
    }

    var symbol: String {
        switch self {
        case .off, .recording: Theme.Icon.allDay
        case .paused, .interrupted: Theme.Icon.allDayPaused
        case .stopped: Theme.Icon.error
        }
    }

    /// Red while recording, as the clock of any recording; a pause the user chose is quiet, one the system made (or a
    /// stop) asks to be looked at.
    var tone: Color {
        switch self {
        case .off: .primary
        case .recording: Theme.danger
        case .paused: .secondary
        case .interrupted, .stopped: Theme.cautionText
        }
    }
}
