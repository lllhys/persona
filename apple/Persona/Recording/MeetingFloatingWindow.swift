#if os(iOS)
import AVFoundation
import SwiftUI

// The floating meeting window (#8731 V5, docs/design/DESIGN_SYSTEM.md "The floating meeting window"): while a meeting
// records and its room is put away, a small dark card over every page of the app says the meeting goes on, for how long,
// and who speaks now; a tap brings the room back. Dragged, it settles on the nearer side; pulled past a side, it docks as
// a bar. No buttons in it (Pause and End are the room's and the Dynamic Island's). The room's full-screen cover lies over
// it, so it shows only while the room is away; it goes with the recording.

/// Over the app's root (`RootView`): the window while a meeting records.
struct MeetingFloatingWindow: View {
    @State private var capture = RecordingCapture.shared
    @State private var library = RecordingLibrary.shared

    var body: some View {
        if capture.state == .recording || capture.state == .paused, let id = capture.recordingID,
           let entry = library.entry(id), entry.isMeeting {
            MeetingFloatCard(entry: entry, paused: capture.state == .paused)
        }
    }
}

/// The card and where it sits: dragged anywhere, then on the nearer side at that height; pulled past a side, a bar.
private struct MeetingFloatCard: View {
    var entry: RecordingEntry
    var paused: Bool

    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var place = MeetingFloatPlace.start
    @GestureState private var drag: CGSize = .zero

    var body: some View {
        GeometryReader { geo in
            let width = Theme.Size.meetingFloatWidth * Theme.sizeScale(typeSize)
            let top = geo.safeAreaInsets.top + Theme.Size.tapTarget
            let bottom = max(top + 1, geo.size.height - geo.safeAreaInsets.bottom - Theme.Size.meetingFloatBottomRoom)
            if place.docked {
                MeetingFloatDock(paused: paused) { withAnimation(Theme.Motion.standard) { place.docked = false } }
                    .position(x: place.leading ? Theme.Size.meetingFloatDockWidth / 2 : geo.size.width - Theme.Size.meetingFloatDockWidth / 2,
                              y: top + (bottom - top) * place.height)
            } else {
                let center = place.center(cardWidth: width, edge: Theme.Size.meetingFloatEdge, width: geo.size.width, top: top, bottom: bottom)
                MeetingFloatContent(entry: entry, paused: paused)
                    .frame(width: width)
                    .position(x: center.x + drag.width, y: center.y + drag.height)
                    // Ahead of the card's own tap: the card follows the finger, so a drag ends on it and its button
                    // would take the lift as a tap and open the room (a held press began the button's first).
                    .highPriorityGesture(DragGesture(minimumDistance: Theme.Space.s)
                        .updating($drag) { value, state, _ in state = value.translation }
                        .onEnded { value in
                            let end = CGPoint(x: center.x + value.translation.width, y: center.y + value.translation.height)
                            withAnimation(Theme.Motion.standard) {
                                place = MeetingFloatPlace.dropped(center: end, cardWidth: width, width: geo.size.width, top: top,
                                                                  bottom: bottom, pull: Theme.Recording.floatDockPull)
                            }
                        })
                    .accessibilityAction(named: Text("Move to the Side [Meeting Float]")) {
                        withAnimation(Theme.Motion.standard) { place.docked = true }
                    }
            }
        }
        .transition(.opacity)
    }
}

/// The card: the meeting's name and time, then who speaks now with its face and a small waveform. A tap opens the room.
private struct MeetingFloatContent: View {
    var entry: RecordingEntry
    var paused: Bool

    @State private var speaker = MeetingSpeaker.shared
    @State private var call = MeetingCall.shared
    @State private var gate = GateBox()
    @State private var headset = MeetingFloatHeadset.now()

    var body: some View {
        Button { MeetingRecorder.openRecordings() } label: {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack(spacing: Theme.Space.xs) {
                    Circle().fill(paused ? Color.secondary : Theme.danger)
                        .frame(width: Theme.Size.dot, height: Theme.Size.dot)
                    Text(entry.displayTitle).font(Theme.Font.label).fontWeight(.semibold).lineLimit(1)
                    Spacer(minLength: Theme.Space.xs)
                    // The system ticks it, as on the now-recording card: the window over every page doesn't draw again
                    // four times a second, and both say the same time.
                    RecordingClock(running: !paused, font: Theme.Font.label)
                        .foregroundStyle(paused ? Color.secondary : Theme.danger)
                }
                TimelineView(.periodic(from: .now, by: Theme.Recording.floatTick)) { context in
                    let who = now(context.date)
                    MeetingFloatSpeakerRow(speaker: who, paused: paused)
                }
            }
            .padding(Theme.Space.s)
            .frame(maxWidth: .infinity, alignment: .leading)
            // A solid dark card with a hairline edge: a translucent material read gray over a light page, and a dark
            // card needs its edge over a dark one.
            .background(Theme.groupedCard, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                    .strokeBorder(Theme.Fill.chip, lineWidth: Theme.Size.hairline)
            }
            .contentShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
        }
        .buttonStyle(.plain)
        // Dark in both appearances, as the meeting room is (#8731 E1).
        .environment(\.colorScheme, .dark)
        .onReceive(NotificationCenter.default.publisher(for: AVAudioSession.routeChangeNotification)) { _ in
            headset = MeetingFloatHeadset.now()
        }
        .accessibilityHint(Text("Opens the meeting room. [Meeting Float]"))
        .accessibilityIdentifier("meeting-float")
    }

    /// Who speaks at `date`: the gate kept between looks in a box of its own, so a look doesn't redraw anything else.
    private func now(_ date: Date) -> MeetingFloatSpeaker {
        #if DEBUG
        if let forced = UserDefaults.standard.string(forKey: "showFloatSpeaker").flatMap(MeetingFloatSpeaker.init(rawValue:)) {
            return forced
        }
        #endif
        let personaSpeaking = speaker.speaking != nil || call.call(for: entry.id)?.speaking == true
        let next = MeetingFloatSpeaker.next(level: RecordingCapture.shared.inputLevel, personaSpeaking: personaSpeaking,
                                            paused: paused, headset: headset, gate: gate.value ?? MeetingFloatSpeaker.Gate(),
                                            now: date.timeIntervalSinceReferenceDate, onLevel: Theme.Recording.floatVoiceOn,
                                            offLevel: Theme.Recording.floatVoiceOff, hold: Theme.Recording.floatVoiceHold)
        gate.value = next.gate
        return next.speaker
    }

    /// The voice gate between the clock's looks: not observed, so keeping it redraws nothing. Nil until the first look,
    /// so making the box costs nothing (P3).
    private final class GateBox {
        var value: MeetingFloatSpeaker.Gate?
    }
}

