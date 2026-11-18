import Foundation
import PersonaScreenCore
#if canImport(CoreGraphics)
import CoreGraphics
#endif

// Persona Screen: the capture-and-encode helper for the iOS live-screen video channel (#3954). It is an app bundle
// inside Persona Link.app so its Screen Recording grant is its own, not the node's (`design-3954.poc.md`). The node
// starts it through LaunchServices with a per-stream Unix socket and token.
//
// On a Mac it captures with ScreenCaptureKit and encodes with VideoToolbox (`ScreenCaptureSource`); without
// ScreenCaptureKit it carries the wire protocol with a stub source that pauses at once, so the phone stays on RFB.

enum PersonaScreenMain {
    static func run(_ argv: [String]) -> Int32 {
        let args: HelperArguments
        do {
            args = try HelperArguments.parse(argv)
        } catch {
            warn("bad arguments: \(error)")
            return 2
        }
        switch args.mode {
        case .requestPermission:
            return requestPermission()
        case .preflight:
            return preflight()
        case let .stream(socket, token):
            do {
                try runStream(socketPath: socket, token: token, x264: args.x264)
                return 0
            } catch {
                warn("stream ended: \(error)")
                return 1
            }
        }
    }

    /// `--request-permission`: ask macOS for Screen Recording. macOS shows its alert on the Mac's screen; only input
    /// at that Mac answers it (`design-3954.poc.md`, "Who clicks Allow"). Nothing here answers for the user.
    private static func requestPermission() -> Int32 {
        #if canImport(CoreGraphics)
        let granted = CGRequestScreenCaptureAccess()
        print("screen_recording: \(granted ? "granted" : "missing")")
        return granted ? 0 : 1
        #else
        warn("screen recording is a macOS capability")
        return 1
        #endif
    }

    /// `--preflight`: whether Screen Recording is granted, asking nothing (`CGPreflightScreenCaptureAccess()`). The
    /// node reports this as `screen_video.permission`.
    private static func preflight() -> Int32 {
        #if canImport(CoreGraphics)
        let granted = CGPreflightScreenCaptureAccess()
        print("screen_recording: \(granted ? "granted" : "missing")")
        return granted ? 0 : 1
        #else
        print("screen_recording: missing")
        return 1
        #endif
    }

    private static func runStream(socketPath: String, token: String, x264: String?) throws {
        let socket = try UnixSocketClient.connect(path: socketPath)
        defer { socket.close() }
        try socket.writeAll(Data((token + "\n").utf8))

        // A windowless helper is one macOS may nap. Clamped, VideoToolbox's callbacks come late and a frame's encode reads
        // 18–35 ms instead of 8 (#8713), so the stream holds a latency-critical activity. Idle system sleep stays allowed:
        // whether the Mac stays awake is the node's `keep_awake`.
        let activity = ProcessInfo.processInfo.beginActivity(
            options: [.userInitiatedAllowingIdleSystemSleep, .latencyCritical], reason: "Live screen stream")
        defer { ProcessInfo.processInfo.endActivity(activity) }

        #if canImport(ScreenCaptureKit)
        let source: ScreenSource = ScreenCaptureSource(x264: x264)
        #else
        let source: ScreenSource = UnavailableScreenSource(reason: "permission")
        #endif
        let writeLock = NSLock()
        source.onMessage = { message in
            let framed = message.framed()
            writeLock.lock()
            defer { writeLock.unlock() }
            try? socket.writeAll(framed)
        }
        defer { source.stop() }

        var parser = ScreenVideoParser()
        while true {
            let chunk = try socket.readChunk()
            if chunk.isEmpty {
                break // the node closed the stream
            }
            for raw in try parser.push(chunk) {
                let message = try ScreenVideoMessage.decode(type: raw.type, body: raw.body)
                HelperDispatch.handle(message, source: source)
            }
        }
    }

    private static func warn(_ text: String) {
        FileHandle.standardError.write(Data("persona-screen: \(text)\n".utf8))
    }
}

exit(PersonaScreenMain.run(Array(CommandLine.arguments.dropFirst())))
