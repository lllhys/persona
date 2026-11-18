import SwiftUI
import WidgetKit

/// What the Record complication draws (`MeetingRecordingComplication`), the same at all times: the recording mark and
/// Record, the word of every start (#5493). The watch app compiles this file too, for its debug preview of the faces.
/// Only the view: the widget and its provider stay in the extension.
struct MeetingRecordingComplicationView: View {
    let family: WidgetFamily

    var body: some View {
        switch family {
        case .accessoryCircular:
            ZStack {
                AccessoryWidgetBackground()
                Image(systemName: WatchFaceTokens.recordingSymbol).font(WatchFaceTokens.recordingGlyph)
                    .foregroundStyle(WatchFaceTokens.recording)
            }
            .accessibilityLabel(Text("Record [Recordings Section ef2821]"))
        case .accessoryCorner:
            Image(systemName: WatchFaceTokens.recordingSymbol).font(WatchFaceTokens.recordingGlyph)
                .foregroundStyle(WatchFaceTokens.recording)
                .widgetLabel { Text("Record [Recordings Section ef2821]") }
        case .accessoryInline:
            Label("Record [Recordings Section ef2821]", systemImage: WatchFaceTokens.recordingSymbol)
        default:
            HStack(spacing: WatchFaceTokens.previewSpacing) {
                Image(systemName: WatchFaceTokens.recordingSymbol).font(WatchFaceTokens.recordingGlyph)
                    .foregroundStyle(WatchFaceTokens.recording)
                VStack(alignment: .leading) {
                    Text("Record [Recordings Section ef2821]").font(WatchFaceTokens.title).lineLimit(1).minimumScaleFactor(WatchFaceTokens.titleMinScale)
                    // What a tap records, which the title alone no longer says (it was Meeting Recording until #5493).
                    Text("Meeting").font(WatchFaceTokens.caption).foregroundStyle(.secondary).lineLimit(1)
                }
                Spacer(minLength: 0)
            }
        }
    }
}

#if DEBUG
/// `-openWatchItem record-complication` in the watch app: every family at a 46 mm face's sizes, on a mock face's grey,
/// since a simulator cannot put a complication on a face from the command line (screenshots).
struct MeetingRecordingComplicationPreview: View {
    var body: some View {
        let size = WatchFaceTokens.previewRectangular
        ScrollView {
            VStack(alignment: .leading, spacing: WatchFaceTokens.previewSpacing) {
                HStack(spacing: WatchFaceTokens.previewSpacing) {
                    MeetingRecordingComplicationView(family: .accessoryCircular)
                        .frame(width: WatchFaceTokens.previewCircular, height: WatchFaceTokens.previewCircular)
                    MeetingRecordingComplicationView(family: .accessoryCorner)
                        .frame(width: WatchFaceTokens.previewCorner, height: WatchFaceTokens.previewCorner)
                }
                MeetingRecordingComplicationView(family: .accessoryInline).font(WatchFaceTokens.previewInline).lineLimit(1)
                MeetingRecordingComplicationView(family: .accessoryRectangular).frame(width: size.width, height: size.height)
                    .background(WatchFaceTokens.previewWash, in: RoundedRectangle(cornerRadius: WatchFaceTokens.previewRadius))
            }
        }
        .accessibilityIdentifier("watch-record-complication-preview")
    }
}
#endif
