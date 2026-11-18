import Foundation

/// The iPhones a computer reaches that the assistant may operate through it (the hub's extension iphone,
/// docs/extensions/iphone.md): `Machine.iphone`, nil while the extension is off or the computer can't reach iPhones.
nonisolated struct MachineIPhone: Codable, Hashable, Sendable {
    /// The computer's switch: Allow the Assistant to Operate iPhones Through This Computer.
    var enabled = false
    /// The device bridge is on the computer; `version`, its version.
    var installed = false
    var version: String? = nil
    var devices: [IPhoneDevice] = []
    /// What is missing on the computer as a whole (no bridge, nothing paired, an old node), in the request's language.
    var note: String? = nil
    /// The note's reason as a code (bridge_missing, none_paired); nil from older nodes (#8199).
    var code: String? = nil
    /// The computer has the developer disk image Prepare mounts (Xcode's on a Mac); nil from older nodes.
    var ddiPresent: Bool? = nil
    /// Pairing an iPhone from the app, installing the bridge or preparing an iPhone (kind pair | forget | install |
    /// prepare): the running one, or the last.
    var job: ComputerJob? = nil

    enum CodingKeys: String, CodingKey {
        case enabled, installed, version, devices, note, code, job
        case ddiPresent = "ddi_present"
    }

    init(enabled: Bool = false, installed: Bool = false, version: String? = nil, devices: [IPhoneDevice] = [], note: String? = nil,
         job: ComputerJob? = nil) {
        self.enabled = enabled
        self.installed = installed
        self.version = version
        self.devices = devices
        self.note = note
        self.job = job
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        installed = try c.decodeIfPresent(Bool.self, forKey: .installed) ?? false
        version = try c.decodeIfPresent(String.self, forKey: .version)
        devices = try c.decodeIfPresent([IPhoneDevice].self, forKey: .devices) ?? []
        note = try c.decodeIfPresent(String.self, forKey: .note)
        code = try c.decodeIfPresent(String.self, forKey: .code)
        ddiPresent = try c.decodeIfPresent(Bool.self, forKey: .ddiPresent)
        job = try? c.decodeIfPresent(ComputerJob.self, forKey: .job)
    }
}

/// One iPhone as the computer's node sees it. `id` is the node's stable id for it; `productType` ("iPhone17,1")
/// and `ios` ("26.1") are how the app on that iPhone tells it is this one.
nonisolated struct IPhoneDevice: Codable, Hashable, Sendable, Identifiable {
    var id: String
    var name: String
    var productType: String? = nil
    var ios: String? = nil
    /// The computer holds a pairing record for it; `usb`: it is plugged in now (one that is can be paired).
    var paired = false
    var usb = false
    /// network | usb, while connected.
    var transport: String? = nil
    var connected = false
    var locked = false
    var ready = false
    var note: String? = nil
    /// The note's reason as a code (usb_unpaired, connecting, developer_mode_off, ddi_not_mounted, locked, …); nil
    /// from older nodes (#8199).
    var code: String? = nil
    /// What the iPhone last said of its Developer Mode; nil when it never said.
    var developerMode: Bool? = nil
    /// Its owner prepared it once, so its computer mounts the developer disk image again by itself after a restart.
    var autoPrepare = false
    /// The computer is on and this iPhone consented: the assistant may operate it.
    var allowed = false

    enum CodingKeys: String, CodingKey {
        case id, name, ios, paired, usb, transport, connected, locked, ready, note, code, allowed
        case productType = "product_type", developerMode = "developer_mode", autoPrepare = "auto_prepare"
    }

    init(id: String, name: String, productType: String? = nil, ios: String? = nil, paired: Bool = true, usb: Bool = false,
         transport: String? = nil, connected: Bool = false, locked: Bool = false, ready: Bool = false, note: String? = nil,
         allowed: Bool = false) {
        self.id = id
        self.name = name
        self.productType = productType
        self.ios = ios
        self.paired = paired
        self.usb = usb
        self.transport = transport
        self.connected = connected
        self.locked = locked
        self.ready = ready
        self.note = note
        self.allowed = allowed
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name) ?? ""
        productType = try c.decodeIfPresent(String.self, forKey: .productType)
        ios = try c.decodeIfPresent(String.self, forKey: .ios)
        paired = try c.decodeIfPresent(Bool.self, forKey: .paired) ?? false
        usb = try c.decodeIfPresent(Bool.self, forKey: .usb) ?? false
        transport = try c.decodeIfPresent(String.self, forKey: .transport)
        connected = try c.decodeIfPresent(Bool.self, forKey: .connected) ?? false
        locked = try c.decodeIfPresent(Bool.self, forKey: .locked) ?? false
        ready = try c.decodeIfPresent(Bool.self, forKey: .ready) ?? false
        note = try c.decodeIfPresent(String.self, forKey: .note)
        code = try c.decodeIfPresent(String.self, forKey: .code)
        developerMode = try c.decodeIfPresent(Bool.self, forKey: .developerMode)
        autoPrepare = try c.decodeIfPresent(Bool.self, forKey: .autoPrepare) ?? false
        allowed = try c.decodeIfPresent(Bool.self, forKey: .allowed) ?? false
    }
}

