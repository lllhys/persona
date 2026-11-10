import Foundation
#if canImport(WatchConnectivity)
import WatchConnectivity
#endif

/// Each group's credential the watch uses, and the groups' names (docs/protocol/multi-hub.md「Watch, Siri, Mac, and demo mode」).
nonisolated struct WatchConfigs: Codable, Sendable, Equatable {
    var configs: [String: ServerConfig]
    var names: [String: String] = [:]
    /// The groups the phone has but could not get the watch a credential for yet (#1185: an older hub, or the hub
    /// not reachable when it asked); the watch asks the user to sync again. nil from an older phone.
    var missing: [String]? = nil
    /// Watch token ids only; absent from older phones. Never the phone's device ids.
    var deviceIDs: [String: String]? = nil
    /// Authenticated phone hello capabilities by group; missing is unknown, an empty array is known unsupported.
    var features: [String: [String]]? = nil
}

nonisolated extension WatchConfigs {
    /// The watch keeps what the phone sent in its keychain: the configs carry hub tokens and Cloudflare Access service
    /// tokens. Builds before this kept them as plain JSON in defaults ("configs", or "config" from before several hubs);
    /// `stored` moves those over.
    static let keychainAccount = "watchConfigs"

    static func stored(_ defaults: UserDefaults = .standard) -> WatchConfigs? {
        switch Keychain.read(account: keychainAccount) {
        case .found(let data): return try? JSONDecoder().decode(Self.self, from: data)
        case .failed: return nil // not readable yet (before the first unlock): leave the old copy for next time
        case .missing: break
        }
        let old = defaults.data(forKey: "configs").flatMap { try? JSONDecoder().decode(Self.self, from: $0) }
            ?? defaults.data(forKey: "config").flatMap { try? JSONDecoder().decode(ServerConfig.self, from: $0) }
                .map { WatchConfigs(configs: ["": $0]) }
        old?.store(defaults)
        return old
    }

    /// Saves to the keychain and drops the plain copy older builds left in defaults.
    func store(_ defaults: UserDefaults = .standard) {
        guard let data = try? JSONEncoder().encode(self), Keychain.save(data, account: Self.keychainAccount) else { return }
        defaults.removeObject(forKey: "configs")
        defaults.removeObject(forKey: "config")
    }
}

nonisolated extension Interaction {
    /// Whether the phone passes it to the watch: not an MCP form or link, which the watch can't fill in, and which a
    /// watch build from before the kind would fail to decode, losing the whole summary with it.
    var showsOnWatch: Bool { kind != .elicitation }
}

/// What the watch needs to render without its own WebSocket: live sessions and pending interactions.

nonisolated struct WatchSummary: Codable, Sendable {
    var sessions: [Session]
    var interactions: [Interaction]
    var machines: [Machine]
    /// Open inbox items that need the user (docs/assistant/inbox.md); nil from an older phone.
    var inbox: [InboxItem]? = nil
    var at: Date = .now

    enum CodingKeys: String, CodingKey { case sessions, interactions, machines, inbox, at }

    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(sessions.map { NativeSessionEnvelope(session: $0) }, forKey: .sessions)
        try c.encode(interactions, forKey: .interactions)
        try c.encode(machines, forKey: .machines)
        try c.encodeIfPresent(inbox, forKey: .inbox)
        try c.encode(at, forKey: .at)
    }
}

#if canImport(WatchConnectivity)
/// iPhone → Watch sync over WatchConnectivity: the server pairing and a rolling summary.
/// The watch talks to the hub directly for actions (answering, sending), so the phone only seeds it.
final class WatchSync: NSObject, WCSessionDelegate {
    static let shared = WatchSync()

    var onConfig: ((ServerConfig) -> Void)?
    /// Every group's credential (a phone that knows several hubs).
    var onConfigs: ((WatchConfigs) -> Void)?
    var onSummary: ((WatchSummary) -> Void)?
    /// The persona chosen on the phone (the watch shows the Milk Frog only for its own).
    var onPersona: ((String) -> Void)?
    /// Called once the session is activated; the phone pushes its pairing then (pushes before that are dropped).
    var onActivated: (() -> Void)?
    /// iPhone side: answers the watch's "send me the pairing" request.
    var configProvider: (() -> ServerConfig?)?
    var configsProvider: (() -> WatchConfigs?)?
    var onRecording: ((RecordingTransfer) -> Void)?
    /// Watch side: the meetings' minutes the phone relays (`WatchMinutes`).
    var onMinutes: ((WatchMinutes) -> Void)?
    var onRecordingReceipt: ((RecordingReceipt) -> Void)?
    var onRecordingEvent: (() -> Void)?

