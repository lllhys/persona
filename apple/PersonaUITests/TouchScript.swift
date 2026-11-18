import XCTest
import notify

/// A tap that does not wait for the app to be idle (#3027). Each XCUITest gesture waits for the app to be idle before
/// and after it, and a list gliding after a flick is not idle: a tap made with `XCUICoordinate`, even SpringBoard's, came
/// 2 s after the flick, once the glide had stopped. A whole flick-and-tap played as one record of XCTest's lost its
/// timing: the moves came in a burst and the list never glided. So the flick stays XCUITest's own, and this tap is played
/// from another thread while the main one waits the glide out, through the device's event synthesizer with XCTest's event
/// record (`XCSynthesizedEventRecord` of an `XCPointerEventPath`, the classes its gestures are made of; not public API,
/// there since Xcode 9). Points are the screen's, in portrait. docs/lessons/testing.md「XCUITest cannot tap while a list glides」.
enum TouchScript {
    /// The Darwin notification the chat posts as a flick starts to glide (`ChatView.glideNote`, with `-probes YES`).
    static let glideNote = "dev.persona.test.chat-glide"

    /// Taps `point` `then` seconds after the app next posts `glideNote` (the finger has let go and the list glides), from
    /// a background queue; returns at once, before the flick is made. When the flick starts depends on XCUITest's wait
    /// for the app to be idle, which a running turn stretches: a fixed delay came before the flick, or into it. The
    /// synthesizer's own answers do not come while XCUITest's gesture call waits, so they cannot time it either.
    /// Wait on `played` once the flick's call has returned; `error` says what went wrong, `log` when each try was played.
    static func tapWhenGlideBegins(_ point: CGPoint, then: Double = 0.1, within: Double = 20) throws
        -> (played: XCTestExpectation, error: () -> Error?, log: () -> String) {
        guard let pathClass = NSClassFromString("XCPointerEventPath"),
              let recordClass = NSClassFromString("XCSynthesizedEventRecord"),
              let synthesizer = (XCUIDevice.shared as NSObject).value(forKey: "eventSynthesizer") as? NSObject
        else { throw XCTSkip("this Xcode has no XCTest event record to play touches with") }
        guard synthesizer.responds(to: Job.play) else { throw XCTSkip("this Xcode's event synthesizer plays no records") }
        let tap = try make(recordClass, "initWithName:interfaceOrientation:") { imp, sel, raw in
            typealias Init = @convention(c) (UnsafeMutableRawPointer, Selector, NSString, Int) -> UnsafeMutableRawPointer?
            return unsafeBitCast(imp, to: Init.self)(raw, sel, "tap" as NSString, 1) // portrait
        }
        let finger = try make(pathClass, "initForTouchAtPoint:offset:") { imp, sel, raw in
            typealias Init = @convention(c) (UnsafeMutableRawPointer, Selector, CGPoint, Double) -> UnsafeMutableRawPointer?
            return unsafeBitCast(imp, to: Init.self)(raw, sel, point, 0)
        }
        typealias Lift = @convention(c) (NSObject, Selector, Double) -> Void
        let lift = NSSelectorFromString("liftUpAtOffset:")
        unsafeBitCast(finger.method(for: lift), to: Lift.self)(finger, lift, 0.05)
        tap.perform(NSSelectorFromString("addPointerEventPath:"), with: finger)

        let played = XCTestExpectation(description: "tap played")
        let job = Job(synthesizer: synthesizer, tap: tap, played: played)
        let queue = DispatchQueue(label: "TouchScript")
        var token: Int32 = 0
        let status = notify_register_dispatch(glideNote, &token, queue) { token in
            notify_cancel(token)
            job.noted()
            queue.asyncAfter(deadline: .now() + then) { job.play(on: queue) }
        }
        guard status == NOTIFY_STATUS_OK else { throw XCTSkip("could not wait for the app's glide (notify \(status))") }
        queue.asyncAfter(deadline: .now() + within) { [token] in
            if job.timeOut() { notify_cancel(token) }
        }
        return (played, { job.error }, { job.log })
    }

    /// The tap, played once; what came of it. The synthesizer takes calls from any thread. While XCUITest's flick is still
    /// its gesture (the daemon counts it until some time after the finger lifts) it refuses the tap ("only one gesture can
    /// be performed at a time"); the tap is played again every `retry` s until it is taken, for `retryFor` s.
    private final class Job: @unchecked Sendable {
        static let play = NSSelectorFromString("synthesizeEvent:completion:")
        static let busy = 21 // XCTDaemonErrorDomain: another gesture is being performed
        static let retry = 0.02
        static let retryFor = 3.0
        let synthesizer: NSObject
        let tap: NSObject
        let played: XCTestExpectation
        private let lock = NSLock()
        private var started = false
        private var failure: Error?
        private var note: TimeInterval = 0
        private var lines: [String] = []
        init(synthesizer: NSObject, tap: NSObject, played: XCTestExpectation) {
            self.synthesizer = synthesizer
            self.tap = tap
            self.played = played
        }

        func noted() { lock.lock(); note = ProcessInfo.processInfo.systemUptime; lock.unlock() }

