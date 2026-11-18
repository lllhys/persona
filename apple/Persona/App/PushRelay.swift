import Foundation
import os
import PersonaWire

/// The push relay this build registers with (docs/operations/push-relay.md): it holds the app's APNs key and pushes for hubs that
/// have none. The phone registers its device token here and hands the grant it gets, with its public key, to its own hub;
/// the relay never sees the key, so it cannot read what the hub seals.
nonisolated enum PushRelay {
    static let log = Logger(subsystem: "persona", category: "push-relay")

    /// From the build config (`app.push_relay`, Info.plist `PersonaPushRelay`); nil: this build uses no relay.
    static var url: URL? {
        guard let s = NativeStateMigration.infoString("PersonaPushRelay"),
              !s.isEmpty, let u = URL(string: s), u.scheme == "https" else { return nil } // the grant must not cross in the clear
        return u
    }

    private struct Cached: Codable { var relay: String; var token: String; var env: String; var topic: String; var grant: String }
    static let account = "push-relay-grant"
    /// The grant for the PushKit token of urgent calls, kept apart from the notifications' one.
    static let voipAccount = "push-relay-voip-grant"

    /// The grant for this device token: the cached one while relay, token, environment and app are the same, else a new one.
    static func grant(relay: URL, token: String, env: String, topic: String, account: String = account,
                      session: URLSession = .shared) async throws -> String {
        if let data = Keychain.load(account: account), let c = try? JSONDecoder().decode(Cached.self, from: data),
           c.relay == relay.absoluteString, c.token == token, c.env == env, c.topic == topic {
            return c.grant
        }
        var req = URLRequest(url: relay.appending(path: "v1/register"), timeoutInterval: 15)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONEncoder().encode(["apns_token": token, "env": env, "topic": topic])
        // A redirect is not followed: URLSession would send the POST on as a GET, which never registers. Its status
        // and target say the relay moved (an old address, builds up to 190, #5998).
        let (data, resp) = try await session.data(for: req, delegate: NoRedirects())
        struct Answer: Decodable { var grant: String?; var error: String? }
        let answer = try? JSONDecoder().decode(Answer.self, from: data)
        let http = resp as? HTTPURLResponse
        guard http?.statusCode == 200, let grant = answer?.grant, !grant.isEmpty else {
            let status = http?.statusCode ?? 0
            if (300..<400).contains(status) {
                throw RelayError(status: status, message: "", location: http?.value(forHTTPHeaderField: "Location") ?? "")
            }
            throw RelayError(status: status, message: answer?.error ?? "")
        }
        if let data = try? JSONEncoder().encode(Cached(relay: relay.absoluteString, token: token, env: env, topic: topic, grant: grant)) {
            Keychain.save(data, account: account)
        }
        log.notice("registered with the push relay")
        return grant
    }

    struct RelayError: LocalizedError {
        var status: Int
        var message: String
        /// A redirect's target.
        var location: String?
        var errorDescription: String? { "push relay \(status) \(location ?? message)" }

        /// What Settings › Notifications and the hub are told.
        var outcome: PushHealth.Relay {
            if let location { return .moved(status: status, location: location) }
            return .refused(status: status, message: message)
        }
    }

    /// The outcome of a registration that threw `error`.
    static func outcome(of error: any Error) -> PushHealth.Relay {
        if let e = error as? RelayError { return e.outcome }
        return .unreachable(error.localizedDescription)
    }

    // The completion-handler form: the async one crashes the compiler (Xcode 27.1) emitting its Objective-C thunk.
    private final class NoRedirects: NSObject, URLSessionTaskDelegate, Sendable {
        func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                        newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
            completionHandler(nil)
        }
    }
}