    private var lastSummaryPush = Date.distantPast
    private var pendingSummary: WatchSummary?

    func activate() {
        guard WCSession.isSupported() else { return }
        WCSession.default.delegate = self
        WCSession.default.activate()
    }

    /// Every group's credential for the watch (docs/protocol/multi-hub.md「Watch, Siri, Mac, and demo mode」), what the groups are called and which have none
    /// yet; `config` (the first group's) stays for a watch app from before several hubs. Sent with no credentials too
    /// (#1185): a watch still holding one the phone no longer gives it drops it.
    func push(_ all: WatchConfigs) {
        var values: [String: Any] = ["configs": (try? JSONEncoder().encode(all)) ?? Data()]
        if let first = all.configs[""] ?? all.configs.values.first { values["config"] = (try? JSONEncoder().encode(first)) ?? Data() }
        update(values, removing: values["config"] == nil ? ["config"] : [])
        #if os(iOS) || os(visionOS)
        // Application context only keeps the latest state; user info is queued and delivered even if the watch app
        // was never opened, so a fresh install still gets paired.
        if WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled {
            WCSession.default.transferUserInfo(values)
        }
        #endif
    }

    /// The iPhone's interface language (docs/protocol/i18n.md: the watch follows the phone), as a code.
    func push(language: String) {
        update(["language": language])
    }

    /// The persona chosen on the phone, by id.
    func push(persona: String) {
        update(["persona": persona])
    }

    #if os(iOS) || os(visionOS)
    /// A paired watch with the app on it: the phone gets it credentials only then.
    var watchAppInstalled: Bool { WCSession.isSupported() && WCSession.default.isPaired && WCSession.default.isWatchAppInstalled }
    #endif

