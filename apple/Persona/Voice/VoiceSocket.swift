import Foundation

/// What came in on the call's socket.
nonisolated enum VoiceSocketEvent: Equatable, Sendable {
    case text(String)
    case data(Data)
    /// It is gone: the last event, once.
    case closed(VoiceSocketClose)
}

nonisolated struct VoiceSocketClose: Equatable, Sendable {
    /// The HTTP status the hub refused the handshake with (401 a token it does not take, 403 a device that may only
    /// answer, 404 no such extension); nil when the socket was open and then went away.
    var status: Int?
    var reason: String?

    init(status: Int? = nil, reason: String? = nil) {
        self.status = status
        self.reason = reason
    }
}

/// The call's socket to the voice extension, through the hub. Sends go from any thread (the microphone's PCM comes
/// from the audio thread); events come to the handler on the main actor, in order.
nonisolated protocol VoiceSocket: AnyObject, Sendable {
    func open(_ handler: @escaping @MainActor @Sendable (VoiceSocketEvent) -> Void)
    func send(text: String)
    func send(data: Data)
    /// Closes it from this side: no `.closed` event follows.
    func close()
}

/// The socket on `URLSessionWebSocketTask`, made from `VoiceEndpoint.request` and the group's `APIClient.session` (as
/// `RFBWebSocket` and the hub's own socket do). It pings every 20 seconds (PR #1931's heartbeat): a ping that fails, or
/// is still unanswered at the next one, ends it, so a dead link shows instead of a silent call.
nonisolated final class HubVoiceSocket: VoiceSocket, @unchecked Sendable {
    static let pingEvery: Duration = .seconds(20)

    private let task: URLSessionWebSocketTask
    private let lock = NSLock()
    private var handler: (@MainActor @Sendable (VoiceSocketEvent) -> Void)?
    private var done = false
    private var awaitingPong = false
    private var reader: Task<Void, Never>?
    private var pinger: Task<Void, Never>?

    init(request: URLRequest, session: URLSession) {
        task = session.webSocketTask(with: request)
        task.delegate = HubRedirects.shared
        task.maximumMessageSize = 16 << 20
    }

    func open(_ handler: @escaping @MainActor @Sendable (VoiceSocketEvent) -> Void) {
        lock.withLock { self.handler = handler }
        task.resume()
        let reader = Task { [weak self] in
            guard let self else { return }
            await self.read()
        }
        let pinger = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.pingEvery)
                guard let self, !Task.isCancelled, !self.isDone else { return }
                // The last ping had no answer in a whole interval: the link is dead even if nothing failed.
                guard self.lock.withLock({ () -> Bool in
                    defer { self.awaitingPong = true }
                    return !self.awaitingPong
                }) else {
                    self.finish(VoiceSocketClose(reason: "no pong"))
                    return
                }
                self.task.sendPing { @Sendable [weak self] error in
                    guard let self else { return }
                    if let error {
                        self.finish(VoiceSocketClose(reason: error.localizedDescription))
                    } else {
                        self.lock.withLock { self.awaitingPong = false }
                    }
                }
            }
        }
        lock.withLock {
            self.reader = reader
            self.pinger = pinger
        }
    }

    func send(text: String) {
        task.send(.string(text)) { @Sendable [weak self] error in
            guard let error else { return }
            self?.finish(VoiceSocketClose(reason: error.localizedDescription))
        }
    }

    func send(data: Data) {
        // A failed send shows as the receive failing too; nothing to add per buffer.
        task.send(.data(data)) { @Sendable _ in }
    }

    func close() {
        guard let tasks = stop() else { return }
        tasks.forEach { $0?.cancel() }
        task.cancel(with: .normalClosure, reason: nil)
    }

    /// Marks it done (once): the tasks to cancel, nil when it already was.
    private func stop() -> [Task<Void, Never>?]? {
        lock.withLock {
            guard !done else { return nil }
            done = true
            handler = nil
            return [reader, pinger]
        }
    }

    private var isDone: Bool { lock.withLock { done } }

    /// One message at a time, each handed over before the next is read: frames keep their order (a `clear` between two
    /// pieces of audio drops only what came before it).
    private func read() async {
        while true {
            let event: VoiceSocketEvent
            do {
                switch try await task.receive() {
                case .string(let text): event = .text(text)
                case .data(let data): event = .data(data)
                @unknown default: continue
                }
            } catch {
                finish(VoiceSocketClose(reason: error.localizedDescription))
                return
            }
            guard let handler = lock.withLock({ done ? nil : self.handler }) else { return }
            // Checked again on the main actor: nothing is handed over after `.closed`.
            await MainActor.run { [weak self] in
                guard let self, !self.isDone else { return }
                handler(event)
            }
        }
    }

    /// The handshake's status when the hub answered it with something else than 101.
    private var refusedStatus: Int? {
        guard let status = (task.response as? HTTPURLResponse)?.statusCode, status != 101, status >= 300 else { return nil }
        return status
    }

    private var closeReason: String? {
        task.closeReason.flatMap { String(data: $0, encoding: .utf8) }.flatMap { $0.isEmpty ? nil : $0 }
    }

    /// Every way it goes away ends here, once. A handshake the hub refused fails the pending `start` send, the ping and
    /// the receive at the same moment, in no set order, and the first to get here wins: so the status and the hub's close
    /// reason are read here for all of them, else a 401/403/404 could come out as a passing drop and be tried again.
    private func finish(_ close: VoiceSocketClose) {
        let close = VoiceSocketClose(status: close.status ?? refusedStatus, reason: closeReason ?? close.reason)
        let handler = lock.withLock { self.handler }
        guard let tasks = stop() else { return }
        tasks.forEach { $0?.cancel() }
        task.cancel(with: .goingAway, reason: nil)
        guard let handler else { return }
        Task { @MainActor in handler(.closed(close)) }
    }
}
