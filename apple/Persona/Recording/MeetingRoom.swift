#if os(iOS)
import SwiftUI

// The meeting room (#8731 v2 B1, docs/design/DESIGN_SYSTEM.md "The meeting room"): the live meeting page full screen and dark,
// whoever is in it at the top, three pages to swipe (live, highlights, map) and a meeting app's round controls. The
// page's content and rules stay `MeetingPage`'s and `MeetingPageBar`'s; these are the room's own parts.

/// Who is in the meeting: you, Persona while it answers (how: on screen or out loud, lit while it speaks), and Invite
/// while it isn't in. Inviting makes Persona answer on screen; the bar's speaker makes it speak, and the bar's Remove
/// takes it out again (back to the room's notes, #8851).
struct MeetingRoomPeople: View {
    var entry: RecordingEntry
    var paused: Bool

    @State private var speaker = MeetingSpeaker.shared

    private var role: MeetingAttendance { entry.attendance?.sentRole ?? .off }

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.l) {
            person(String(localized: "You [Meeting People]"), ring: !paused && speaker.speaking == nil) {
                Monogram(name: String(localized: "You [Meeting People]"), size: Theme.Size.avatar, fill: Theme.Fill.chip)
            } detail: { EmptyView() }
            if role.answers {
                person("Persona", ring: speaker.speaking != nil) {
                    AssistantAvatar(size: Theme.Size.avatar, still: true)
                } detail: {
                    Text(role.speaks ? String(localized: "Answers Out Loud") : String(localized: "Answers on Screen"))
                        .font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
                .accessibilityIdentifier("meeting-room-persona")
            } else if entry.roomMeeting {
                Button {
                    Haptics.tap()
                    _ = MeetingRecorder.shared.setRole(.listener, recordingID: entry.id.uuidString)
                } label: {
                    person(String(localized: "Invite Persona"), tint: Theme.accent) {
                        Circle().strokeBorder(Theme.accent, style: StrokeStyle(lineWidth: Theme.Size.rule, dash: [Theme.Space.xs]))
                            .frame(width: Theme.Size.avatar, height: Theme.Size.avatar)
                            .overlay { ThemedIcon(Theme.Icon.invite).foregroundStyle(Theme.accent) }
                    } detail: { EmptyView() }
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("meeting-room-invite")
            }
            Spacer(minLength: 0)
        }
    }

    /// One person: the face (a green ring while they speak: you while the room records and Persona is quiet, Persona
    /// while it says an answer), the name, and what they do under it.
    private func person(_ name: String, ring: Bool = false, tint: Color = .primary, @ViewBuilder face: () -> some View,
                        @ViewBuilder detail: () -> some View) -> some View {
        VStack(spacing: Theme.Space.xs) {
            face()
                .overlay {
                    if ring { Circle().strokeBorder(Theme.positive, lineWidth: Theme.Size.rule).padding(-Theme.Space.xs) }
                }
            Text(name).font(Theme.Font.caption).foregroundStyle(tint).lineLimit(1)
            detail()
        }
        .fixedSize()
        .accessibilityElement(children: .combine)
    }
}

/// The room's three pages by name; the pages swipe too.
struct MeetingRoomTabs: View {
    @Binding var page: MeetingRoomPage

    var body: some View {
        MeetingTabRow(pages: MeetingRoomPage.allCases, page: $page, title: \.title) { "meeting-room-tab-\($0.rawValue)" }
    }
}

/// A row of a page's parts by name, the picked one on a chip (the meeting room's pages, #8731 B1; after the meeting, V3).
struct MeetingTabRow<Page: Hashable>: View {
    var pages: [Page]
    @Binding var page: Page
    var title: (Page) -> String
    var identifier: (Page) -> String

    var body: some View {
        HStack(spacing: Theme.Space.xs) {
            ForEach(pages, id: \.self) { p in
                let on = p == page
                Button {
                    withAnimation(Theme.Motion.standard) { page = p }
                } label: {
                    Text(title(p)).font(on ? Theme.Font.secondaryStrong : Theme.Font.secondaryMedium)
                        .foregroundStyle(on ? Color.primary : Color.secondary)
                        .lineLimit(1)
                        .padding(.horizontal, Theme.Space.m)
                        .padding(.vertical, Theme.Space.xs)
                        .background(on ? Theme.Fill.chip : Color.clear, in: .capsule)
                        .contentShape(.capsule)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
                .accessibilityIdentifier(identifier(p))
            }
        }
        .frame(maxWidth: .infinity)
    }
}

nonisolated enum MeetingRoomPage: String, CaseIterable, Hashable, Sendable {
    case live, highlights, map

    var title: String {
        switch self {
        case .live: String(localized: "Live [Meeting Room]")
        case .highlights: String(localized: "Highlights [Meeting Room]")
        case .map: String(localized: "Map [Meeting Room]")
        }
    }
}

/// The meeting so far as a map: its topics in order, the one on now lit, those still to come dimmed, each topic's note
/// under it. Empty until the room set any. The meeting room's Map page, and after the meeting its map full screen (V3).
struct MeetingTopicMap: View {
    var title: String
    var topics: [MeetingTopic]

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                if topics.isEmpty {
                    Text("The map grows as the meeting moves through its topics.")
                        .font(Theme.Font.secondary).foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                } else {
                    Text(title).font(Theme.Font.heading)
                    MeetingTopicMapRows(topics: topics)
                }
            }
            .padding(Theme.Space.page)
            .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .padding(.horizontal, Theme.Space.page)
            .padding(.vertical, Theme.Space.s)
        }
        .accessibilityIdentifier("meeting-room-map")
    }
}

/// The map's topics, each on its line with its note under it: the meeting room's Map page and the after-meeting card draw
/// them alike.
struct MeetingTopicMapRows: View {
    var topics: [MeetingTopic]

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            ForEach(Array(topics.enumerated()), id: \.offset) { _, topic in
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    HStack(spacing: Theme.Space.s) {
                        Text(topic.title).font(Theme.Font.bodyStrong)
                            .foregroundStyle(topic.known == .open ? Color.secondary : topic.known == .current ? Theme.positive : Color.primary)
                        if topic.known == .current {
                            Text("On Now [Meeting Topic]").font(Theme.Font.label).foregroundStyle(Theme.positive)
                        }
                    }
                    if let note = topic.note, !note.isEmpty {
                        Text(note).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    }
                }
                .padding(.leading, Theme.Space.m)
                .overlay(alignment: .leading) { Capsule().fill(Theme.accent.opacity(0.5)).frame(width: Theme.Size.rule) }
                .accessibilityElement(children: .combine)
            }
        }
    }
}

/// A round control of the room's bar: the symbol in a disc, its word under it; the end one red.
struct MeetingRoomButton: View {
    var title: String
    var symbol: String
    var on = false
    var ends = false
    var action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: Theme.Space.xs) {
                ThemedIcon(symbol).font(Theme.Font.heading)
                    .foregroundStyle(ends ? Theme.onSolid : on ? Theme.groupedPage : Color.primary)
                    .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                    .background(ends ? Theme.danger : on ? Color.primary : Theme.Fill.chip, in: .circle)
                Text(title).font(Theme.Font.label).lineLimit(1).minimumScaleFactor(0.8)
                    .foregroundStyle(ends ? Theme.danger : on ? Color.primary : Color.secondary)
            }
            .frame(maxWidth: .infinity)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }
}
#endif
