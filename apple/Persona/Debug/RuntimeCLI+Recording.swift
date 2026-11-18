#if DEBUG && os(iOS)
import Foundation

/// Recordings, for a script that records meetings (#9417; docs/operations/runtime-cli.md「Commands」): `recording` says
/// what records; `action` `stop` ends it as End's End and Write the Minutes does (`MeetingRecorder.stop`), and `delete`
/// with an `id` deletes that recording, here and then on the hub, as the row's Delete does once confirmed
/// (`RecordingDeletion.run`). A confirmation dialog's buttons take no accessibility activation, and `alert` dismisses
/// one without running its SwiftUI action.
extension RuntimeCLI {
    static let recordingCommands: [String: Command] = [
        "recording": { a in recording(a) },
    ]

    private static func recording(_ a: Args) -> Reply {
        let capture = RecordingCapture.shared
        let id = capture.recordingID?.uuidString
        switch a["action"] as? String {
        case nil:
            return ["state": "\(capture.state)", "recording": id ?? NSNull(), "elapsed": capture.elapsed]
        case "stop":
            guard let id else { return fail("nothing is recording") }
            MeetingRecorder.shared.stop(recordingID: id)
            return ["stopped": id]
        case "delete":
            guard let target = (a["id"] as? String).flatMap(UUID.init(uuidString:)), RecordingLibrary.shared.entry(target) != nil else {
                return fail("no recording with that id")
            }
            guard target.uuidString != id else { return fail("that recording is being made: stop it first") }
            if let problem = RecordingDeletion.run(target) { return fail(problem) }
            return ["deleted": target.uuidString]
        case let other?:
            return fail("unknown action \(other): stop, delete")
        }
    }
}
#endif
