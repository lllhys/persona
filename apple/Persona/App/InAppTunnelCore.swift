#if canImport(PersonaTunnelCore)
import PersonaTunnelCore
#endif
import Foundation

/// The in-app tunnel core (docs/operations/network.md「Connecting to hub through an in-app tunnel」): a C library with the PersonaTunnel interface, linked
/// into device builds only when the build machine's config names one (`app.tunnel_core`, docs/operations/config.md). It runs the
/// tunnel's own control panel on loopback (where the user signs in and picks a node), a userspace tunnel (no system
/// VPN), and loopback port forwards through it; no proxy. One per process, started the first time it is needed and
/// never with auto-connect: the tunnel is up only while the app needs it. Its data (the login, this device's identity
/// and keys) is in Application Support/tunnel, out of backups. Every call blocks, so each runs off the main actor.
/// Builds without the core (the simulator, a machine with no `app.tunnel_core`): `available` is false and every call
/// fails.
nonisolated enum InAppTunnel {
    nonisolated struct Failure: LocalizedError, Equatable {
        let message: String
        var errorDescription: String? { message }
    }

    static let notSignedIn = Failure(message: String(localized: "The tunnel isn't signed in yet. Sign in under “Sign In and Pick a Node” first."))
    static let missing = Failure(message: String(localized: "This build of the app has no tunnel core"))

    /// The version of the C interface the app is written against (PersonaTunnelAPIVersion).
    static let apiVersion = 1

    #if canImport(PersonaTunnelCore)
    static let available = true
    #else
    static let available = false
    #endif

    /// PersonaTunnelStatus, as JSON.
    nonisolated struct Status: Decodable, Equatable, Sendable {
        var state: String
        var loggedIn: Bool
        var hasServer: Bool
        var serverName: String?
        var error: String?
        var needsMfa: Bool?

        var connected: Bool { state == "connected" }

        enum CodingKeys: String, CodingKey {
            case state, error
            case loggedIn = "logged_in", hasServer = "has_server", serverName = "server_name", needsMfa = "needs_mfa"
        }
    }

    private static let lock = NSLock()
    nonisolated(unsafe) private static var panelURL: URL?

    static var dataDir: URL {
        FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("tunnel", isDirectory: true)
    }

    /// The control panel's address (with its one-time token), starting the core first when it is not running. `probe`
    /// is what the core checks to tell whether the tunnel carries traffic: the hub's address in the tunnel's network.
    @concurrent static func start(probe route: InAppTunnelRoute) async throws -> URL {
        if let running = lock.withLock({ panelURL }) { return running }
        #if canImport(PersonaTunnelCore)
        guard Int(PersonaTunnelAPIVersion()) == apiVersion else {
            throw Failure(message: String(localized: "The tunnel core's version doesn't match the app"))
        }
        var dir = dataDir
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? dir.setResourceValues(values)
        let host = route.targetHost.trimmingCharacters(in: .whitespaces)
        let raw = try check(take(withC(dir.path) { d in
            withC(host) { h in withC(String(route.targetPort)) { p in PersonaTunnelStart(d, h, p) } }
        }))
        guard let url = URL(string: raw) else { throw Failure(message: String(localized: "The tunnel core returned an invalid address")) }
        lock.withLock { panelURL = url }
        return url
        #else
        throw missing
        #endif
    }

    /// The tunnel up, waiting up to `timeout` seconds; kept as it is when it is up already.
    @concurrent static func connect(_ route: InAppTunnelRoute, timeout: Int) async throws {
        _ = try await start(probe: route)
        #if canImport(PersonaTunnelCore)
        let r = take(PersonaTunnelConnect(Int32(timeout)))
        if r == "error: not signed in" { throw notSignedIn }
        _ = try check(r)
        #endif
    }

    /// The tunnel down (the core, its login and its forwards stay). Nothing when the core never started.
    @concurrent static func disconnect() async {
        guard started else { return }
        #if canImport(PersonaTunnelCore)
        PersonaTunnelDisconnect()
        #endif
    }

    /// What the core says about its login and tunnel; nil when it is not running.
    @concurrent static func status() async -> Status? {
        guard started else { return nil }
        #if canImport(PersonaTunnelCore)
        guard let raw = try? check(take(PersonaTunnelStatus())) else { return nil }
        return try? JSONDecoder().decode(Status.self, from: Data(raw.utf8))
        #else
        return nil
        #endif
    }

    /// A loopback forward to `target` (`host:port`) through the tunnel, on `port` when that is free (0: any); its
    /// address. `probe` starts the core when it is not running: always a hub's own in-app route, never the target of a
    /// forward that is not a hub (an SSH server's), since the core keeps the first probe it is given.
    @concurrent static func forward(_ target: String, port: Int, probe: InAppTunnelRoute) async throws -> URL {
        _ = try await start(probe: probe)
        #if canImport(PersonaTunnelCore)
        let addr = try check(take(withC(target) { PersonaTunnelForward($0, Int32(port)) }))
        guard let url = URL(string: "http://\(addr)") else { throw Failure(message: String(localized: "The tunnel core returned an invalid address")) }
        return url
        #else
        throw missing
        #endif
    }

    @concurrent static func stopForward(_ target: String) async {
        guard started else { return }
        #if canImport(PersonaTunnelCore)
        withC(target) { PersonaTunnelStopForward($0) }
        #endif
    }

    /// Text the user brought back from a sign-in page outside the app (a link they copied): handed to the core, which
    /// takes it when it finishes its sign-in. True when it did.
    @concurrent static func offerLink(_ text: String) async -> Bool {
        guard started else { return false }
        #if canImport(PersonaTunnelCore)
        return (try? check(take(withC(text) { PersonaTunnelOfferLink($0) }))) == "accepted"
        #else
        return false
        #endif
    }

    private static var started: Bool { lock.withLock { panelURL != nil } }

    #if canImport(PersonaTunnelCore)
    private static func withC<R>(_ s: String, _ body: (UnsafeMutablePointer<CChar>) -> R) -> R {
        let p = strdup(s)!
        defer { free(p) }
        return body(p)
    }

    /// A string the core returned, freed.
    private static func take(_ p: UnsafeMutablePointer<CChar>?) -> String {
        guard let p else { return "error: no result" }
        defer { PersonaTunnelFree(p) }
        return String(cString: p)
    }

    /// The core's "error: …" as a Failure; anything else as it is.
    private static func check(_ s: String) throws -> String {
        guard s.hasPrefix("error:") else { return s }
        throw Failure(message: String(s.dropFirst("error:".count)).trimmingCharacters(in: .whitespaces))
    }
    #endif
}
