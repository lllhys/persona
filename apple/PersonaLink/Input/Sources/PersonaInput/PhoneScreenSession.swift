import AppKit
import PersonaInputKit

// Phone Screen's session (#9314, docs/extensions/computer-use.md "Phone Screen"): one live screen's virtual display
// and the windows put on it, driven by the node over the helper's socket (JSON lines: PhoneScreenRequest in, one
// PhoneScreenReply out for each, in order). It ends, putting everything back, when the node closes the socket: the
// phone left the mode, its screen-sharing stream ended, or the node went away.

@MainActor
final class PhoneScreenSession {
    private var display: PhoneDisplay?
    /// The other displays' modes and the windows' frames before the display came, to set back what macOS moves.
    private var modesBefore: [CGDirectDisplayID: ModeSnapshot] = [:]
    private var framesBefore: [WindowKey: CGRect] = [:]
    private var restore = PhoneScreenRestore()
    /// The window on the display now.
    private var placed: WindowKey?

    init() {
        AX.configureTimeout()
    }

    /// What a session that died left to put back, read off the main thread before the first request is taken.
    func adopt(_ left: PhoneScreenRestore) {
        for r in left.records { restore.add(r) }
    }

    func handle(_ r: PhoneScreenRequest) -> PhoneScreenReply {
        guard AX.trusted else { return .failure("permission", "Persona Input is not allowed to control the computer (Accessibility)") }
        switch r.op {
        case "start": return start(width: r.width ?? 0, height: r.height ?? 0)
        case "windows": return PhoneScreenReply(ok: true, windows: windows())
        case "place":
            guard let pid = r.pid, let window = r.window else { return .failure("bad_request", "place needs pid and window") }
            return place(WindowKey(pid: pid, id: window))
        case "stop":
            end()
            return PhoneScreenReply(ok: true)
        default:
            return .failure("bad_request", "unknown op \(r.op)")
        }
    }

    // MARK: start

    private func start(width: Double, height: Double) -> PhoneScreenReply {
        let fit = PhoneScreenFit(viewWidth: width, viewHeight: height)
        if let d = display {
            if abs(d.fit.ratio - fit.ratio) > 0.01 {
                let had = placed
                if let had { putBack(had) }
                guard d.reshape(fit) else { return .failure("display", "the display could not take the new shape") }
                if let had { _ = place(had) }
            }
            return started(d)
        }
        putBackLeftovers() // windows a session that died left on its display
        modesBefore = Displays.modes()
        framesBefore = Dictionary(WindowServer.onScreen().map { ($0.key, $0.frame) }, uniquingKeysWith: { a, _ in a })
        let d = PhoneDisplay(fit: fit)
        do {
            try d.create()
        } catch PhoneScreenError.display(let why) {
            _ = d.release()
            settle()
            return .failure("display", why)
        } catch {
            return .failure("display", "\(error)")
        }
        display = d
        settle()
        return started(d)
    }

    private func started(_ d: PhoneDisplay) -> PhoneScreenReply {
        PhoneScreenReply(ok: true, display: d.rect, displays: Displays.arrangement(known: [d.id: 2]))
    }

    // MARK: windows

    /// Standard windows of the apps in the Dock (minimized ones too), front to back as on screen, then the rest.
    private func windows() -> [PhoneScreenWindow] {
        let order = Dictionary(WindowServer.onScreen().enumerated().map { ($1.key, $0) }, uniquingKeysWith: { a, _ in a })
        let me = ProcessInfo.processInfo.processIdentifier
        var out: [(Int, PhoneScreenWindow)] = []
        for app in NSWorkspace.shared.runningApplications where app.activationPolicy == .regular && app.processIdentifier != me {
            let pid = app.processIdentifier
            for e in AX.windows(pid: pid) ?? [] where AX.isStandard(e) {
                guard let id = AX.windowID(e) else { continue }
                let key = WindowKey(pid: pid, id: id)
                let w = PhoneScreenWindow(pid: pid, window: id, app: app.localizedName ?? app.bundleIdentifier ?? "?",
                                          bundle: app.bundleIdentifier, title: AX.string(e, kAXTitleAttribute) ?? "",
                                          minimized: AX.flag(e, kAXMinimizedAttribute) ? true : nil,
                                          placed: key == placed ? true : nil, frame: AX.frame(e)?.phoneRect)
                out.append((order[key] ?? Int.max, w))
            }
        }
        return out.sorted { ($0.0, $0.1.app, $0.1.window) < ($1.0, $1.1.app, $1.1.window) }.map(\.1)
    }

