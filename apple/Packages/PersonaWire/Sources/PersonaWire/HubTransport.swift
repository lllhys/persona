import Foundation

/// Where requests for a hub actually go (docs/operations/network.md「Connecting to hub through SSH」). A hub the app reaches through its SSH
/// tunnel keeps its own address everywhere — in the registry, in `LiveConnection.currentConfig`, in every comparison —
/// and only the URLs put on the wire (`APIClient`, the socket, port forwards) are turned into the tunnel's loopback
/// address here. Nothing routed: every URL is what it was.
///
/// The app sets the routes (SSHTunnels); everything else only reads them, from any thread. The watch and the
/// extensions never route anything.
public nonisolated enum HubTransport {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var routes: [String: URL] = [:]
    /// When each hub's requests began to go through the tunnel they go through now (`since`).
    nonisolated(unsafe) private static var routedAt: [String: Date] = [:]

    /// A hub address as the routes key it: without the trailing slash.
    public static func key(_ url: URL) -> String {
        url.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
    }

    /// The tunnel's address for requests to `hub` from now on; nil: straight to the hub again.
    public static func route(_ hub: URL, via local: URL?) {
        lock.lock()
        defer { lock.unlock() }
        let k = key(hub)
        if routes[k] != local { routedAt[k] = local == nil ? nil : Date() }
        routes[k] = local
    }

    /// Requests to `hub` go straight to it again, unless another tunnel routes them by now: a hub can have more than
    /// one way through, and closing one must not take the route of the one that replaced it.
    public static func unroute(_ hub: URL, from local: URL) {
        lock.lock()
        defer { lock.unlock() }
        if routes[key(hub)] == local {
            routes[key(hub)] = nil
            routedAt[key(hub)] = nil
        }
    }

    /// The tunnel requests to `hub` go through, nil when they go straight to it.
    public static func via(_ hub: URL) -> URL? {
        lock.lock()
        defer { lock.unlock() }
        return routes[key(hub)]
    }

    /// When requests to `hub` began to go through the tunnel they go through now; nil when they go straight to it. A
    /// tunnel's own connection stays on the network it was made on (hub transcription's Wi-Fi rule,
    /// `MeetingTranscriptionRule.mayStream`).
    public static func since(_ hub: URL) -> Date? {
        lock.lock()
        defer { lock.unlock() }
        return routes[key(hub)] == nil ? nil : routedAt[key(hub)]
    }

    public static func clearAll() {
        lock.lock()
        defer { lock.unlock() }
        routes = [:]
        routedAt = [:]
    }

    /// `url` (the hub's address or anything under it) as it is reached: the tunnel's scheme, host and port when the hub
    /// is routed through one, the path and query kept.
    public static func resolve(_ url: URL, hub: URL) -> URL {
        guard let local = via(hub), var comps = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let target = URLComponents(url: local, resolvingAgainstBaseURL: false) else { return url }
        // A WebSocket keeps its kind: ws(s) to the hub is ws through the tunnel (the tunnel itself is plain TCP).
        let ws = comps.scheme == "ws" || comps.scheme == "wss"
        comps.scheme = ws ? (target.scheme == "https" ? "wss" : "ws") : target.scheme
        comps.host = target.host
        comps.port = target.port
        return comps.url ?? url
    }
}

