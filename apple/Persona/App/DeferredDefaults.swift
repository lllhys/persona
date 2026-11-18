import Foundation
import PersonaWire
#if os(macOS)
import AppKit
#endif

/// UserDefaults writes that can wait (the section or tab to come back to): every write to UserDefaults draws each
/// `AppStorage` view in the app again (CLAUDE.md「Most common pitfalls」), and a switch between the Mac's
/// sections wrote two, with every section's page kept in the window (#390). On the Mac they are written once the
/// switching rests (`rest`), when the app is left, or when it quits; elsewhere, and to any other defaults, at once.
/// The keys stay in UserDefaults rather than `StateCache`: they are read at launch where they always were, and a crash
/// before the rest loses only the last switch (where to come back to).
@MainActor enum DeferredDefaults {
    static let rest: Duration = .seconds(3)
    private static var pending: [String: Any?] = [:]
    private static var task: Task<Void, Never>?
    private static var observers: [Any] = []

    /// `value` nil removes the key.
    static func set(_ value: Any?, forKey key: String, in defaults: UserDefaults = .standard) {
        #if os(macOS)
        guard defaults === UserDefaults.standard else { return write(value, key, defaults) }
        pending[key] = .some(value)
        watchTheApp()
        task?.cancel()
        task = Task {
            try? await Task.sleep(for: rest)
            guard !Task.isCancelled else { return }
            flush()
        }
        #else
        write(value, key, defaults)
        #endif
    }

    /// Writes what waits.
    static func flush() {
        task?.cancel()
        task = nil
        let now = pending
        pending = [:]
        for (key, value) in now { write(value ?? nil, key, .standard) }
    }

    private static func write(_ value: Any?, _ key: String, _ defaults: UserDefaults) {
        if let value { defaults.set(value, forKey: key) } else { defaults.removeObject(forKey: key) }
    }

    #if os(macOS)
    private static func watchTheApp() {
        guard observers.isEmpty else { return }
        for name in [NSApplication.willResignActiveNotification, NSApplication.willTerminateNotification] {
            observers.append(NotificationCenter.default.addObserver(forName: name, object: nil, queue: .main) { _ in
                MainActor.assumeIsolated { flush() }
            })
        }
    }
    #endif
}
