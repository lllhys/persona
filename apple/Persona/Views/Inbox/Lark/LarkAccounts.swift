import CryptoKit
import Foundation
import Observation
import PersonaWire

/// Which of the hub's Feishu accounts the Messages page shows (docs/extensions/feishu.md「Multiple accounts」). The hub's own account
/// (lark.account) is what requests without `ol_account` are about; another one is named on each request.
/// The last list and choice are kept per hub, so the chips are there at once on the next launch.
@MainActor @Observable
final class LarkAccountChoice {
    nonisolated struct Saved: Codable, Sendable {
        var list: APIClient.LarkAccounts?
        var selected: Int?
    }

    struct Deps {
        var config: ServerConfig
        var accounts: () async throws -> APIClient.LarkAccounts
        var cached: () -> Saved?
        var save: (Saved) -> Void

        static func live(_ config: ServerConfig) -> Self {
            let api = APIClient(config: config)
            let name = "lark-accounts-" + SHA256.hash(data: Data((config.url.absoluteString + "\n" + config.token).utf8))
                .map { String(format: "%02x", $0) }.joined()
            return Self(config: config, accounts: { try await api.larkAccounts() },
                        cached: { StateCache.load(name, as: Saved.self) }, save: { StateCache.save(name, $0) })
        }
    }

    /// The signed-in accounts, in the Lark gateway's order; empty from a hub that has no account list (one account).
    private(set) var accounts: [LarkAccount] = []
    private(set) var defaultID: Int?
    private var selected: Int?
    @ObservationIgnored private let deps: Deps

    var config: ServerConfig { deps.config }

    init(deps: Deps) {
        self.deps = deps
        if let saved = deps.cached() {
            selected = saved.selected
            if let list = saved.list { take(list) }
        }
    }

    /// The account chosen; the hub's own until another is picked (or when the chosen one is gone).
    var selectedID: Int? {
        if let selected, accounts.contains(where: { $0.id == selected }) { return selected }
        return defaultID
    }

    /// What the Messages page names as `ol_account`: nil for the hub's own account.
    var requestAccount: Int? {
        guard let id = selectedID, id != defaultID else { return nil }
        return id
    }

    /// Whose chats they are, shown in a chat's subtitle while more than one account is signed in.
    var selectedName: String? {
        guard accounts.count > 1 else { return nil }
        return accounts.first { $0.id == selectedID }?.title
    }

    func select(_ id: Int) {
        guard id != selectedID else { return }
        selected = id
        persist()
    }

    /// Reads the list again (unread counts change with every message). A hub from before several accounts answers
    /// 404: no chips.
    func load() async {
        do {
            take(try await deps.accounts())
        } catch let error as APIError where error.status == 404 {
            listed = nil
            accounts = []
            defaultID = nil
        } catch {
            return
        }
        persist()
    }

    @ObservationIgnored private var listed: APIClient.LarkAccounts?

    private func take(_ list: APIClient.LarkAccounts) {
        listed = list
        let signedIn = list.accounts.filter { $0.signedIn && $0.quarantined != true }
        if signedIn != accounts { accounts = signedIn }
        if defaultID != list.default { defaultID = list.default }
    }

    private func persist() {
        deps.save(Saved(list: listed, selected: selected))
    }
}
