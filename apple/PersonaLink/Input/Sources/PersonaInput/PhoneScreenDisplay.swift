import AppKit
import CGVirtualDisplayBridge
import CoreGraphics
import PersonaInputKit

// Phone Screen's display (#9314): the portrait virtual display, its modes, and the other displays' modes kept as they
// were. What was measured (Snug, https://github.com/zephyr4123/snug, MIT; and on macOS 26.6 for #9314):
// - every width of the fit is a mode, HiDPI on, the pixel limit framing every aspect: each width has a 2× mode;
// - a new display comes online at the mode macOS remembers for it (the first time the widest, at 1×), so the helper
//   switches it to the base width at 2× once it is online;
// - switching modes and moving the display run in a short-lived child (`--phone-screen-set`): a process that switched a
//   virtual display's mode itself keeps it online 40 s after it lets it go; released after a child switched it, it goes
//   offline in 0.02 s;
// - a process can't read the modes of a display that appeared after it started, so the display's own size comes from
//   CGDisplayBounds, and its scale is the mode the child set;
// - macOS applies the modes it remembers for a combination of displays whenever the combination changes, so the other
//   displays' modes are recorded before adding and removing and set back after.

/// A display mode as macOS reports it, enough to find the same one again (mode ids are renumbered between combinations).
struct ModeSnapshot: Equatable {
    var width: Int, height: Int, pixelWidth: Int, pixelHeight: Int, refresh: Double, ioID: Int32

    init(_ m: CGDisplayMode) {
        width = m.width; height = m.height; pixelWidth = m.pixelWidth; pixelHeight = m.pixelHeight
        refresh = m.refreshRate; ioID = m.ioDisplayModeID
    }

    init(width: Int, height: Int, pixelWidth: Int, pixelHeight: Int, refresh: Double, ioID: Int32) {
        self.width = width; self.height = height; self.pixelWidth = pixelWidth; self.pixelHeight = pixelHeight
        self.refresh = refresh; self.ioID = ioID
    }

    func same(as o: ModeSnapshot) -> Bool {
        width == o.width && height == o.height && pixelWidth == o.pixelWidth && pixelHeight == o.pixelHeight
            && abs(refresh - o.refresh) < 0.5
    }

    var arguments: [String] { ["\(width)", "\(height)", "\(pixelWidth)", "\(pixelHeight)", "\(refresh)", "\(ioID)"] }
}

enum Displays {
    static func online() -> [CGDirectDisplayID] {
        var n: UInt32 = 0
        guard CGGetOnlineDisplayList(0, nil, &n) == .success, n > 0 else { return [] }
        var ids = [CGDirectDisplayID](repeating: 0, count: Int(n))
        guard CGGetOnlineDisplayList(n, &ids, &n) == .success else { return [] }
        return Array(ids.prefix(Int(n)))
    }

    static func isOnline(_ id: CGDirectDisplayID) -> Bool { online().contains(id) }

    /// The modes of displays this process can read (every display that was there when it started).
    static func modes(except skip: CGDirectDisplayID? = nil) -> [CGDirectDisplayID: ModeSnapshot] {
        var out: [CGDirectDisplayID: ModeSnapshot] = [:]
        for id in online() where id != skip {
            if let m = CGDisplayCopyDisplayMode(id) { out[id] = ModeSnapshot(m) }
        }
        return out
    }

    /// Every display in the global space; `known` gives the scale of a display whose mode this process can't read.
    static func arrangement(known: [CGDirectDisplayID: Double]) -> [PhoneScreenDisplay] {
        let main = CGMainDisplayID()
        return online().compactMap { id in
            let b = CGDisplayBounds(id)
            guard !b.isEmpty else { return nil }
            var scale = known[id] ?? 1
            if known[id] == nil, let m = CGDisplayCopyDisplayMode(id), m.width > 0 {
                scale = Double(m.pixelWidth) / Double(m.width)
            }
            return PhoneScreenDisplay(id: Int(id), x: Int(b.minX), y: Int(b.minY), width: Int(b.width), height: Int(b.height),
                                      scale: scale, main: id == main ? true : nil)
        }
    }

    /// Polls until `done` or `timeout`, keeping the main run loop turning (display callbacks come on it).
    @discardableResult
    static func poll(timeout: TimeInterval, every: TimeInterval = 0.05, until done: () -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while !done() {
            guard Date() < deadline else { return false }
            RunLoop.main.run(until: Date().addingTimeInterval(every))
        }
        return true
    }
}