    private func place(_ key: WindowKey) -> PhoneScreenReply {
        guard let d = display else { return .failure("no_display", "start the phone screen first") }
        guard let e = AX.element(key) else {
            return .failure(NSRunningApplication(processIdentifier: key.pid) == nil ? "window_gone" : "not_responding",
                            "the window is gone or its app does not answer")
        }
        if let old = placed, old != key { putBack(old) }
        if restore.record(pid: key.pid, window: key.id) == nil {
            guard let frame = AX.frame(e) else { return .failure("not_responding", "the window's app does not answer") }
            let app = NSRunningApplication(processIdentifier: key.pid)?.localizedName ?? ""
            restore.add(.init(pid: key.pid, window: key.id, app: app, frame: frame.phoneRect,
                              minimized: AX.flag(e, kAXMinimizedAttribute), fullScreen: AX.flag(e, AX.fullScreen)))
            guard save() else {
                restore.remove(pid: key.pid, window: key.id)
                return .failure("display", "could not write where the window was, so it was not moved")
            }
        }
        AX.setFullScreen(e, false)
        AX.setMinimized(e, false)
        // Every window starts from the base width: one that needed a wider mode must not leave the next one small.
        if d.width != d.fit.baseWidth { _ = d.set(width: d.fit.baseWidth, place: false) }
        var got = fill(e, d)
        // An app with a minimum size stays larger: a wider mode of the same shape holds it, and the picture scales.
        if let g = got {
            let area = d.area()
            let need = d.fit.width(toFit: PhoneScreenSize(width: Int(g.width.rounded()), height: Int(g.height.rounded())),
                                   in: PhoneScreenSize(width: Int(area.width), height: Int(area.height)), at: d.width)
            if need != d.width, d.set(width: need, place: false) { got = fill(e, d) }
        } else if d.width != d.fit.baseWidth, d.set(width: d.fit.baseWidth, place: false) {
            got = fill(e, d)
        }
        AX.focus(e, pid: key.pid)
        placed = key
        let app = NSRunningApplication(processIdentifier: key.pid)
        // Terminals and editors: bigger text while on the phone (PhoneScreenZoom), noted before the keys go so a crash
        // still undoes it.
        // Only once the record with the steps is on disk (#9540): a failed save sends no key.
        PhoneScreenZoom.raise(&restore, pid: key.pid, window: key.id, steps: PhoneScreenZoom.steps(for: app?.bundleIdentifier),
                              save: { RestoreFile.save($0) }, send: { AX.zoom(pid: key.pid, steps: $0) })
        let w = PhoneScreenWindow(pid: key.pid, window: key.id, app: app?.localizedName ?? "", bundle: app?.bundleIdentifier,
                                  title: AX.string(e, kAXTitleAttribute) ?? "", placed: true, frame: got?.phoneRect)
        // The arrangement too: the display may have changed width, and the phone finds it on the picture by all of them.
        return PhoneScreenReply(ok: true, display: d.rect, displays: Displays.arrangement(known: [d.id: 2]), placed: w)
    }

    /// The window over the display's area (a fitted window first goes back to the base width).
    private func fill(_ e: AXUIElement, _ d: PhoneDisplay) -> CGRect? {
        AX.move(e, to: d.area())
    }

    /// One window back where its record says, then the record goes.
    private func putBack(_ key: WindowKey) {
        defer { if placed == key { placed = nil } }
        guard let r = restore.record(pid: key.pid, window: key.id) else { return }
        if let e = AX.element(key) {
            let undo = PhoneScreenZoom.undo(r)
            if undo != 0 { // the text size back first: the keys go to the app's front window
                AXUIElementPerformAction(e, kAXRaiseAction as CFString)
                AX.set(e, kAXMainAttribute, kCFBooleanTrue)
                AX.zoom(pid: key.pid, steps: undo)
            }
            AX.move(e, to: CGRect(r.frame))
            if r.fullScreen { AX.setFullScreen(e, true) }
            if r.minimized { AX.setMinimized(e, true) }
        } else if NSRunningApplication(processIdentifier: key.pid) != nil, AX.windows(pid: key.pid) == nil {
            return // its app does not answer: keep the record for the next try
        } else if PhoneScreenZoom.undo(r) != 0, let app = NSRunningApplication(processIdentifier: key.pid),
                  PhoneScreenZoom.appWide.contains(app.bundleIdentifier ?? "") {
            AX.zoom(pid: key.pid, steps: PhoneScreenZoom.undo(r)) // the window is gone, but the app's zoom is the app's
        }
        restore.remove(pid: key.pid, window: key.id)
        _ = save()
    }

    private func putBackLeftovers() {
        for r in restore.records { putBack(WindowKey(pid: r.pid, id: r.window)) }
    }

    /// `--phone-screen-restore` (#9500): what a helper that died left on its display, put back without a session (the
    /// node runs it when it starts, and after a session ended with records still there). How many records it handled
    /// and how many it kept (an app that didn't answer).
    func restoreOnly() -> (done: Int, kept: Int) {
        let had = restore.records.count
        putBackLeftovers()
        return (had - restore.records.count, restore.records.count)
    }

    // MARK: end

    /// Windows back, the display gone, then whatever macOS moved set back. Safe to call twice.
    func end() {
        putBackLeftovers()
        placed = nil
        guard let d = display else { return }
        display = nil
        d.release()
        settle()
        modesBefore = [:]
        framesBefore = [:]
    }

