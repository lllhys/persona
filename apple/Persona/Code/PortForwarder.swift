import Foundation
import Network

/// A machine's port on the phone's loopback (docs/operations/ports.md「Forwarding」), the way Moshi forwards a dev server: the app
/// listens on 127.0.0.1 and ::1 at the same port number when it is free (another one when not), and each connection
/// the in-app browser makes there becomes one WebSocket to the hub (…/ports/{port}/connect, the device token), which
/// carries its bytes to the port on the machine. The page loads from http://localhost:<port>/ exactly as on the
/// machine: its absolute paths, its own WebSocket (HMR), redirects and cookies need nothing rewritten.
///
/// Listeners last while the app runs; one found broken (the app was suspended) is started again when next asked for.
@MainActor
final class PortForwarder {
    static let shared = PortForwarder()

    struct Key: Hashable, Sendable {
        var machine: String
        var port: Int
    }

    private var forwards: [Key: PortForward] = [:]
    private let queue = DispatchQueue(label: "persona.ports")

    /// The phone's port for a machine's port, listening from now on.
    func localPort(_ key: Key, config: ServerConfig) async throws -> Int {
        if let f = forwards[key], f.isReady { return f.localPort }
        forwards[key]?.stop()
        forwards[key] = nil
        let taken = Set(forwards.values.map(\.localPort))
        let f = PortForward(request: Self.connectRequest(key, config: config), queue: queue)
        try await f.start(preferring: taken.contains(key.port) ? 0 : key.port)
        forwards[key] = f
        return f.localPort
    }

    /// The address to open a port link at on the phone.
    func url(for link: PortLink, machine: String, config: ServerConfig) async throws -> URL {
        let port = try await localPort(Key(machine: machine, port: link.port), config: config)
        guard let url = URL(string: "\(link.secure ? "https" : "http")://localhost:\(port)\(link.path)") else {
            throw URLError(.badURL)
        }
        return url
    }

    /// Which machine port a page on the phone's loopback is (the sheet names it).
    func key(for localURL: URL) -> Key? {
        guard let p = localURL.port, ["localhost", "127.0.0.1"].contains(localURL.host() ?? "") else { return nil }
        return forwards.first { $0.value.localPort == p }?.key
    }

    static func connectRequest(_ key: Key, config: ServerConfig) -> URLRequest {
        var comps = URLComponents(url: config.url, resolvingAgainstBaseURL: false)!
        var base = comps.percentEncodedPath
        if base.hasSuffix("/") { base.removeLast() }
        comps.percentEncodedPath = base + "/api/v1/machines/\(key.machine)/ports/\(key.port)/connect"
        comps.scheme = config.url.scheme == "https" ? "wss" : "ws"
        var req = URLRequest(url: HubTransport.resolve(comps.url!, hub: config.url), timeoutInterval: 15)
        config.authorize(&req)
        req.setValue(LanguageStore.shared.launched, forHTTPHeaderField: "Accept-Language")
        return req
    }
}

/// One forwarded port: its listeners and the connections through them.
nonisolated final class PortForward: @unchecked Sendable {
    private let request: URLRequest
    private let queue: DispatchQueue
    private let session: URLSession
    private var listeners: [NWListener] = []
    private(set) var localPort = 0

    init(request: URLRequest, queue: DispatchQueue) {
        self.request = request
        self.queue = queue
        let c = URLSessionConfiguration.default
        c.urlCache = nil
        session = URLSession(configuration: c)
    }

    var isReady: Bool { !listeners.isEmpty && listeners.allSatisfy { $0.state == .ready } }

    /// Listens on 127.0.0.1 at port (0: any free one), then on ::1 at the same one when it can (localhost is ::1
    /// first; without it WebKit falls back to 127.0.0.1).
    func start(preferring port: Int) async throws {
        let v4: NWListener
        do {
            v4 = try await listen("127.0.0.1", port)
        } catch where port != 0 {
            v4 = try await listen("127.0.0.1", 0) // taken on the phone: another number, as Moshi does
        }
        localPort = Int(v4.port?.rawValue ?? 0)
        listeners = [v4]
        if let v6 = try? await listen("::1", localPort) { listeners.append(v6) }
    }

    func stop() {
        listeners.forEach { $0.cancel() }
        listeners = []
    }

    private func listen(_ host: NWEndpoint.Host, _ port: Int) async throws -> NWListener {
        let params = NWParameters.tcp
        params.requiredLocalEndpoint = .hostPort(host: host, port: NWEndpoint.Port(rawValue: UInt16(port)) ?? .any)
        let l = try NWListener(using: params)
        l.newConnectionHandler = { @Sendable [weak self] conn in self?.accept(conn) }
        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<NWListener, Error>) in
            let once = RunOnce()
            l.stateUpdateHandler = { @Sendable state in
                switch state {
                case .ready: once.run { cont.resume(returning: l) }
                case .failed(let e), .waiting(let e):
                    once.run { cont.resume(throwing: e) }
                    l.cancel()
                case .cancelled: once.run { cont.resume(throwing: CancellationError()) }
                default: break
                }
            }
            l.start(queue: queue)
        }
    }

    private func accept(_ conn: NWConnection) {
        let ws = session.webSocketTask(with: request)
        ws.delegate = HubRedirects.shared
        ws.maximumMessageSize = 1 << 20
        Pump(conn: conn, ws: ws, queue: queue).start()
    }
}

/// One browser connection and its WebSocket to the hub, each read only once the last bytes went out the other side.
nonisolated private final class Pump: @unchecked Sendable {
    let conn: NWConnection
    let ws: URLSessionWebSocketTask
    let queue: DispatchQueue
    private let done = RunOnce()

    init(conn: NWConnection, ws: URLSessionWebSocketTask, queue: DispatchQueue) {
        self.conn = conn
        self.ws = ws
        self.queue = queue
    }

    func start() {
        conn.stateUpdateHandler = { @Sendable [self] state in
            if case .failed = state { close() }
        }
        conn.start(queue: queue)
        ws.resume()
        readLocal()
        readRemote()
    }

    private func readLocal() {
        conn.receive(minimumIncompleteLength: 1, maximumLength: 64 << 10) { @Sendable [self] data, _, complete, error in
            if let data, !data.isEmpty {
                ws.send(.data(data)) { @Sendable [self] err in
                    if err != nil || complete { close() } else { readLocal() }
                }
            } else if complete || error != nil {
                close()
            } else {
                readLocal()
            }
        }
    }

    private func readRemote() {
        ws.receive { @Sendable [self] result in
            let data: Data
            switch result {
            case .success(.data(let d)): data = d
            case .success(.string(let s)): data = Data(s.utf8)
            case .success: data = Data()
            case .failure:
                // The machine's side ended: what was sent before is already written, then the browser sees the end.
                conn.send(content: nil, contentContext: .finalMessage, isComplete: true, completion: .contentProcessed { @Sendable [self] _ in close() })
                return
            }
            conn.send(content: data, completion: .contentProcessed { @Sendable [self] err in
                if err != nil { close() } else { readRemote() }
            })
        }
    }

    private func close() {
        done.run {
            ws.cancel(with: .normalClosure, reason: nil)
            conn.cancel()
        }
    }
}

/// Runs its block the first time only, from any thread.
nonisolated final class RunOnce: @unchecked Sendable {
    private let lock = NSLock()
    private var ran = false

    func run(_ f: () -> Void) {
        lock.lock()
        let first = !ran
        ran = true
        lock.unlock()
        if first { f() }
    }
}
