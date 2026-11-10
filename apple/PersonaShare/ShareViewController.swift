import Security
import SwiftUI
import UIKit
import UniformTypeIdentifiers

/// Share › Persona (#4586): text, a link or pictures from another app go to Persona's main conversation, with a note
/// typed here, as Android's ShareActivity sends them. The extension talks to the hub itself (the app may not be
/// running): it reads the pairing the app keeps in the keychain and sends like the composer does.
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let model = ShareModel(items: (extensionContext?.inputItems as? [NSExtensionItem]) ?? []) { [weak self] sent in
            if sent {
                self?.extensionContext?.completeRequest(returningItems: nil)
            } else {
                self?.extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
            }
        }
        let host = UIHostingController(rootView: ShareView(model: model))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
        Task { await model.load() }
    }
}

@MainActor @Observable final class ShareModel {
    enum Phase: Equatable { case loading, ready, sending, failed(String) }

    var note = ""
    private(set) var text = ""
    private(set) var images: [UIImage] = []
    private(set) var phase = Phase.loading
    private let items: [NSExtensionItem]
    private let finish: (Bool) -> Void

    /// What Persona's composer sends at most (ChatView `add`).
    static let maxImages = 4

    init(items: [NSExtensionItem], finish: @escaping (Bool) -> Void) {
        self.items = items
        self.finish = finish
    }

    var canSend: Bool { phase == .ready && !(message.isEmpty && images.isEmpty) }

    /// The note, then what was shared (a page's title and its link, or the text).
    var message: String {
        [note.trimmingCharacters(in: .whitespacesAndNewlines), text].filter { !$0.isEmpty }.joined(separator: "\n\n")
    }

    func load() async {
        var parts: [String] = []
        for item in items {
            if let title = item.attributedContentText?.string.trimmingCharacters(in: .whitespacesAndNewlines), !title.isEmpty {
                parts.append(title)
            }
            for provider in item.attachments ?? [] {
                if provider.hasItemConformingToTypeIdentifier(UTType.image.identifier), images.count < Self.maxImages {
                    if let data = try? await provider.data(for: .image), let img = UIImage(data: data) { images.append(img) }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.url.identifier) {
                    if let url = try? await provider.url(), !parts.contains(url.absoluteString) { parts.append(url.absoluteString) }
                } else if provider.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) {
                    if let s = try? await provider.text(), !s.isEmpty, !parts.contains(s) { parts.append(s) }
                }
            }
        }
        text = parts.joined(separator: "\n")
        phase = .ready
    }

    func cancel() { finish(false) }

    func send() async {
        guard canSend else { return }
        phase = .sending
        let payload = images.compactMap(ShareModel.jpeg)
        let body = message.isEmpty ? String(localized: "Please look at the image [Assistant View 5ad878]") : message
        do {
            try await ShareHub.sendToMain(text: body, images: payload)
            finish(true)
        } catch let e as ShareHub.Failure {
            phase = .failed(e.sentence)
        } catch {
            phase = .failed(String(localized: "Can't reach the hub. Try again later."))
        }
    }

    /// As the composer sends a picture: at most 1600 px a side, JPEG.
    private static func jpeg(_ img: UIImage) -> ShareHub.Image? {
        let side = max(img.size.width, img.size.height)
        let scale = side > 1600 ? 1600 / side : 1
        let size = CGSize(width: (img.size.width * scale).rounded(), height: (img.size.height * scale).rounded())
        let format = UIGraphicsImageRendererFormat.default()
        format.scale = 1
        let scaled = UIGraphicsImageRenderer(size: size, format: format).image { _ in img.draw(in: CGRect(origin: .zero, size: size)) }
        return scaled.jpegData(compressionQuality: 0.75).map { ShareHub.Image(mime: "image/jpeg", data: $0.base64EncodedString()) }
    }
}

struct ShareView: View {
    @Bindable var model: ShareModel
    @FocusState private var noteFocused: Bool

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    TextField(String(localized: "Add a note (optional)"), text: $model.note, axis: .vertical)
                        .lineLimit(2...6)
                        .focused($noteFocused)
                } footer: {
                    Text("Goes to Persona's main conversation")
                }
                if !model.text.isEmpty || !model.images.isEmpty {
                    Section("Shared") {
                        if !model.text.isEmpty {
                            Text(model.text).font(.callout).foregroundStyle(.secondary).lineLimit(6)
                        }
                        if !model.images.isEmpty {
                            Label("\(model.images.count) images", systemImage: "photo.on.rectangle")
                        }
                    }
                }
                if case .failed(let why) = model.phase {
                    Section { Label(why, systemImage: "exclamationmark.triangle") }
                }
            }
            .navigationTitle("Send to Persona")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { model.cancel() } }
                ToolbarItem(placement: .confirmationAction) {
                    if model.phase == .sending {
                        ProgressView()
                    } else {
                        Button("Send") { Task { await model.send() } }.disabled(!model.canSend && !isFailed)
                    }
                }
            }
            .onAppear { noteFocused = true }
        }
    }

    private var isFailed: Bool { if case .failed = model.phase { true } else { false } }
}

/// The few hub calls the extension makes, on the pairing the app keeps in the keychain (`HubStore`, account "hubs",
/// the app's service and keychain group): the default group's hubs, tried in turn.
enum ShareHub {
    struct Image: Encodable { var mime: String; var data: String }

    enum Failure: Error {
        case notPaired, unauthorized, noMain, unreachable, accessNeeded, accessRejected

