import SwiftUI
import WidgetKit

/// Record on the watch face and in the Smart Stack (docs/apple/recording.md「Apple Watch」): an explicit start, like the
/// iPhone's Control, so a tap opens the watch app straight into a meeting recording (persona://record?meeting=1). It says
/// Record, the word of every start (#5493). Static: it shows the same at all times.
struct MeetingRecordingComplication: Widget {
    /// The complication's identity on faces already set up: never change it (it was Meeting Recording until #5493).
    static let kind = "persona.watch.meeting-recording"

    var body: some WidgetConfiguration {
        StaticConfiguration(kind: Self.kind, provider: MeetingRecordingProvider()) { _ in
            MeetingRecordingComplicationFamilyView()
                .containerBackground(WatchFaceTokens.background, for: .widget)
                .widgetURL(URL(string: "persona://record?meeting=1"))
        }
        .configurationDisplayName("Record [Recordings Section ef2821]")
        .description("Starts recording a meeting on Apple Watch.")
        .supportedFamilies([.accessoryCircular, .accessoryCorner, .accessoryRectangular, .accessoryInline])
    }
}

struct MeetingRecordingEntry: TimelineEntry { var date: Date }

struct MeetingRecordingProvider: TimelineProvider {
    func placeholder(in context: Context) -> MeetingRecordingEntry { MeetingRecordingEntry(date: .now) }
    func getSnapshot(in context: Context, completion: @escaping (MeetingRecordingEntry) -> Void) { completion(MeetingRecordingEntry(date: .now)) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<MeetingRecordingEntry>) -> Void) {
        completion(Timeline(entries: [MeetingRecordingEntry(date: .now)], policy: .never))
    }
}

/// The face as WidgetKit sizes it, in the family it asks for.
struct MeetingRecordingComplicationFamilyView: View {
    @Environment(\.widgetFamily) private var family

    var body: some View { MeetingRecordingComplicationView(family: family) }
}
