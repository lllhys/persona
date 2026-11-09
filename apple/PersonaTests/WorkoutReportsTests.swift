#if os(iOS)
import Foundation
import Testing
@testable import Persona

@MainActor
struct WorkoutReportsTests {
    @Test func newInstallDoesNotOptIn() throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: folder) }
        let reports = WorkoutReports(folder: folder)
        #expect(!reports.enabled)
        #expect(reports.error == nil)
    }

    @Test func unreadableConsentFailsClosed() throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: folder) }
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try Data("broken state".utf8).write(to: folder.appending(path: "state.json"))
        let reports = WorkoutReports(folder: folder)
        #expect(!reports.enabled)
        #expect(reports.error != nil)
    }

    @Test func restartPreservesConsentAndPendingMeasurements() throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: folder) }
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let report = WorkoutReports.Report(id: UUID().uuidString, title: "Workout report", activity: "Running",
            source: "Test", start: start, end: start.addingTimeInterval(600), enabled_at: start,
            duration: 600, distance: nil, energy: nil, heart_rate: [])
        let state = WorkoutReports.State(enabledAt: start, group: "test-hub", anchor: Data([1, 2]), pending: [report])
        let file = folder.appending(path: "state.json")
        try JSON.encoder.encode(state).write(to: file)
        #expect(WorkoutReports(folder: folder).enabled)
        let restored = try JSON.decoder.decode(WorkoutReports.State.self, from: Data(contentsOf: file))
        #expect(restored.enabledAt == start)
        #expect(restored.pending.first?.id == report.id)
        #expect(restored.pending.first?.distance == nil)
        #expect(restored.anchor == Data([1, 2]))
    }
}
#endif
