import Foundation

/// Only the sent-ledger settings links supported by the native app. Other settings URLs keep their existing behavior.
nonisolated enum SentSettingsLink {
    static func matches(_ url: URL) -> Bool {
        guard HubNaming.accepts(scheme: url.scheme), url.host()?.lowercased() == "settings",
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return false }
        switch components.path {
        case "/sent", "/connectors/-sent": return true
        case "", "/":
            let query = components.queryItems ?? []
            let tab = query.first { $0.name == "tab" }?.value
            let connector = query.first { $0.name == "connector" }?.value
            return tab == "sent" || (tab == "connectors" && connector == "-sent")
        default: return false
        }
    }
}