    /// After the combination of displays changed: other displays' modes back as they were, then windows macOS moved.
    private func settle() {
        Displays.poll(timeout: 0.5) { false } // macOS moves windows and modes just after the change
        let now = Displays.modes()
        for (id, was) in modesBefore {
            guard let is_ = now[id], !is_.same(as: was) else { continue }
            _ = Child.run(["--phone-screen-mode", "\(id)"] + was.arguments)
        }
        let placedKey = placed
        for entry in WindowServer.onScreen() {
            guard entry.key != placedKey, restore.record(pid: entry.key.pid, window: entry.key.id) == nil,
                  let was = framesBefore[entry.key], !entry.frame.near(was), let e = AX.element(entry.key) else { continue }
            AX.move(e, to: was)
        }
    }

    // MARK: records

    private func save() -> Bool { RestoreFile.save(restore) }
}

/// Where windows were before they went on the display (PhoneScreenRestore), so a helper that died can put them back:
/// `~/.persona/phone-screen.json`, gone while there is nothing to put back. A few hundred bytes, read once a session and
/// written before each move (the move must not happen without it).
nonisolated enum RestoreFile {
    static let store = PhoneScreenRestoreStore(
        url: FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".persona/phone-screen.json"))

    static func load() -> PhoneScreenRestore { store.readRecords() }

    static func save(_ restore: PhoneScreenRestore) -> Bool { store.writeRecords(restore) }
}

/// `--phone-screen-restore`: put back what the records say, then exit (0, or 1 without Accessibility). The records are
/// read off the main thread, the windows moved on it.
enum PhoneScreenRestoreRun {
    static func run() -> Never {
        DispatchQueue.global().async {
            let left = RestoreFile.load()
            DispatchQueue.main.async {
                guard !left.records.isEmpty else {
                    print("phone_screen_restore: nothing")
                    exit(0)
                }
                guard AX.trusted else {
                    print("phone_screen_restore: no accessibility")
                    exit(1)
                }
                let r = MainActor.assumeIsolated { () -> (done: Int, kept: Int) in
                    let s = PhoneScreenSession()
                    s.adopt(left)
                    return s.restoreOnly()
                }
                print("phone_screen_restore: put back \(r.done), kept \(r.kept)")
                exit(0)
            }
        }
        RunLoop.main.run()
        exit(0)
    }
}

/// The socket side: token and permission lines, then requests read on a thread of their own and answered on the main
/// thread (the display and Accessibility live there), until the node closes the socket.
enum PhoneScreenServer {
    static func run(socket path: String, token: String) -> Never {
        let socket: UnixSocket
        do {
            socket = try UnixSocket.connect(path: path)
            try socket.write(Array((token + "\n").utf8))
            try socket.write(Array(((AX.trusted ? "granted" : "missing") + "\n").utf8))
        } catch {
            warn("phone screen: \(error)")
            exit(1)
        }
        let session = MainActor.assumeIsolated { PhoneScreenSession() }
        let finish: @Sendable () -> Void = {
            DispatchQueue.main.async {
                MainActor.assumeIsolated { session.end() }
                socket.close()
                exit(0)
            }
        }
        for sig in [SIGTERM, SIGINT, SIGHUP] {
            signal(sig, SIG_IGN)
            let source = DispatchSource.makeSignalSource(signal: sig, queue: .main)
            source.setEventHandler(handler: finish)
            source.resume()
            signals.append(source)
        }
        let reader: @Sendable () -> Void = {
            var pending = [UInt8]()
            while true {
                guard let chunk = try? socket.read(), !chunk.isEmpty else { break }
                pending += chunk
                while let nl = pending.firstIndex(of: 0x0A) {
                    let line = Data(pending[..<nl])
                    pending.removeSubrange(...nl)
                    let reply: PhoneScreenReply
                    if let req = try? JSONDecoder().decode(PhoneScreenRequest.self, from: line) {
                        reply = DispatchQueue.main.sync { MainActor.assumeIsolated { session.handle(req) } }
                    } else {
                        reply = .failure("bad_request", "not a request")
                    }
                    var out = (try? JSONEncoder().encode(reply)) ?? Data(#"{"ok":false,"error":"bad_request"}"#.utf8)
                    out.append(0x0A)
                    guard (try? socket.write(Array(out))) != nil else { finish(); return }
                }
            }
            finish()
        }
        // The records a session that died left, read off the main thread; requests are read only once they are adopted.
        DispatchQueue.global().async {
            let left = RestoreFile.load()
            DispatchQueue.main.async {
                MainActor.assumeIsolated { session.adopt(left) }
                Thread(block: reader).start()
            }
        }
        RunLoop.main.run()
        exit(0)
    }

    nonisolated(unsafe) private static var signals: [DispatchSourceSignal] = []

    private static func warn(_ text: String) {
        FileHandle.standardError.write(Data("persona-input: \(text)\n".utf8))
    }
}
