import Foundation
import os
import PersonaWire

/// Where shared code tells Developing Persona (Settings › About, #9460, docs/operations/developing-persona.md) what it
/// counts: the hub socket's frames by type, its connects and drops, and the error reports the app keeps. A no-op until
/// the app installs a sink, which it does only while the switch is on; the watch and the extensions never do.
nonisolated enum DevProbe {
    enum Socket: String, Sendable { case connected, dropped }

    protocol Sink: AnyObject, Sendable {
        func frame(type: String, bytes: Int)
        func socket(_ event: Socket)
        func error(_ report: ExtensionErrorReport)
    }

    private static let sink = OSAllocatedUnfairLock<(any Sink)?>(initialState: nil)

    static func install(_ new: (any Sink)?) { sink.withLock { $0 = new } }

    static func frame(type: String, bytes: Int) { sink.withLock { $0 }?.frame(type: type, bytes: bytes) }
    static func socket(_ event: Socket) { sink.withLock { $0 }?.socket(event) }
    static func error(_ report: ExtensionErrorReport) { sink.withLock { $0 }?.error(report) }
}