        var sentence: String {
            switch self {
            case .accessNeeded: String(localized: "This hub is behind Cloudflare Access and needs a service token")
            case .accessRejected: String(localized: "Cloudflare Access rejected the service token")
            case .notPaired: String(localized: "No hub paired yet: pair one in Persona first")
            case .unauthorized: String(localized: "The pairing no longer works: pair again in Persona")
            case .noMain: String(localized: "No main conversation yet: send something in Persona first")
            case .unreachable: String(localized: "Can't reach the hub. Try again later.")
            }
        }
    }

    /// The registry's shape, only what the extension uses (Shared/Hubs.swift `HubRegistry`).
    private struct Registry: Decodable {
        struct Hub: Decodable {
            struct Config: Decodable {
                var url: URL
                var token: String
                /// The hub's Cloudflare Access service token (Shared/CloudflareAccess.swift `HubAccess`).
                var access: Access?
            }
            struct Access: Decodable {
                var clientID: String
                var clientSecret: String
                enum CodingKeys: String, CodingKey { case clientID = "client_id", clientSecret = "client_secret" }
            }
            var config: Config
            var group: String?
        }
        var hubs: [Hub]
    }

    private struct Assistant: Decodable {
        var machineID: String
        var main: String?
        enum CodingKeys: String, CodingKey { case main, machineID = "machine_id" }
    }

    private struct Send: Encodable {
        var text: String
        var images: [Image]?
        var clientMsgID: String
        enum CodingKeys: String, CodingKey { case text, images, clientMsgID = "client_msg_id" }
    }

    static func sendToMain(text: String, images: [Image]) async throws {
        let hubs = try paired()
        var last = Failure.unreachable
        let id = UUID().uuidString // the same on every hub tried: one that took it and did not answer types it once
        for hub in hubs {
            do {
                let a: Assistant = try JSONDecoder().decode(Assistant.self, from: await call(hub, "GET", "/api/v1/assistant"))
                guard let main = a.main, !main.isEmpty else { throw Failure.noMain }
                let body = try JSONEncoder().encode(Send(text: text, images: images.isEmpty ? nil : images, clientMsgID: id))
                _ = try await call(hub, "POST", "/api/v1/sessions/\(a.machineID)/\(main)/messages", body: body)
                return
            } catch let f as Failure where f == .unreachable {
                last = f
            }
        }
        throw last
    }

    /// The default group's hubs (the app opens that one first), or the first group's.
    private static func paired() throws -> [Registry.Hub] {
        let app = Bundle.main.object(forInfoDictionaryKey: "PersonaAppBundleID") as? String ?? ""
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: app,
                                    kSecAttrAccount as String: "hubs", kSecReturnData as String: true,
                                    kSecMatchLimit as String: kSecMatchLimitOne]
        var out: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &out) == errSecSuccess, let data = out as? Data,
              let r = try? JSONDecoder().decode(Registry.self, from: data), let first = r.hubs.first else { throw Failure.notPaired }
        let group = r.hubs.contains { ($0.group ?? "").isEmpty } ? "" : (first.group ?? "")
        return r.hubs.filter { ($0.group ?? "") == group }
    }

    private static func call(_ hub: Registry.Hub, _ method: String, _ path: String, body: Data? = nil) async throws -> Data {
        var req = URLRequest(url: hub.config.url.appending(path: path), timeoutInterval: 60)
        req.httpMethod = method
        req.setValue("Bearer \(hub.config.token)", forHTTPHeaderField: "Authorization")
        if let access = hub.config.access { // the request is on the hub's own address
            req.setValue(access.clientID, forHTTPHeaderField: "CF-Access-Client-Id")
            req.setValue(access.clientSecret, forHTTPHeaderField: "CF-Access-Client-Secret")
        }
        if let body {
            req.httpBody = body
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, response): (Data, URLResponse)
        do { (data, response) = try await URLSession.shared.data(for: req, delegate: NoRedirects()) } catch { throw Failure.unreachable }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        // Cloudflare Access in front of the hub: a redirect to its login (not followed), or its own page (as
        // `CloudflareAccess.blocked` in the app).
        let location = (response as? HTTPURLResponse)?.value(forHTTPHeaderField: "Location")?.lowercased() ?? ""
        let page = (response.mimeType ?? "").contains("html") ? String(decoding: data.prefix(64 << 10), as: UTF8.self).lowercased() : ""
        if location.contains("cloudflareaccess.com") || page.contains("cloudflareaccess.com") || page.contains("/cdn-cgi/access/") {
            throw hub.config.access == nil ? Failure.accessNeeded : Failure.accessRejected
        }
        switch status {
        case 200..<300: return data
        case 401, 403: throw Failure.unauthorized
        default: throw Failure.unreachable
        }
    }
}

/// Redirects are not followed: the hub's credential and its Cloudflare Access token stay on its own address.
/// `nonisolated`: this target defaults to MainActor, and a MainActor redirect method would never be called (#1378).
private nonisolated final class NoRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

private extension NSItemProvider {
    func data(for type: UTType) async throws -> Data {
        try await withCheckedThrowingContinuation { c in
            _ = loadDataRepresentation(for: type) { data, error in
                if let data { c.resume(returning: data) } else { c.resume(throwing: error ?? CocoaError(.fileReadUnknown)) }
            }
        }
    }

    func url() async throws -> URL {
        let item = try await loadItem(forTypeIdentifier: UTType.url.identifier)
        if let url = item as? URL { return url }
        if let data = item as? Data, let url = URL(dataRepresentation: data, relativeTo: nil) { return url }
        throw CocoaError(.fileReadUnknown)
    }

    func text() async throws -> String {
        let item = try await loadItem(forTypeIdentifier: UTType.plainText.identifier)
        if let s = item as? String { return s }
        if let data = item as? Data, let s = String(data: data, encoding: .utf8) { return s }
        throw CocoaError(.fileReadUnknown)
    }
}