/// The portrait virtual display. Made and let go on the main thread; letting go must leave no other strong reference.
@MainActor
final class PhoneDisplay {
    /// Fixed identities, so ColorSync keeps one profile, not one a session; the next is tried while a display with the
    /// previous one is still online (initWithDescriptor gives nil then) or never comes online.
    static let serials: [UInt32] = [9_314_001, 9_314_002, 9_314_003]
    static let vendorID: UInt32 = 0x5045 // "PE"
    static let productID: UInt32 = 0x9314
    static let name = "Persona Phone Screen"
    /// About 220 pixels per inch at the base width, so macOS treats it as a Retina panel.
    static let millimeters = CGSize(width: 62, height: 124)

    private var display: CGVirtualDisplay?
    private(set) var id: CGDirectDisplayID = 0
    private(set) var fit: PhoneScreenFit
    private(set) var width: Int

    init(fit: PhoneScreenFit) {
        self.fit = fit
        width = fit.baseWidth
    }

    /// Adds the display, waits for it to come online and switches it to the base width at 2× beside the main display.
    func create() throws {
        for serial in Self.serials {
            guard let d = make(serial: serial) else { continue }
            display = d
            id = d.displayID
            if Displays.poll(timeout: 6, until: { Displays.isOnline(self.id) }) {
                Displays.poll(timeout: 0.8) { false } // its mode is still settling right after it comes online
                guard set(width: fit.baseWidth, place: true) else { throw PhoneScreenError.display("could not switch it to 2×") }
                return
            }
            release()
        }
        throw PhoneScreenError.display("macOS did not bring a virtual display online")
    }

    /// A new aspect on the same display: a new mode list (the display stays online and keeps its id), then the base width.
    func reshape(_ new: PhoneScreenFit) -> Bool {
        guard let d = display else { return false }
        guard autoreleasepool(invoking: { d.apply(Self.settings(new)) }) else { return false }
        fit = new
        let base = new.size(width: new.baseWidth)
        Displays.poll(timeout: 3, every: 0.1) { Self.modeListed(id, base) }
        return set(width: new.baseWidth, place: true)
    }

    /// Switches to `width` at 2× (a child does it), placing the display beside the main one when `place`; waits until
    /// the global space shows the new size.
    func set(width w: Int, place: Bool) -> Bool {
        let size = fit.size(width: w)
        var args = ["--phone-screen-set", "\(id)", "\(size.width)", "\(size.height)"]
        if place {
            let others = Displays.online().filter { $0 != id && $0 != CGMainDisplayID() }.map { CGDisplayBounds($0).phoneRect }
            if let o = PhoneScreenPlacement.origin(for: size, main: CGDisplayBounds(CGMainDisplayID()).phoneRect, others: others) {
                args += ["\(Int(o.x))", "\(Int(o.y))"]
            }
        }
        guard Child.run(args) == 0 else { return false }
        let ok = Displays.poll(timeout: 2) {
            let b = CGDisplayBounds(self.id)
            return Int(b.width) == size.width && Int(b.height) == size.height
        }
        if ok { width = w }
        return ok
    }

    /// Where windows go on it, in the global space: NSScreen's visible frame (below the menu bar) when AppKit lists it,
    /// else its bounds less a menu bar's height.
    func area() -> CGRect {
        let bounds = CGDisplayBounds(id)
        let main = NSScreen.screens.first { $0.frame.origin == .zero } ?? NSScreen.main
        if let screen = NSScreen.screens.first(where: { ($0.deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.uint32Value == id }),
           let mainHeight = main?.frame.height, screen.frame.width == bounds.width {
            let v = screen.visibleFrame
            return CGRect(x: v.minX, y: mainHeight - v.maxY, width: v.width, height: v.height)
        }
        // The menu bar is as tall on every display as on the main one.
        let bar = main.map { max(0, $0.frame.maxY - $0.visibleFrame.maxY) } ?? 25
        return CGRect(x: bounds.minX, y: bounds.minY + bar, width: bounds.width, height: bounds.height - bar)
    }

    var rect: PhoneScreenDisplay {
        let b = CGDisplayBounds(id)
        return PhoneScreenDisplay(id: Int(id), x: Int(b.minX), y: Int(b.minY), width: Int(b.width), height: Int(b.height), scale: 2)
    }

    /// Lets the display go and waits until it is offline (normally 0.1 s; a sleeping monitor delays it until it wakes).
    @discardableResult
    func release() -> Bool {
        let gone = id
        autoreleasepool { display = nil }
        guard gone != 0 else { return true }
        return Displays.poll(timeout: 10, every: 0.1) { !Displays.isOnline(gone) }
    }

    private func make(serial: UInt32) -> CGVirtualDisplay? {
        let d = CGVirtualDisplayDescriptor()
        d.name = Self.name
        d.maxPixelsWide = UInt32(PhoneScreenFit.maxPixels.width)
        d.maxPixelsHigh = UInt32(PhoneScreenFit.maxPixels.height)
        d.sizeInMillimeters = Self.millimeters
        d.vendorID = Self.vendorID
        d.productID = Self.productID
        d.serialNum = serial
        d.queue = DispatchQueue.main
        guard let display = autoreleasepool(invoking: { CGVirtualDisplay(descriptor: d) }) else { return nil }
        guard autoreleasepool(invoking: { display.apply(Self.settings(fit)) }) else { return nil }
        return display
    }

    private static func settings(_ fit: PhoneScreenFit) -> CGVirtualDisplaySettings {
        let s = CGVirtualDisplaySettings()
        s.hiDPI = true
        s.modes = fit.widths.map { w in
            let size = fit.size(width: w)
            return CGVirtualDisplayMode(width: UInt(size.width), height: UInt(size.height), refreshRate: 60)
        }
        return s
    }

    /// Whether a child sees the 2× mode yet (this process can't read the new display's modes).
    private static func modeListed(_ id: CGDirectDisplayID, _ size: PhoneScreenSize) -> Bool {
        Child.run(["--phone-screen-has", "\(id)", "\(size.width)", "\(size.height)"]) == 0
    }
}

enum PhoneScreenError: Error {
    case display(String)
}

/// The helper's own binary run as a short-lived child for display configuration, waited for at most 10 s.
enum Child {
    static func run(_ args: [String]) -> Int32? {
        guard let exe = Bundle.main.executablePath ?? CommandLine.arguments.first else { return nil }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: exe)
        p.arguments = args
        do { try p.run() } catch { return nil }
        let deadline = Date().addingTimeInterval(10)
        while p.isRunning, Date() < deadline { RunLoop.main.run(until: Date().addingTimeInterval(0.02)) }
        if p.isRunning { p.terminate(); return nil }
        return p.terminationStatus
    }
}

