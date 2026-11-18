#if os(iOS)
import SwiftUI

// After the meeting, in one screen (#8731 V3, docs/design/DESIGN_SYSTEM.md "After the meeting"): the meeting's facts, its
// summary card, its to-dos (the minutes' own action card), its map and, under them, the minutes, the transcript, the
// photos and the conversation by name. `MeetingPage`'s after mode lays them out; these are the screen's own parts.

/// The parts under the summary, by name. Photos only where the meeting has them, Conversation where the room or a
/// follow-up has one: the composer at the bottom writes to it.
nonisolated enum MeetingAfterPart: String, CaseIterable, Hashable, Sendable {
    case minutes, transcript, photos, conversation

    var title: String {
        switch self {
        case .minutes: String(localized: "Minutes [Meeting After]")
        case .transcript: String(localized: "Transcript [Meeting After]")
        case .photos: String(localized: "Photos [Meeting After]")
        case .conversation: String(localized: "Conversation [Meeting After]")
        }
    }

    /// The parts a meeting shows (pure, for the tests).
    static func shown(photos: Bool, conversation: Bool) -> [MeetingAfterPart] {
        allCases.filter { ($0 != .photos || photos) && ($0 != .conversation || conversation) }
    }
}

/// The meeting's name, then when it ran, how long, who was in it and where it was transcribed.
struct MeetingAfterHeader: View {
    var entry: RecordingEntry

    /// Persona answered in the meeting at some point: invited from the start, or later.
    static func personaWasIn(_ entry: RecordingEntry) -> Bool {
        guard let a = entry.attendance else { return false }
        return a.sentRole.answers || a.presence.contains(where: \.answers) || a.roles.contains { $0.attendance.answers }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(entry.displayTitle).font(Theme.Font.title).lineLimit(Theme.Recording.pageTitleLines)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("recording-wrapup-title")
            Text(facts).font(Theme.Font.secondary).foregroundStyle(.secondary)
                .accessibilityIdentifier("meeting-after-facts")
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var facts: String {
        let minutes = max(1, Int((entry.duration / 60).rounded()))
        let people = Self.personaWasIn(entry)
            ? ListFormatter.localizedString(byJoining: [String(localized: "You [Meeting People]"), "Persona"])
            : String(localized: "You [Meeting People]")
        let parts = [
            entry.createdAt.formatted(.dateTime.month().day()) + " " + MeetingPage.span(entry),
            String(localized: "\(minutes) min [Meeting After]"),
            people,
            entry.attendance?.transcription == .hub ? String(localized: "Cloud Transcription [Meeting After]") : nil,
        ]
        return parts.compactMap { $0 }.joined(separator: " · ")
    }
}

/// The summary card: the minutes' headline and core conclusions (or first points) and how many decisions, to-dos and
/// open questions they hold; while the minutes are on their way, where they are (`waiting`), in the card's place, so the
/// card comes in where the line was and nothing moves under it.
struct MeetingSummaryCard<Waiting: View>: View {
    var summary: MeetingAfterSummary?
    @ViewBuilder var waiting: Waiting

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Label { Text("Summary [Meeting After]") } icon: { ThemedIcon(Theme.Icon.persona) }
                .font(Theme.Font.secondaryStrong).foregroundStyle(Theme.accent)
            if let summary {
                if let headline = summary.headline {
                    Text(headline).font(Theme.Font.bodyStrong).fixedSize(horizontal: false, vertical: true)
                }
                ForEach(Array(summary.lines.enumerated()), id: \.offset) { _, line in
                    Label { Text(line).fixedSize(horizontal: false, vertical: true) } icon: { Text(verbatim: "•") }
                        .font(Theme.Font.body)
                }
                HStack(alignment: .top, spacing: Theme.Space.l) {
                    count(summary.decisions, String(localized: "Decisions [Meeting After]"))
                    count(summary.todos, String(localized: "To-Dos [Meeting After]"))
                    count(summary.open, String(localized: "Open [Meeting After]"))
                }
                .padding(.top, Theme.Space.xs)
            } else {
                waiting
            }
        }
        .padding(Theme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("meeting-after-summary")
    }

    private func count(_ n: Int, _ label: String) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(n.formatted()).font(Theme.Font.heading).monospacedDigit()
            Text(label).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
        }
        .accessibilityElement(children: .combine)
    }
}

/// The meeting map: its topics as the meeting room's Map page draws them (the first few here), and Full Screen for the
/// whole map in that page's view.
struct MeetingMapCard: View {
    var topics: [MeetingTopic]
    var fullScreen: () -> Void

    static let shownHere = 5

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            HStack(alignment: .firstTextBaseline) {
                Text("Meeting Map [Meeting After]").font(Theme.Font.secondaryStrong).foregroundStyle(.secondary)
                Spacer(minLength: Theme.Space.s)
                Button(action: fullScreen) {
                    Label("Full Screen [Meeting Map]", systemImage: Theme.Icon.disclosure).labelStyle(.titleOnly)
                        .font(Theme.Font.secondaryStrong).lineLimit(1)
                }
                .buttonStyle(.plain).foregroundStyle(Theme.accent)
                .accessibilityIdentifier("meeting-after-map-full")
            }
            MeetingTopicMapRows(topics: Array(topics.prefix(Self.shownHere)))
            if topics.count > Self.shownHere {
                Text("\(topics.count - Self.shownHere) more topics [Meeting Map]").font(Theme.Font.caption).foregroundStyle(.secondary)
            }
        }
        .padding(Theme.Space.m)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("meeting-after-map")
    }
}

/// The meeting's photos, in the order they were taken; a tap opens one full screen.
struct MeetingAfterPhotos: View {
    var recording: UUID

    @State private var viewing: MeetingPhotoItem?
    private var album: MeetingPhotos.Album { MeetingPhotos.shared.album(recording) }

    var body: some View {
        let items = album.items.values.sorted { ($0.at ?? 0, $0.photoID) < ($1.at ?? 0, $1.photoID) }
        Group {
            if items.isEmpty {
                Text(album.loaded ? String(localized: "No photos in this meeting.") : String(localized: "Loading photos…"))
                    .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
            } else {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: Theme.Recording.photoThumbSide), spacing: Theme.Space.s)],
                          alignment: .leading, spacing: Theme.Space.s) {
                    ForEach(items) { item in
                        Button { viewing = item } label: {
                            MeetingPhotoThumb(recording: recording, photoID: item.photoID, n: item.n, ready: item.hasImage)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(Text([String(localized: "Photo"), item.at.map { RecordingFormat.clock($0) }]
                            .compactMap { $0 }.joined(separator: ", ")))
                        .accessibilityAddTraits(.isImage)
                        .accessibilityIdentifier("meeting-after-photo")
                    }
                }
            }
        }
        .task(id: recording) {
            if !album.loaded { _ = await MeetingPhotos.shared.refresh(recording) }
        }
        .fullScreenCover(item: $viewing) { item in MeetingPhotoViewer(recording: recording, item: item) }
    }
}
#endif
