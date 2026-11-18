import Foundation
import PersonaKit
import PersonaWire

/// A link to a port on the machine a conversation runs on (docs/operations/ports.md「Recognizing links in replies」): what an agent prints for the dev
/// server it started — http://localhost:5173/, http://127.0.0.1:8000/docs, http://0.0.0.0:3000, http://[::1]:4173,
/// or the machine's own name — opened through the hub, never on the phone's own localhost.
nonisolated struct PortLink: Hashable, Sendable {
    var port: Int
    /// Path, query and fragment ("/" at least).
    var path: String
    var secure: Bool
}

nonisolated enum PortLinks {
    /// Hosts that always mean "this machine".
    private static let loopback: Set<String> = ["localhost", "127.0.0.1", "0.0.0.0", "::1", "::", "[::1]", "[::]"]

    /// The port link a URL is, if it names this machine (`localhost`, a loopback address, or one of `hostnames`, the
    /// machine's name with or without .local) with an explicit port.
    static func link(_ url: URL, hostnames: [String] = []) -> PortLink? {
        guard let scheme = url.scheme?.lowercased(), scheme == "http" || scheme == "https",
              let host = url.host(percentEncoded: false)?.lowercased(), let port = url.port, (1...65535).contains(port)
        else { return nil }
        guard loopback.contains(host) || host.hasSuffix(".localhost") || names(hostnames).contains(host) else { return nil }
        var comps = URLComponents(url: url, resolvingAgainstBaseURL: false)
        comps?.scheme = nil
        comps?.host = nil
        comps?.port = nil
        comps?.user = nil
        comps?.password = nil
        var path = comps?.string ?? "/"
        if !path.hasPrefix("/") { path = "/" + path }
        return PortLink(port: port, path: path, secure: scheme == "https")
    }

    /// The port links in text, in order, one per port and path (memoized: rows ask on every redraw).
    static func find(in text: String, hostnames: [String] = []) -> [PortLink] {
        guard text.contains(":") else { return [] }
        let key = hostnames.isEmpty ? text : hostnames.joined(separator: ",") + "\u{0}" + text
        return memo(key) { _ in scan(text, hostnames: hostnames) }
    }

    private static let memo = TextMemo<[PortLink]>()

    /// Bare `localhost:3000/x` (no scheme), as servers often print it.
    private static let bare = try! NSRegularExpression(
        pattern: #"(?<![A-Za-z0-9_./:@\-])(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):(\d{1,5})(/[A-Za-z0-9_.~!$&'()*+,;=:@%/?#\-]*)?"#)

    private static func scan(_ text: String, hostnames: [String]) -> [PortLink] {
        var out: [PortLink] = []
        func add(_ l: PortLink) { if !out.contains(l) { out.append(l) } }
        let ns = text as NSString
        var covered: [NSRange] = []
        for (r, url) in Links.scan(text) {
            covered.append(r)
            if let l = link(url, hostnames: hostnames) { add(l) }
        }
        for m in bare.matches(in: text, range: NSRange(location: 0, length: ns.length))
        where !covered.contains(where: { NSIntersectionRange($0, m.range).length > 0 }) {
            var s = ns.substring(with: m.range)
            while let last = s.last, ".,;:!?)".contains(last) { s.removeLast() }
            if let url = URL(string: "http://" + s), let l = link(url) { add(l) }
        }
        return out
    }

    private static func names(_ hostnames: [String]) -> Set<String> {
        var out = Set<String>()
        for h in hostnames where !h.isEmpty {
            let n = h.lowercased()
            out.insert(n)
            if n.hasSuffix(".local") { out.insert(String(n.dropLast(6))) } else if !n.contains(".") { out.insert(n + ".local") }
        }
        return out
    }
}