/// A paired hub's SSH tunnel (Settings › hub › SSH Tunnel): the server, who logs in, where the hub is from there, and the
/// server's key the user confirmed. Kept with the pairing (the keychain); this device's own key is kept apart
/// (SSHKeys), one per device.
public nonisolated struct SSHRoute: Codable, Hashable, Sendable {
    public var host: String
    public var port: Int = 22
    /// As typed; a Warpgate login (`user:target`) included.
    public var user: String
    /// The hub's address as the SSH server sees it.
    public var targetHost: String = SSHRoute.defaultTargetHost
    public var targetPort: Int = SSHRoute.defaultTargetPort
    /// `SHA256:…` of the server's key, once the user confirmed it; nil before (the tunnel is not used then).
    public var hostKey: String?
    /// The key's algorithm as OpenSSH prints it (`ED25519`), for showing the fingerprint.
    public var hostKeyAlgorithm: String?
    /// Through the tunnel every time, not only when the hub's own addresses do not answer.
    public var always = false
    /// The SSH server is dialled through the in-app tunnel (a loopback forward to `host:port` through its core), not
    /// over this device's own network; its key is still pinned for `host:port` (docs/operations/network.md「Connecting to hub through SSH」).
    public var viaInAppTunnel = false

    public static let defaultTargetHost = "127.0.0.1"
    public static let defaultTargetPort = 18660

    public init(host: String, port: Int = 22, user: String, targetHost: String = SSHRoute.defaultTargetHost,
         targetPort: Int = SSHRoute.defaultTargetPort, hostKey: String? = nil, hostKeyAlgorithm: String? = nil, always: Bool = false,
         viaInAppTunnel: Bool = false) {
        self.host = host
        self.port = port
        self.user = user
        self.targetHost = targetHost
        self.targetPort = targetPort
        self.hostKey = hostKey
        self.hostKeyAlgorithm = hostKeyAlgorithm
        self.always = always
        self.viaInAppTunnel = viaInAppTunnel
    }

    /// Ready to carry traffic: a server, a user and a confirmed key.
    public var isUsable: Bool { !host.isEmpty && !user.isEmpty && (1...65535).contains(port) && hostKey?.isEmpty == false }

    /// `user@host` (`:port` when not 22), what lists show.
    public var label: String { "\(user)@\(host)" + (port == 22 ? "" : ":\(port)") }

    /// The same server, user and target: a confirmed key still belongs to it. Another server needs its key confirmed.
    public func sameServer(_ other: SSHRoute) -> Bool { host == other.host && port == other.port }

    /// The SSH server as the in-app tunnel core's forward takes it (`host:port`, IPv6 in brackets).
    public var server: String { InAppTunnelRoute(targetHost: host, targetPort: port).target }

    public enum CodingKeys: String, CodingKey {
        case host, port, user, always
        case targetHost = "target_host", targetPort = "target_port", hostKey = "host_key", hostKeyAlgorithm = "host_key_algorithm"
        case viaInAppTunnel = "via_in_app_tunnel"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.decode(String.self, forKey: .host)
        user = try c.decode(String.self, forKey: .user)
        port = try c.decodeIfPresent(Int.self, forKey: .port) ?? 22
        targetHost = try c.decodeIfPresent(String.self, forKey: .targetHost) ?? Self.defaultTargetHost
        targetPort = try c.decodeIfPresent(Int.self, forKey: .targetPort) ?? Self.defaultTargetPort
        hostKey = try c.decodeIfPresent(String.self, forKey: .hostKey)
        hostKeyAlgorithm = try c.decodeIfPresent(String.self, forKey: .hostKeyAlgorithm)
        always = try c.decodeIfPresent(Bool.self, forKey: .always) ?? false
        viaInAppTunnel = try c.decodeIfPresent(Bool.self, forKey: .viaInAppTunnel) ?? false
    }
}

/// A paired hub's in-app tunnel (Settings › hub › the tunnel's name, docs/operations/network.md「Connecting to hub through an in-app tunnel」): what the hub's
/// tunnel extension says (its name, where the hub is from inside the network the tunnel reaches) and whether to go
/// through it every time. The account the tunnel signs in with is the core's own (one per device, in the app's
/// container), not part of the pairing.
public nonisolated struct InAppTunnelRoute: Codable, Hashable, Sendable {
    /// The hub's address as the tunnel's network sees it: a host name (resolved by that network's DNS) or an IP.
    public var targetHost: String
    public var targetPort: Int = InAppTunnelRoute.defaultTargetPort
    /// The name the hub's extension gives the tunnel (contributes.tunnels[].name); empty when it gave none.
    public var name: String = ""
    /// Through the tunnel every time, not only when the hub's own addresses do not answer.
    public var always = false
    /// The hub machine's SSH port in the tunnel's network (the extension's `ssh_port`); nil when it gave none. An SSH
    /// tunnel dialled through this tunnel probes `targetHost:sshPort`, which answers when the hub listens only on loopback.
    public var sshPort: Int?

    public static let defaultTargetPort = 18660

    public init(targetHost: String, targetPort: Int = InAppTunnelRoute.defaultTargetPort, name: String = "", always: Bool = false,
         sshPort: Int? = nil) {
        self.targetHost = targetHost
        self.targetPort = targetPort
        self.name = name
        self.always = always
        self.sshPort = sshPort
    }

    /// What the core probes for an SSH tunnel dialled through this tunnel: the hub machine's SSH port when the
    /// extension gave one, else the route as it is (the direct in-app tunnel always uses `targetPort`).
    public var sshProbe: InAppTunnelRoute {
        guard let sshPort else { return self }
        var r = self
        r.targetPort = sshPort
        return r
    }

    public var isUsable: Bool { !targetHost.trimmingCharacters(in: .whitespaces).isEmpty && (1...65535).contains(targetPort) }

    /// What the app calls the tunnel: the hub's name for it, else 「内网隧道」.
    public var title: String {
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return n.isEmpty ? String(localized: "Tunnel [Hub Transport e248d4]") : n
    }

    /// `host:port` as the core's forward takes it (IPv6 in brackets).
    public var target: String {
        let host = targetHost.trimmingCharacters(in: .whitespaces)
        return (host.contains(":") ? "[\(host)]" : host) + ":\(targetPort)"
    }

    public enum CodingKeys: String, CodingKey {
        case always, name
        case targetHost = "target_host", targetPort = "target_port", sshPort = "ssh_port"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        targetHost = try c.decode(String.self, forKey: .targetHost)
        targetPort = try c.decodeIfPresent(Int.self, forKey: .targetPort) ?? Self.defaultTargetPort
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        always = try c.decodeIfPresent(Bool.self, forKey: .always) ?? false
        sshPort = try c.decodeIfPresent(Int.self, forKey: .sshPort)
    }
}