    #if os(watchOS)
    /// Watch side: ask the phone for its pairing right now (needs the phone nearby).
    func requestConfig(done: @escaping @MainActor (Bool) -> Void) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isReachable else {
            done(false)
            return
        }
        // Both handlers run on WatchConnectivity's queue: `@Sendable` keeps them off the main actor (BackgroundCallbackTests).
        WCSession.default.sendMessage(["request": "config"], replyHandler: { @Sendable reply in
            nonisolated(unsafe) let r = reply
            Task { @MainActor in
                self.apply(r)
                done(r["configs"] != nil || r["config"] != nil) // the phone answered with its pairing
            }
        }, errorHandler: { @Sendable _ in Task { @MainActor in done(false) } })
    }
    #endif

    /// Throttled: application context is meant for state, not a stream.
    func push(summary: @autoclosure () -> WatchSummary) {
        #if os(iOS) || os(visionOS)
        guard WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled else { return }
        pendingSummary = summary()
        let wait = 2 - Date.now.timeIntervalSince(lastSummaryPush)
        if wait <= 0 { flushSummary() } else {
            Task { @MainActor in
                try? await Task.sleep(for: .seconds(wait))
                self.flushSummary()
            }
        }
        #endif
    }

    private func flushSummary() {
        guard let s = pendingSummary, let data = try? JSON.encoder.encode(s) else { return }
        pendingSummary = nil
        lastSummaryPush = .now
        update(["summary": data])
    }

    /// iPhone side: the meetings' minutes for the watch, when they changed.
    func push(minutes: WatchMinutes) {
        #if os(iOS) || os(visionOS)
        guard WCSession.isSupported(), WCSession.default.activationState == .activated, WCSession.default.isWatchAppInstalled,
              let data = minutes.data, WCSession.default.applicationContext[WatchMinutes.key] as? Data != data else { return }
        update([WatchMinutes.key: data])
        #endif
    }

    private func update(_ values: [String: Any], removing gone: [String] = []) {
        guard WCSession.isSupported(), WCSession.default.activationState == .activated else { return }
        var ctx = WCSession.default.applicationContext
        values.forEach { ctx[$0.key] = $0.value }
        gone.forEach { ctx[$0] = nil }
        try? WCSession.default.updateApplicationContext(ctx)
    }

    /// The iPhone side sets this once it can hold rolling recordings; an older phone never does, so the watch keeps to plain recordings.
    static let phoneRollingKey = "recording.phoneRolling"

    private func apply(_ ctx: [String: Any]) {
        if let rolling = ctx["recordingRolling"] as? Bool { UserDefaults.standard.set(rolling, forKey: Self.phoneRollingKey) }
        #if os(watchOS)
        if let language = ctx["language"] as? String {
            LanguageStore.shared.follow(language)
            if AppLanguage.codes.contains(language) {
                LanguageStore.groupDefaults()?.set(language, forKey: LanguageStore.resolvedKey)
            }
        }
        #endif
        if let persona = ctx["persona"] as? String { onPersona?(persona) }
        if let data = ctx["configs"] as? Data, let all = try? JSONDecoder().decode(WatchConfigs.self, from: data) {
            onConfigs?(all)
        } else if let data = ctx["config"] as? Data, let cfg = try? JSONDecoder().decode(ServerConfig.self, from: data) {
            onConfig?(cfg)
        }
        if let data = ctx["summary"] as? Data, let s = try? JSON.decoder.decode(WatchSummary.self, from: data) {
            onSummary?(s)
        }
        if let minutes = WatchMinutes(data: ctx[WatchMinutes.key] as? Data) { onMinutes?(minutes) }
    }

    nonisolated func session(_ session: WCSession, activationDidCompleteWith state: WCSessionActivationState, error: Error?) {
        nonisolated(unsafe) let ctx = session.receivedApplicationContext
        Task { @MainActor in
            self.apply(ctx)
            #if os(iOS) || os(visionOS)
            self.update(["recordingRolling": true])
            #endif
            self.onActivated?()
            self.onRecordingEvent?()
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any] = [:]) {
        let recording = RecordingTransfer(dictionary: userInfo)
        let receipt = RecordingReceipt(dictionary: userInfo)
        nonisolated(unsafe) let copy = userInfo
        Task { @MainActor in
            if let recording { self.onRecording?(recording) }
            else if let receipt { self.onRecordingReceipt?(receipt) }
            else { self.apply(copy) }
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveMessage message: [String: Any], replyHandler: @escaping ([String: Any]) -> Void) {
        nonisolated(unsafe) let reply = replyHandler
        Task { @MainActor in
            var answer: [String: Any] = [:]
            if let cfg = self.configProvider?(), let data = try? JSONEncoder().encode(cfg) { answer["config"] = data }
            if let all = self.configsProvider?(), let data = try? JSONEncoder().encode(all) { answer["configs"] = data }
            reply(answer)
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext ctx: [String: Any]) {
        nonisolated(unsafe) let copy = ctx
        Task { @MainActor in self.apply(copy) }
    }

    nonisolated func sessionReachabilityDidChange(_ session: WCSession) {
        Task { @MainActor in self.onRecordingEvent?() }
    }

    nonisolated func session(_ session: WCSession, didReceive file: WCSessionFile) {
        #if os(iOS) || os(visionOS)
        guard let transfer = RecordingTransfer(dictionary: file.metadata), let segment = transfer.segment else { return }
        // The system deletes the temporary file once the callback returns: move it synchronously first, then let the main thread update the index.
        let dir = RecordingLibrary.directory.appendingPathComponent(transfer.id.uuidString, isDirectory: true)
        let destination = dir.appendingPathComponent(RecordingSegment.fileName(segment.index))
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            if !FileManager.default.fileExists(atPath: destination.path) {
                try FileManager.default.moveItem(at: file.fileURL, to: destination)
            }
            Task { @MainActor in self.onRecording?(transfer) }
        } catch { /* Not acknowledged; the watch keeps the audio and retries. */ }
        #endif
    }

    nonisolated func session(_ session: WCSession, didFinish fileTransfer: WCSessionFileTransfer, error: Error?) {
        #if os(watchOS)
        guard let transfer = RecordingTransfer(dictionary: fileTransfer.file.metadata) else { return }
        let failed = error != nil
        Task { @MainActor in WatchRecordingTransfer.shared.finished(transfer, failed: failed) }
        #endif
    }

    nonisolated func session(_ session: WCSession, didFinish userInfoTransfer: WCSessionUserInfoTransfer, error: Error?) {
        #if os(watchOS)
        guard let transfer = RecordingTransfer(dictionary: userInfoTransfer.userInfo) else { return }
        let failed = error != nil
        Task { @MainActor in WatchRecordingTransfer.shared.finished(transfer, failed: failed) }
        #endif
    }

    func confirmRecording(_ receipt: RecordingReceipt) {
        guard WCSession.default.activationState == .activated else { return }
        WCSession.default.transferUserInfo(receipt.dictionary)
    }

    #if os(iOS) || os(visionOS)
    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}
    nonisolated func sessionDidDeactivate(_ session: WCSession) { session.activate() }
    #endif
}
#endif
