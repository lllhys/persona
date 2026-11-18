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

/// `persona://iphone/setup`: this iPhone's guided setup (#8199), the link a failed iPhone tool call has the assistant give.
nonisolated enum IPhoneSetupLink {
    static func matches(_ url: URL) -> Bool {
        HubNaming.accepts(scheme: url.scheme) && url.host()?.lowercased() == "iphone"
            && ["/setup", "/setup/"].contains(url.path().lowercased())
    }
}