/// A paired hub's local proxy (Settings › hub › Local Proxy, docs/operations/network.md "Connecting to hub through a
/// local SOCKS5 proxy"): a SOCKS5 proxy on this device that another app serves, the login it may ask for (RFC 1929),
/// and the hub's address as the proxy's network sees it. Kept with the pairing (the keychain), the password included.
public nonisolated struct LocalProxyRoute: Codable, Hashable, Sendable {
    public var host: String = LocalProxyRoute.defaultHost
    public var port: Int
    /// Empty: the proxy asks for no login.
    public var username: String = ""
    public var password: String = ""
    /// The hub's host as the proxy's network sees it; nil: the paired address's.
    public var targetHost: String?
    /// The hub's HTTP port as the proxy's network sees it; nil: the paired address's (`targetPort(for:)`).
    public var targetPort: Int?
    /// Through the proxy every time, not only when the hub's own addresses do not answer.
    public var always = false

    public static let defaultHost = "127.0.0.1"
    /// The hub's HTTP port (hub.json `listen`), what an https address is reached at through the proxy: the forward
    /// carries plain HTTP, as the SSH and in-app tunnels do.
    public static let hubHTTPPort = 18660

    public init(host: String = LocalProxyRoute.defaultHost, port: Int, username: String = "", password: String = "",
         targetHost: String? = nil, targetPort: Int? = nil, always: Bool = false) {
        self.host = host
        self.port = port
        self.username = username
        self.password = password
        self.targetHost = targetHost
        self.targetPort = targetPort
        self.always = always
    }

    public var isUsable: Bool {
        !host.trimmingCharacters(in: .whitespaces).isEmpty && (1...65535).contains(port)
            && targetPort.map { (1...65535).contains($0) } ?? true
    }

    /// `host:port` of the proxy, what lists show.
    public var label: String {
        let h = host.trimmingCharacters(in: .whitespaces)
        return (h.contains(":") ? "[\(h)]" : h) + ":\(port)"
    }

    /// The proxy is on this device (localhost, 127.x.x.x, ::1). Anywhere else, what goes through it (the hub's plain
    /// HTTP, this device's credential, the proxy's password) crosses the network unencrypted.
    public var isOnThisDevice: Bool {
        let h = host.trimmingCharacters(in: .whitespaces).trimmingCharacters(in: CharacterSet(charactersIn: "[]")).lowercased()
        if h == "localhost" || h == "::1" { return true }
        let parts = h.split(separator: ".", omittingEmptySubsequences: false)
        return parts.count == 4 && parts.allSatisfy { UInt8($0) != nil } && parts[0] == "127"
    }

    /// The username to sign in with, nil when the proxy asks for none.
    public var login: (username: String, password: String)? {
        let u = username.trimmingCharacters(in: .whitespaces)
        return u.isEmpty ? nil : (u, password)
    }

    /// The hub's host the proxy is asked for: the one set here, else the paired address's.
    public func targetHost(for hub: URL) -> String {
        let t = targetHost?.trimmingCharacters(in: .whitespaces) ?? ""
        return t.isEmpty ? Self.host(of: hub) : t
    }

    /// The hub's port the proxy is asked for: the one set here, else the paired address's when it is http (80 when it
    /// names none), else the hub's HTTP port.
    public func targetPort(for hub: URL) -> Int {
        targetPort ?? Self.defaultTargetPort(for: hub)
    }

    public static func defaultTargetPort(for hub: URL) -> Int {
        guard hub.scheme?.lowercased() == "http" else { return hubHTTPPort }
        return hub.port ?? 80
    }

    /// A URL's host without the brackets of an IPv6 literal.
    public static func host(of url: URL) -> String {
        (url.host(percentEncoded: false) ?? "").trimmingCharacters(in: CharacterSet(charactersIn: "[]"))
    }

    public enum CodingKeys: String, CodingKey {
        case host, port, username, password, always
        case targetHost = "target_host", targetPort = "target_port"
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        host = try c.decodeIfPresent(String.self, forKey: .host) ?? Self.defaultHost
        port = try c.decode(Int.self, forKey: .port)
        username = try c.decodeIfPresent(String.self, forKey: .username) ?? ""
        password = try c.decodeIfPresent(String.self, forKey: .password) ?? ""
        targetHost = try c.decodeIfPresent(String.self, forKey: .targetHost)
        targetPort = try c.decodeIfPresent(Int.self, forKey: .targetPort)
        always = try c.decodeIfPresent(Bool.self, forKey: .always) ?? false
    }
}