/// Who speaks: their face (a ring while they speak; Persona's in its purple), the word for it and a small waveform.
private struct MeetingFloatSpeakerRow: View {
    var speaker: MeetingFloatSpeaker
    var paused: Bool

    private var tint: Color { speaker == .persona ? Theme.Hue.purple : Theme.positive }
    private var speaking: Bool { speaker == .persona || speaker == .you || speaker == .someone }

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            face
                .overlay {
                    if speaking { Circle().strokeBorder(tint, lineWidth: Theme.Size.rule).padding(-Theme.Space.xxs) }
                }
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(word).font(Theme.Font.secondaryStrong).lineLimit(1).minimumScaleFactor(0.8)
                MeetingFloatWave(moving: speaking, tint: speaking ? tint : Color.secondary)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("meeting-float-\(speaker.rawValue)")
    }

    @ViewBuilder private var face: some View {
        switch speaker {
        case .persona: AssistantAvatar(size: Theme.Size.meetingFloatFace, still: true)
        case .you: Monogram(name: String(localized: "You [Meeting People]"), size: Theme.Size.meetingFloatFace, fill: Theme.Fill.chip)
        case .someone, .quiet, .paused:
            ThemedIcon(speaker == .paused ? Theme.Icon.pause : Theme.Icon.meeting).font(Theme.Font.body)
                .foregroundStyle(speaker == .someone ? Color.primary : Color.secondary)
                .scaledFrame(width: Theme.Size.meetingFloatFace, height: Theme.Size.meetingFloatFace)
                .background(Theme.Fill.chip, in: .circle)
        }
    }

    private var word: String {
        switch speaker {
        case .persona: String(localized: "Persona Is Answering [Meeting Float]")
        case .you: String(localized: "You're Speaking [Meeting Float]")
        case .someone: String(localized: "Someone Is Speaking [Meeting Float]")
        case .quiet: String(localized: "Quiet [Meeting Float]")
        case .paused: String(localized: "Paused [Meeting Float]")
        }
    }
}

/// A few bars that move while someone speaks (the level meter's pose, still under Reduce Motion), flat otherwise.
private struct MeetingFloatWave: View {
    var moving: Bool
    var tint: Color

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private static let bars = 7

    var body: some View {
        TimelineView(.animation(minimumInterval: Theme.Motion.recordingLevelFrame, paused: !moving || reduceMotion)) { context in
            let heights = RecordingLevelPose.bars(level: moving ? 0.6 : 0, at: moving && !reduceMotion ? context.date.timeIntervalSinceReferenceDate : nil,
                                                  paused: !moving, count: Self.bars)
            HStack(spacing: Theme.Size.recordingMeterGap / 2) {
                ForEach(heights.indices, id: \.self) { i in
                    Capsule().fill(tint)
                        .frame(width: Theme.Size.recordingMeterBar / 1.5, height: Theme.Size.meetingFloatWave)
                        .scaleEffect(y: heights[i], anchor: .center)
                }
            }
        }
        .frame(height: Theme.Size.meetingFloatWave)
        .accessibilityHidden(true)
    }
}

/// The docked bar on the screen's side: the recording's dot and a still waveform; a tap brings the card back.
private struct MeetingFloatDock: View {
    var paused: Bool
    var open: () -> Void

    var body: some View {
        Button(action: open) {
            VStack(spacing: Theme.Space.xs) {
                Circle().fill(paused ? Color.secondary : Theme.danger)
                    .frame(width: Theme.Size.dot, height: Theme.Size.dot)
                ThemedIcon(Theme.Icon.recording).font(Theme.Font.label).foregroundStyle(Theme.positive)
            }
            .frame(width: Theme.Size.meetingFloatDockWidth, height: Theme.Size.meetingFloatDockHeight)
            .background(Theme.groupedCard, in: .capsule)
            .overlay { Capsule().strokeBorder(Theme.Fill.chip, lineWidth: Theme.Size.hairline) }
        }
        .buttonStyle(.plain)
        .environment(\.colorScheme, .dark)
        .accessibilityLabel(Text("Meeting Recording [Meeting Float]"))
        .accessibilityHint(Text("Shows the meeting window again. [Meeting Float]"))
        .accessibilityIdentifier("meeting-float-dock")
    }
}

/// Whether the recording's microphone is a headset's (wired, AirPods or another Bluetooth headset): it hears little but
/// its wearer, so a voice is "you".
enum MeetingFloatHeadset {
    static func now() -> Bool {
        AVAudioSession.sharedInstance().currentRoute.inputs.contains { [.headsetMic, .bluetoothHFP].contains($0.portType) }
    }
}
#endif
