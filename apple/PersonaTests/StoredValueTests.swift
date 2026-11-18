import Foundation
import Observation
import Testing
@testable import Persona

/// `@StoredValue` redraws only for its own key (#1337): `@AppStorage` redrew every view holding one on any write to
/// UserDefaults (a Mac window resize writes the split view's frames every frame).
@MainActor
@Suite(.serialized)
struct StoredValueTests {
    let defaults = UserDefaults(suiteName: "StoredValueTests")!

    init() { defaults.removePersistentDomain(forName: "StoredValueTests") }

    /// Whether reading `read` registered a change by the time `act` ran and the notification queue drained.
    private func changes(_ read: @escaping () -> Void, _ act: () -> Void) async -> Bool {
        nonisolated(unsafe) var fired = false
        withObservationTracking(read) { fired = true }
        act()
        await Task.yield()
        try? await Task.sleep(for: .milliseconds(50))
        return fired
    }

    @Test func anotherKeysWriteDoesNotRedraw() async {
        let compact = StoredValue(wrappedValue: false, "test.compact", store: defaults)
        #expect(await !changes({ _ = compact.wrappedValue }) { defaults.set("x", forKey: "test.other") })
        // The same value written again is no change either.
        #expect(await !changes({ _ = compact.wrappedValue }) { defaults.set(false, forKey: "test.compact") })
    }

    @Test func itsOwnKeyRedraws() async {
        let compact = StoredValue(wrappedValue: false, "test.compact2", store: defaults)
        #expect(await changes({ _ = compact.wrappedValue }) { compact.wrappedValue = true })
        #expect(defaults.bool(forKey: "test.compact2"))
        // Written elsewhere (an `@AppStorage` of the same key): picked up.
        #expect(await changes({ _ = compact.wrappedValue }) { defaults.set(false, forKey: "test.compact2") })
        #expect(compact.wrappedValue == false)
    }

    /// The value the key has already is not written again (#4206): every write wakes every `@AppStorage` view.
    @Test func theSameValueIsNotWrittenAgain() async {
        let compact = StoredValue(wrappedValue: false, "test.same", store: defaults)
        compact.wrappedValue = true
        nonisolated(unsafe) var writes = 0
        let token = NotificationCenter.default.addObserver(forName: UserDefaults.didChangeNotification, object: defaults, queue: nil) { _ in
            writes += 1
        }
        defer { NotificationCenter.default.removeObserver(token) }
        compact.wrappedValue = true
        #expect(writes == 0)
        compact.wrappedValue = false
        #expect(writes == 1)
        #expect(defaults.bool(forKey: "test.same") == false)
    }

    @Test func rawValuesAndSharing() {
        enum Mode: String { case a, b }
        let one = StoredValue(wrappedValue: Mode.a, "test.mode", store: defaults)
        let two = StoredValue(wrappedValue: Mode.a, "test.mode", store: defaults)
        one.wrappedValue = .b
        #expect(two.wrappedValue == .b, "views storing the same key share one value")
        #expect(defaults.string(forKey: "test.mode") == "b")
    }

    /// A launch argument's value is a string in the argument domain (`-dev.compact YES`): read as `@AppStorage` reads
    /// it, not as the default (#2147).
    @Test func stringsReadAsLaunchArgumentsGiveThem() {
        enum Level: Int { case low = 1, high = 2 }
        defaults.set("YES", forKey: "test.flagString")
        #expect(StoredValue(wrappedValue: false, "test.flagString", store: defaults).wrappedValue == true)
        defaults.set("NO", forKey: "test.flagNo")
        #expect(StoredValue(wrappedValue: true, "test.flagNo", store: defaults).wrappedValue == false)
        defaults.set("3", forKey: "test.countString")
        #expect(StoredValue(wrappedValue: 0, "test.countString", store: defaults).wrappedValue == 3)
        defaults.set("1.5", forKey: "test.scaleString")
        #expect(StoredValue(wrappedValue: 1.0, "test.scaleString", store: defaults).wrappedValue == 1.5)
        defaults.set("2", forKey: "test.levelString")
        #expect(StoredValue(wrappedValue: Level.low, "test.levelString", store: defaults).wrappedValue == .high)
        // Missing: the default, as before.
        #expect(StoredValue(wrappedValue: true, "test.flagMissing", store: defaults).wrappedValue == true)
        #expect(StoredValue(wrappedValue: 7, "test.countMissing", store: defaults).wrappedValue == 7)
    }
}
