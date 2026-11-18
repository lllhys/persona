import Foundation
import MetricKit

/// MetricKit for Developing Persona (#9460, closes #4265): while the switch is on, the system's daily metric payloads
/// (CPU, run time, network, disk, GPU, display, animation, responsiveness, memory, exits, cellular) and its diagnostic
/// payloads (crashes, hangs, CPU and disk-write exceptions, with unsymbolicated call stacks) are kept as delivered,
/// with any text in them scrubbed. Turning the switch on also keeps what the system still has from before
/// (`pastPayloads`, `pastDiagnosticPayloads`).
nonisolated final class DevMetrics: NSObject, MXMetricManagerSubscriber, @unchecked Sendable {
    static let shared = DevMetrics(store: .shared)

    let store: DevStore

    init(store: DevStore) { self.store = store }

    func start() {
        let manager = MXMetricManager.shared
        manager.add(self)
        keep(manager.pastPayloads)
        keep(manager.pastDiagnosticPayloads)
    }

    func stop() { MXMetricManager.shared.remove(self) }

    func didReceive(_ payloads: [MXMetricPayload]) { keep(payloads) }

    func didReceive(_ payloads: [MXDiagnosticPayload]) { keep(payloads) }

    private func keep(_ payloads: [MXMetricPayload]) {
        for p in payloads {
            store.writeMetric(Self.scrubbed(p.jsonRepresentation()), name: "metrics-\(Self.name(p.timeStampBegin))-\(Self.name(p.timeStampEnd)).json")
        }
    }

    private func keep(_ payloads: [MXDiagnosticPayload]) {
        for p in payloads {
            let json = p.jsonRepresentation()
            // Several can share an end: the bytes tell them apart.
            store.writeMetric(Self.scrubbed(json), name: "diagnostics-\(Self.name(p.timeStampEnd))-\(Self.digest(json)).json")
        }
    }

    static func name(_ date: Date) -> String {
        date.formatted(.iso8601.year().month().day().dateSeparator(.omitted).time(includingFractionalSeconds: false).timeSeparator(.omitted))
    }

    /// A short, stable tag for some bytes (FNV-1a).
    static func digest(_ data: Data) -> String {
        var hash: UInt64 = 0xcbf29ce484222325
        for byte in data { hash = (hash ^ UInt64(byte)) &* 0x100000001b3 }
        return String(hash, radix: 16)
    }

    /// The payload's JSON with every string through `DevScrub.text` (an exception's reason can carry anything); numbers,
    /// binary names and addresses stay as they are. Bytes that aren't JSON stay out.
    static func scrubbed(_ json: Data) -> Data {
        guard let object = try? JSONSerialization.jsonObject(with: json) else { return Data("{}".utf8) }
        let clean = scrub(object, key: nil)
        return (try? JSONSerialization.data(withJSONObject: clean, options: [.sortedKeys, .prettyPrinted, .withoutEscapingSlashes])) ?? Data("{}".utf8)
    }

    /// Keys whose strings are the system's own identifiers (binary names and UUIDs, versions, device types), kept whole.
    private static let kept: Set<String> = ["binaryName", "binaryUUID", "appVersion", "appBuildVersion", "osVersion", "deviceType",
                                           "platformArchitecture", "regionFormat", "bundleIdentifier", "metaVersion", "timeStampBegin",
                                           "timeStampEnd", "terminationReason", "exceptionType", "exceptionCode", "signal",
                                           "virtualMemoryRegionInfo", "callStackPerThread", "lowPowerModeEnabled", "isTestFlightApp"]

    private static func scrub(_ value: Any, key: String?) -> Any {
        switch value {
        case let dict as [String: Any]: return Dictionary(uniqueKeysWithValues: dict.map { ($0.key, scrub($0.value, key: $0.key)) })
        case let list as [Any]: return list.map { scrub($0, key: key) }
        case let text as String: return key.map(kept.contains) == true ? text : DevScrub.text(text)
        default: return value
        }
    }
}