        func play(on queue: DispatchQueue) {
            lock.lock(); started = true; let since = ProcessInfo.processInfo.systemUptime - note; lock.unlock()
            typealias Synthesize = @convention(c) (NSObject, Selector, NSObject, @escaping @convention(block) (Bool, NSError?) -> Void) -> Void
            unsafeBitCast(synthesizer.method(for: Self.play), to: Synthesize.self)(synthesizer, Self.play, tap) { _, error in
                self.lock.lock()
                let now = ProcessInfo.processInfo.systemUptime - self.note
                self.lines.append(String(format: "played %.2f s after the glide began, answered %.2f s: %@", since, now,
                                         error.map { "\($0.domain) \($0.code)" } ?? "taken"))
                let again = error?.code == Self.busy && now < Self.retryFor
                if !again { self.failure = self.failure ?? error }
                self.lock.unlock()
                if again { queue.asyncAfter(deadline: .now() + Self.retry) { self.play(on: queue) } } else { self.played.fulfill() }
            }
        }

        /// No glide in time: fails the wait. True if the tap was never played.
        func timeOut() -> Bool {
            lock.lock(); defer { lock.unlock() }
            guard !started else { return false }
            started = true
            failure = NSError(domain: "TouchScript", code: 2, userInfo: [NSLocalizedDescriptionKey: "the app posted no glide"])
            played.fulfill()
            return true
        }

        var error: Error? { lock.lock(); defer { lock.unlock() }; return failure }
        /// Each time the tap was played, when, and what the synthesizer said.
        var log: String { lock.lock(); defer { lock.unlock() }; return lines.joined(separator: "\n") }
    }

    /// Two fingers down at `a[0]` and `b[0]`, through the rest of their points `step` seconds apart, then up: a gesture
    /// XCUITest has no call for (#6196: a scroll whose fingers drift apart). Points are the screen's, in portrait.
    /// Played through the device's event synthesizer as `tapWhenGlideBegins` plays its tap; returns once it has played.
    static func twoFingers(_ a: [CGPoint], _ b: [CGPoint], step: Double = 0.016) throws {
        guard let pathClass = NSClassFromString("XCPointerEventPath"),
              let recordClass = NSClassFromString("XCSynthesizedEventRecord"),
              let synthesizer = (XCUIDevice.shared as NSObject).value(forKey: "eventSynthesizer") as? NSObject,
              synthesizer.responds(to: Job.play), a.count == b.count, a.count > 1
        else { throw XCTSkip("this Xcode has no XCTest event record to play touches with") }
        let record = try make(recordClass, "initWithName:interfaceOrientation:") { imp, sel, raw in
            typealias Init = @convention(c) (UnsafeMutableRawPointer, Selector, NSString, Int) -> UnsafeMutableRawPointer?
            return unsafeBitCast(imp, to: Init.self)(raw, sel, "two fingers" as NSString, 1) // portrait
        }
        typealias Move = @convention(c) (NSObject, Selector, CGPoint, Double) -> Void
        typealias Lift = @convention(c) (NSObject, Selector, Double) -> Void
        let move = NSSelectorFromString("moveToPoint:atOffset:"), lift = NSSelectorFromString("liftUpAtOffset:")
        for points in [a, b] {
            let finger = try make(pathClass, "initForTouchAtPoint:offset:") { imp, sel, raw in
                typealias Init = @convention(c) (UnsafeMutableRawPointer, Selector, CGPoint, Double) -> UnsafeMutableRawPointer?
                return unsafeBitCast(imp, to: Init.self)(raw, sel, points[0], 0)
            }
            for (i, p) in points.enumerated().dropFirst() {
                unsafeBitCast(finger.method(for: move), to: Move.self)(finger, move, p, Double(i) * step)
            }
            unsafeBitCast(finger.method(for: lift), to: Lift.self)(finger, lift, Double(points.count) * step)
            record.perform(NSSelectorFromString("addPointerEventPath:"), with: finger)
        }
        let played = XCTestExpectation(description: "two fingers played")
        let outcome = Outcome()
        typealias Synthesize = @convention(c) (NSObject, Selector, NSObject, @escaping @convention(block) (Bool, NSError?) -> Void) -> Void
        unsafeBitCast(synthesizer.method(for: Job.play), to: Synthesize.self)(synthesizer, Job.play, record) { _, error in
            outcome.set(error)
            played.fulfill()
        }
        guard XCTWaiter().wait(for: [played], timeout: 10 + Double(a.count) * step) == .completed else {
            throw NSError(domain: "TouchScript", code: 3, userInfo: [NSLocalizedDescriptionKey: "the two fingers were never played"])
        }
        if let error = outcome.error { throw error }
    }

    /// What the synthesizer answered, from its thread.
    private final class Outcome: @unchecked Sendable {
        private let lock = NSLock()
        private var failure: Error?
        func set(_ e: Error?) { lock.lock(); failure = e; lock.unlock() }
        var error: Error? { lock.lock(); defer { lock.unlock() }; return failure }
    }

    /// `alloc` and an `init…` through the runtime, keeping ARC's counts: `alloc` hands over +1, the initializer takes it
    /// and hands back +1.
    private static func make(_ cls: AnyClass, _ initializer: String,
                             _ call: (IMP, Selector, UnsafeMutableRawPointer) -> UnsafeMutableRawPointer?) throws -> NSObject {
        let sel = NSSelectorFromString(initializer)
        guard let allocated = (cls as AnyObject).perform(NSSelectorFromString("alloc")),
              let imp = class_getMethodImplementation(cls, sel),
              let made = call(imp, sel, allocated.toOpaque()),
              let object = Unmanaged<AnyObject>.fromOpaque(made).takeRetainedValue() as? NSObject
        else { throw XCTSkip("XCTest's \(cls) has no \(initializer)") }
        return object
    }
}
