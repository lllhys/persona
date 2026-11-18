import SwiftUI

/// This iPhone's own consent to be operated by the assistant through a computer it is paired with (the hub's extension
/// iphone, docs/extensions/iphone.md), asked in the guided setup's last step (IPhoneSetupView, #8199). The app finds
/// itself among the iPhones the computers list by its model and iOS version; several alike, the user picks; none, there
/// is no switch (fail-closed).
@Observable final class IPhoneConsentModel {
    struct Deps {
        /// The iPhones the computers reach; nil from a hub without the extension (the section stays hidden).
        var list: () async throws -> IPhoneList?
        var consent: (_ device: String, _ allowed: Bool) async throws -> IPhoneSeen
        var model: String
        var ios: String
        var picked: () -> String?
        var pick: (String?) -> Void

        static let pickedKey = "iphoneControl.device"

        static var live: Deps {
            .init(list: {
                guard let api = AppModel.shared.api else { return nil }
                do { return try await api.iphones() } catch let e as APIError where e.status == 404 { return nil }
            }, consent: { device, allowed in
                guard let api = AppModel.shared.api else { throw APIError(status: 0, message: String(localized: "Not connected to the hub yet")) }
                return try await api.setIPhoneConsent(device: device, allowed: allowed)
            }, model: ErrorReports.deviceModel, ios: ErrorReports.osVersion,
            picked: { UserDefaults.standard.string(forKey: pickedKey) },
            pick: { id in UserDefaults.standard.set(id, forKey: pickedKey) })
        }
    }

    private(set) var list: IPhoneList?
    private(set) var loaded = false
    private(set) var saving = false
    private(set) var error: String?
    private(set) var picked: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) {
        self.deps = deps
        picked = deps.picked()
    }

    /// Shown only while the hub has the extension on.
    var shown: Bool { list?.on == true }

    var match: IPhoneIdentity.Match {
        IPhoneIdentity.match(list?.devices ?? [], model: deps.model, ios: deps.ios, picked: picked)
    }

    func load() async {
        do {
            list = try await deps.list()
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
        loaded = true
    }

    /// "This One": the user says which of the alike iPhones this is.
    func pick(_ id: String?) {
        picked = id
        deps.pick(id)
    }

    /// The switch. Only for the iPhone this one is (match); anything else is refused here too.
    func setAllowed(_ allowed: Bool) async {
        guard case .this(let me, _) = match else { return }
        saving = true
        defer { saving = false }
        do {
            let seen = try await deps.consent(me.id, allowed)
            if var l = list, let i = l.devices.firstIndex(where: { $0.id == me.id }) {
                l.devices[i].consented = seen.consented
                l.devices[i].device.allowed = seen.device.allowed
                list = l
            }
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}

nonisolated enum IPhoneConsentText {
    /// Through which computers, and what is left before the assistant can operate it.
    /// The dialog before turning it on names the iPhone matched, so the owner can see it is the one in their hand
    /// (model and iOS version alone can match another).
    static func confirmTitle(_ match: IPhoneIdentity.Match) -> String {
        guard case .this(let me, _) = match else { return String(localized: "Allow the Assistant to Operate This iPhone?") }
        return String(localized: "Allow the Assistant to Operate \(quoted(me))?", comment: "The iPhone's name in quotes")
    }

    static func quoted(_ d: IPhoneSeen) -> String {
        String(localized: "「\(d.device.name.isEmpty ? String(localized: "iPhone") : d.device.name)」", comment: "A device's name in quotes")
    }

    /// Which iPhone it matched (by name, even when only one did), through which computers, and what is left.
    static func detail(_ d: IPhoneSeen) -> String {
        let through = quoted(d) + " · " + macs(d)
        guard d.consented else { return through }
        if !d.macOn { return through + " · " + String(localized: "Turn it on on the computer's page too") }
        return through + " · " + IPhoneControlText.state(d.device).0
    }

    static func macs(_ d: IPhoneSeen) -> String {
        String(localized: "Through \(d.macNames)")
    }
}