/// One iPhone in `GET /api/v1/iphone`: the device (each once, whichever computers reach it), the computers, and
/// whether it consented.
nonisolated struct IPhoneSeen: Codable, Hashable, Sendable, Identifiable {
    struct Mac: Codable, Hashable, Sendable {
        var id: String
        var name: String
        var enabled = false

        enum CodingKeys: String, CodingKey { case id, name, enabled }

        init(id: String, name: String, enabled: Bool = false) {
            self.id = id
            self.name = name
            self.enabled = enabled
        }

        init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            name = try c.decodeIfPresent(String.self, forKey: .name) ?? id
            enabled = try c.decodeIfPresent(Bool.self, forKey: .enabled) ?? false
        }
    }

    var device: IPhoneDevice
    var machines: [Mac] = []
    var consented = false

    var id: String { device.id }

    enum CodingKeys: String, CodingKey { case machines, consented }

    init(device: IPhoneDevice, machines: [Mac] = [], consented: Bool = false) {
        self.device = device
        self.machines = machines
        self.consented = consented
    }

    init(from decoder: Decoder) throws {
        device = try IPhoneDevice(from: decoder) // the device's fields are at the same level
        let c = try decoder.container(keyedBy: CodingKeys.self)
        machines = try c.decodeIfPresent([Mac].self, forKey: .machines) ?? []
        consented = try c.decodeIfPresent(Bool.self, forKey: .consented) ?? false
    }

    func encode(to encoder: Encoder) throws {
        try device.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(machines, forKey: .machines)
        try c.encode(consented, forKey: .consented)
    }

    /// The computers that reach it, by name.
    var macNames: String { machines.map(\.name).joined(separator: ", ") }
    /// Some computer that reaches it is turned on.
    var macOn: Bool { machines.contains { $0.enabled } }
}

/// `GET /api/v1/iphone`: whether the extension is on, and every iPhone the computers reach.
nonisolated struct IPhoneList: Codable, Hashable, Sendable {
    var on = false
    var devices: [IPhoneSeen] = []
}

/// Which of the iPhones the computers list is the one this app runs on. The app cannot read its own device id (iOS gives
/// apps no UDID), so it matches its model identifier and iOS version; several alike, the user picks one. Anything
/// else fails closed: no match, no switch.
nonisolated enum IPhoneIdentity {
    enum Match: Equatable {
        /// This is the one; `alike` is how many matched (more than one: it was picked, and can be picked again).
        case this(IPhoneSeen, alike: Int)
        /// Several look like this iPhone: the user picks.
        case choose([IPhoneSeen])
        /// None of them is this iPhone (not paired with an online computer, or the model and version do not match).
        case none
    }

    /// devices matched against model ("iPhone17,1") and ios ("26.1.1"); picked is the id picked before, if any.
    static func match(_ devices: [IPhoneSeen], model: String, ios: String, picked: String?) -> Match {
        let alike = devices.filter { d in
            guard let type = d.device.productType, let version = d.device.ios, !model.isEmpty else { return false }
            return type == model && same(version, ios)
        }
        if let picked, let d = alike.first(where: { $0.id == picked }) { return .this(d, alike: alike.count) }
        switch alike.count {
        case 0: return .none
        case 1: return .this(alike[0], alike: 1)
        default: return .choose(alike)
        }
    }

    /// The same iOS version, written either way ("26.1" and "26.1.0").
    static func same(_ a: String, _ b: String) -> Bool {
        func parts(_ v: String) -> [Int] {
            var p = v.split(separator: ".").map { Int($0) ?? -1 }
            while p.count > 1, p.last == 0 { p.removeLast() }
            return p
        }
        return !a.isEmpty && parts(a) == parts(b)
    }
}

/// `POST /api/v1/iphone/try`: the guided setup's Try It, a screenshot taken and Weather opened (#8199).
nonisolated struct IPhoneTryResult: Codable, Hashable, Sendable {
    struct Shot: Codable, Hashable, Sendable {
        var mime: String
        var data: String
    }

    var screenshot: Shot? = nil
    var opened = false
    var app = ""
    /// Why it stopped short, in the user's words.
    var error: String? = nil

    enum CodingKeys: String, CodingKey { case screenshot, opened, app, error }

    init(screenshot: Shot? = nil, opened: Bool = false, app: String = "", error: String? = nil) {
        self.screenshot = screenshot
        self.opened = opened
        self.app = app
        self.error = error
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        screenshot = try? c.decodeIfPresent(Shot.self, forKey: .screenshot)
        opened = try c.decodeIfPresent(Bool.self, forKey: .opened) ?? false
        app = try c.decodeIfPresent(String.self, forKey: .app) ?? ""
        error = try c.decodeIfPresent(String.self, forKey: .error)
    }
}
