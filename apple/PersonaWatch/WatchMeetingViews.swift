import SwiftUI

/// Recordings and meetings on the watch's home list (docs/apple/recording.md「Apple Watch」). **Recordings** is a place,
/// as the iPhone drawer's row (#5493, #5517): it opens the recording page, whose Record starts, and starts nothing itself;
/// while a recording runs it reads "Recording… Open" and opens it. While all-day recording runs, **Mark Meeting** comes
/// first: one microphone, so the meeting is marked in the day (docs/assistant/day-recap.md). Then the meetings' minutes,
/// once the phone relays some.
struct WatchMeetingEntries: View {
    @Environment(WatchModel.self) private var model
    @State private var capture = RecordingCapture.shared
    @State private var allDay = WatchAllDay.shared

    var body: some View {
        if capture.allDay {
            Button { allDay.markMeeting() } label: {
                VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
                    Label("Mark Meeting", systemImage: Theme.Icon.meetingMark).labelStyle(.watchRow)
                        .font(Theme.Font.secondaryMedium).lineLimit(1)
                    if allDay.feedback == .meetingMarked {
                        Text("Meeting marked").font(Theme.Font.caption).foregroundStyle(.secondary)
                    }
                }
            }
            .watchCard()
            .accessibilityIdentifier("watch-meeting-mark")
        }
        // The identifier stays what it was when the row was Meeting Recording.
        NavigationLink { WatchRecordingView() } label: { WatchRecordingsLabel().font(Theme.Font.secondaryMedium) }
            .watchCard()
            .accessibilityIdentifier("watch-meeting-recording")
        if !model.minutes.meetings.isEmpty {
            NavigationLink { WatchMinutesList() } label: {
                Label("Meeting Minutes", systemImage: Theme.Icon.minutes).labelStyle(.watchRow).font(Theme.Font.secondaryMedium).lineLimit(1)
            }
            .watchCard()
            .accessibilityIdentifier("watch-meeting-minutes")
        }
    }
}

/// The recording page where a row opens it (the home list, the unpaired page): Recordings, or "Recording… Open" while a
/// recording other than all-day's runs (the day has its own card and page). Reads only the capture's state, never its
/// clock, so the row isn't redrawn every second.
struct WatchRecordingsLabel: View {
    @State private var capture = RecordingCapture.shared

    var body: some View {
        Label(capture.state == .idle || capture.allDay ? String(localized: "Recordings") : String(localized: "Recording… Open"),
              systemImage: Theme.Icon.recording)
            .labelStyle(.watchRow).lineLimit(1)
    }
}

/// The meetings the phone relayed, newest first: each opens its short minutes.
struct WatchMinutesList: View {
    @Environment(WatchModel.self) private var model

    var body: some View {
        List {
            ForEach(model.minutes.meetings) { meeting in
                NavigationLink { WatchMinutesView(meeting: meeting) } label: {
                    VStack(alignment: .leading, spacing: Theme.Watch.lineSpacing) {
                        Text(meeting.title).font(Theme.Font.secondaryMedium).lineLimit(2)
                        Text("\(meeting.date.formatted(date: .abbreviated, time: .shortened)) · \(WatchMinutesView.stateText(meeting.state))")
                            .font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }
                .watchCard()
            }
        }
        .navigationTitle("Meeting Minutes")
    }
}

/// One meeting's short minutes: key points, decisions and action items, a few of each; "Writing the minutes…" until the
/// hub has them. The full minutes, with the transcript, are on the phone.
struct WatchMinutesView: View {
    var meeting: WatchMinutes.Meeting

    static func stateText(_ state: WatchMinutes.Meeting.State) -> String {
        switch state {
        case .writing: String(localized: "Writing the minutes…")
        case .ready: String(localized: "Minutes ready")
        case .failed: String(localized: "The minutes couldn't be written.")
        }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.m) {
                Text(meeting.title).font(Theme.Font.heading).lineLimit(3)
                Text(meeting.date.formatted(date: .abbreviated, time: .shortened)).font(Theme.Font.caption).foregroundStyle(.secondary)
                switch meeting.state {
                case .writing:
                    HStack(spacing: Theme.Space.s) {
                        // The watch's spinner is as wide as the screen's half unless held to an icon's size.
                        ProgressView().frame(width: Theme.Size.iconS, height: Theme.Size.iconS)
                        Text(Self.stateText(.writing)).font(Theme.Font.secondary)
                    }
                    .accessibilityIdentifier("watch-minutes-writing")
                case .failed:
                    Text(Self.stateText(.failed)).font(Theme.Font.secondary).foregroundStyle(Theme.caution)
                case .ready:
                    if meeting.points.isEmpty, meeting.decisions.isEmpty, meeting.actions.isEmpty {
                        Text("Nothing to note: no points, decisions or action items came up.").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                    section(String(localized: "Key Points"), Theme.Icon.keyPoint, meeting.points)
                    section(String(localized: "Decisions"), Theme.Icon.decision, meeting.decisions)
                    section(String(localized: "Action Items"), Theme.Icon.todo, meeting.actions)
                    Text("The full minutes and the transcript are on your iPhone.").font(Theme.Font.caption).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Theme.Space.xs)
        }
        .navigationTitle("Minutes")
    }

    @ViewBuilder private func section(_ heading: String, _ symbol: String, _ lines: [String]) -> some View {
        if !lines.isEmpty {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Label(heading, systemImage: symbol).font(Theme.Font.secondaryMedium).foregroundStyle(Theme.accent)
                ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                    Text(line).font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
                }
            }
            .accessibilityElement(children: .contain)
        }
    }
}
