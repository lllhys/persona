import AppIntents
import SwiftUI
import WidgetKit

/// Control Center / Lock Screen / Action Button: Start Meeting (#8731; Record before), a meeting (`StartMeetingRecordingIntent`), without opening the
/// app; its Live Activity then shows the time and Stop. The kind is the control's identity: never change it (it was
/// Record a Meeting until #5243).
struct MeetingRecordingControl: ControlWidget {
    static let kind = "persona.control.meeting-recording"

    var body: some ControlWidgetConfiguration {
        StaticControlConfiguration(kind: Self.kind) {
            ControlWidgetButton(action: StartMeetingRecordingIntent()) {
                Label("Start Meeting", systemImage: RecordingSymbol.recordingAction)
            }
        }
        .displayName("Start Meeting")
        .description("Starts recording a meeting in Persona, named after the calendar event happening now.")
    }
}
