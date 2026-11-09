#if os(iOS)
import Foundation
import HealthKit
import Observation

/// Consent and the HealthKit cursor are committed together. Failed uploads leave the cursor in place for retry.
@Observable
final class WorkoutReports {
    static let shared = WorkoutReports()
    private let health = HKHealthStore()
    private let file: URL
    private var state: State
    private(set) var busy = false
    private(set) var error: String?
    var enabled: Bool { state.enabledAt != nil }

    nonisolated struct State: Codable {
        var enabledAt: Date?
        var group: String = ""
        var anchor: Data?
        var pending: [Report] = []
    }
    nonisolated struct HeartRate: Codable {
        let minute: Int
        let bpm: Double
    }
    nonisolated struct Report: Codable {
        let id: String
        let title: String
        let activity: String
        let source: String
        let start: Date
        let end: Date
        let enabled_at: Date
        let duration: Double
        let distance: Double?
        let energy: Double?
        let heart_rate: [HeartRate]
    }

    init(folder: URL? = nil) {
        let root = folder ?? URL.applicationSupportDirectory.appending(path: "WorkoutReports")
        file = root.appending(path: "state.json")
        do {
            state = FileManager.default.fileExists(atPath: file.path)
                ? try JSON.decoder.decode(State.self, from: Data(contentsOf: file)) : State()
        } catch {
            state = State()
            self.error = String(localized: "Couldn't read workout report settings. Turn the feature on again to start fresh.")
        }
    }

    private func save(_ next: State) throws {
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true)
        try JSON.encoder.encode(next).write(to: file, options: [.atomic, .completeFileProtection])
        state = next
    }

    func setEnabled(_ enabled: Bool, model: AppModel) async {
        guard !busy else { return }
        busy = true
        defer { busy = false }
        do {
            if enabled {
                guard let hub = model.hub, !hub.isDemo, HKHealthStore.isHealthDataAvailable() else { return }
                let types: Set<HKObjectType> = [HKObjectType.workoutType(), HKQuantityType(.heartRate)]
                try await health.requestAuthorization(toShare: [], read: types)
                try save(State(enabledAt: Date(), group: hub.group))
            } else {
                try save(State())
            }
            error = nil
        } catch { self.error = error.localizedDescription }
    }

    func refresh(model: AppModel) async {
        guard !busy, let since = state.enabledAt, let hub = model.hub,
              !hub.isDemo, hub.group == state.group else { return }
        busy = true
        defer { busy = false }
        do {
            // Upload a previous batch first: a network outage cannot grow an unbounded outbox.
            try await upload(api: hub.api)
            let anchor = try state.anchor.map {
                try NSKeyedUnarchiver.unarchivedObject(ofClass: HKQueryAnchor.self, from: $0)
            } ?? nil
            let (workouts, nextAnchor) = try await read(since: since, anchor: anchor)
            var next = state
            for workout in workouts where workout.startDate >= since && workout.duration >= 600 && workout.endDate <= Date() && workout.endDate.timeIntervalSince(workout.startDate) <= 7 * 86400 {
                let rates = try await heartRates(workout)
                next.pending.append(Report(
                    id: workout.uuid.uuidString, title: String(localized: "Workout report"),
                    activity: Self.activityName(workout.workoutActivityType), source: workout.sourceRevision.source.name,
                    start: workout.startDate, end: workout.endDate, enabled_at: since, duration: workout.duration,
                    distance: workout.totalDistance?.doubleValue(for: .meter()),
                    energy: workout.statistics(for: HKQuantityType(.activeEnergyBurned))?.sumQuantity()?.doubleValue(for: .kilocalorie()),
                    heart_rate: rates))
            }
            next.anchor = try nextAnchor.map { try NSKeyedArchiver.archivedData(withRootObject: $0, requiringSecureCoding: true) }
            try save(next)
            try await upload(api: hub.api)
            error = nil
        } catch { self.error = error.localizedDescription }
    }

    private static func activityName(_ activity: HKWorkoutActivityType) -> String {
        switch activity {
        case .running: "Running"
        case .walking: "Walking"
        case .cycling: "Cycling"
        case .swimming: "Swimming"
        case .hiking: "Hiking"
        case .yoga: "Yoga"
        case .traditionalStrengthTraining, .functionalStrengthTraining: "Strength training"
        case .highIntensityIntervalTraining: "Interval training"
        default: "Workout (HealthKit activity \(activity.rawValue))"
        }
    }

    private func upload(api: APIClient) async throws {
        while let report = state.pending.first {
            let _: Data = try await api.post("/api/v1/assistant/health/workouts", body: report)
            var next = state
            next.pending.removeFirst()
            try save(next)
        }
    }

    private func read(since: Date, anchor: HKQueryAnchor?) async throws -> ([HKWorkout], HKQueryAnchor?) {
        try await withCheckedThrowingContinuation { continuation in
            let predicate = HKQuery.predicateForSamples(withStart: since, end: nil, options: .strictStartDate)
            let query = HKAnchoredObjectQuery(type: HKObjectType.workoutType(), predicate: predicate, anchor: anchor, limit: 50) {
                _, samples, _, next, error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: (samples as? [HKWorkout] ?? [], next)) }
            }
            health.execute(query)
        }
    }

    private func heartRates(_ workout: HKWorkout) async throws -> [HeartRate] {
        let samples: [HKQuantitySample] = try await withCheckedThrowingContinuation { continuation in
            let predicate = NSCompoundPredicate(andPredicateWithSubpredicates: [
                HKQuery.predicateForObjects(from: workout),
                HKQuery.predicateForSamples(withStart: workout.startDate, end: workout.endDate)
            ])
            let query = HKSampleQuery(sampleType: HKQuantityType(.heartRate), predicate: predicate,
                                      limit: HKObjectQueryNoLimit, sortDescriptors: nil) { _, samples, error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: samples as? [HKQuantitySample] ?? []) }
            }
            health.execute(query)
        }
        var buckets: [Int: [Double]] = [:]
        for sample in samples {
            let minute = Int(sample.startDate.timeIntervalSince(workout.startDate) / 60)
            let bpm = sample.quantity.doubleValue(for: HKUnit.count().unitDivided(by: .minute()))
            if minute >= 0, minute <= Int(workout.endDate.timeIntervalSince(workout.startDate) / 60), bpm > 0, bpm <= 300 {
                buckets[minute, default: []].append(bpm)
            }
        }
        return buckets.keys.sorted().map { minute in
            let values = buckets[minute]!
            return HeartRate(minute: minute, bpm: values.reduce(0, +) / Double(values.count))
        }
    }
}
#endif