/// What the child does: `--phone-screen-set <id> <w> <h> [<x> <y>]` switches display id to w × h points at 2× (and
/// moves it), `--phone-screen-mode <id> <w> <h> <pw> <ph> <refresh> <io id>` sets a display back to a recorded mode,
/// `--phone-screen-has <id> <w> <h>` exits 0 when the 2× mode is listed. Changes last for this login only.
enum DisplayConfig {
    static func run(_ argv: [String]) -> Int32 {
        let n = argv.dropFirst().compactMap { Double($0) }
        guard n.count == argv.count - 1, n.count >= 3, let id = CGDirectDisplayID(exactly: n[0]) else { return 2 }
        switch argv[0] {
        case "--phone-screen-has":
            return find(id, hiDPI: Int(n[1]), Int(n[2])) == nil ? 1 : 0
        case "--phone-screen-set":
            guard let mode = find(id, hiDPI: Int(n[1]), Int(n[2])) else { return 3 }
            let origin = n.count >= 5 ? (Int32(n[3]), Int32(n[4])) : nil
            return configure(id, mode: mode, origin: origin)
        case "--phone-screen-mode":
            guard n.count == 7 else { return 2 }
            let want = ModeSnapshot(width: Int(n[1]), height: Int(n[2]), pixelWidth: Int(n[3]), pixelHeight: Int(n[4]),
                                    refresh: n[5], ioID: Int32(n[6]))
            let same = modes(id).filter { ModeSnapshot($0).same(as: want) }
            guard let mode = same.first(where: { $0.ioDisplayModeID == want.ioID }) ?? same.first(where: { $0.isUsableForDesktopGUI() }) ?? same.first
            else { return 3 }
            return configure(id, mode: mode, origin: nil)
        default:
            return 2
        }
    }

    private static func modes(_ id: CGDirectDisplayID) -> [CGDisplayMode] {
        let options = [kCGDisplayShowDuplicateLowResolutionModes: true] as CFDictionary
        return (CGDisplayCopyAllDisplayModes(id, options) as? [CGDisplayMode]) ?? []
    }

    private static func find(_ id: CGDirectDisplayID, hiDPI w: Int, _ h: Int) -> CGDisplayMode? {
        modes(id).first { $0.width == w && $0.height == h && $0.pixelWidth == 2 * w }
    }

    private static func configure(_ id: CGDirectDisplayID, mode: CGDisplayMode, origin: (Int32, Int32)?) -> Int32 {
        var config: CGDisplayConfigRef?
        guard CGBeginDisplayConfiguration(&config) == .success else { return 4 }
        guard CGConfigureDisplayWithDisplayMode(config, id, mode, nil) == .success else {
            CGCancelDisplayConfiguration(config)
            return 4
        }
        if let origin { CGConfigureDisplayOrigin(config, id, origin.0, origin.1) }
        let done = CGCompleteDisplayConfiguration(config, .forSession)
        if done != .success { CGCancelDisplayConfiguration(config) }
        return done == .success ? 0 : 5
    }
}
