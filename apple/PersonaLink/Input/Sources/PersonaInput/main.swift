import CoreGraphics
import Foundation
import PersonaInputKit

// Persona Input (#7775, docs/extensions/computer-use.md "Pixel scrolling"): posts the live screen's two-finger scrolls
// on this Mac as continuous scroll events, as a trackpad's are, so apps scroll by the point, with their own momentum and
// rubber-banding. An app bundle inside Persona Link.app so its Accessibility permission (posting events) is its own,
// not the node's. It posts scroll events only: no clicks, no keys. For Phone Screen (#9314) the same permission lets it
// add a portrait virtual display and move other apps' windows onto it and back (PhoneScreenSession.swift).
//
//   --preflight              whether posting events is allowed, asking nothing (the node reports it); a second line
//                            says whether Phone Screen may move windows (Accessibility), from a helper that has it
//   --request-permission     asks macOS once; macOS shows its window on the Mac's screen, and only the user answers it
//   --socket <path> --token <t>
//                            the node's socket for one live screen session: says the token, then "granted" or
//                            "missing" (missing ends here: the node scrolls by wheel notches instead), then posts every
//                            scroll record that comes until the node closes it
//   --socket <path> --token <t> --phone-screen
//                            one live screen's Phone Screen session on the node's socket: says the token, then
//                            "granted" or "missing", then answers JSON requests until the node closes it, putting
//                            windows and displays back as it ends
//   --phone-screen-restore   puts back what a Phone Screen session that died left (windows, text size), then exits;
//                            the node runs it at start and after a session ends with records left (#9500)
//   --phone-screen-set|-mode|-has …
//                            the short-lived child that configures displays for that session (DisplayConfig)

enum PersonaInputMain {
    static func run(_ argv: [String]) -> Int32 {
        switch argv.first {
        case "--preflight":
            let granted = CGPreflightPostEventAccess()
            print("post_event: \(granted ? "granted" : "missing")")
            print(PhoneScreenPreflight.line(trusted: AX.trusted))
            return granted ? 0 : 1
        case "--phone-screen-set", "--phone-screen-mode", "--phone-screen-has":
            return DisplayConfig.run(argv)
        case "--phone-screen-restore":
            PhoneScreenRestoreRun.run()
        case "--request-permission":
            let granted = CGRequestPostEventAccess()
            print("post_event: \(granted ? "granted" : "missing")")
            return granted ? 0 : 1
        case "--socket":
            if argv.count == 5, argv[2] == "--token", argv[4] == "--phone-screen" {
                PhoneScreenServer.run(socket: argv[1], token: argv[3])
            }
            guard argv.count == 4, argv[2] == "--token" else { return usage() }
            do {
                return try stream(socket: argv[1], token: argv[3])
            } catch {
                warn("stream ended: \(error)")
                return 1
            }
        default:
            return usage()
        }
    }

    private static func stream(socket path: String, token: String) throws -> Int32 {
        let socket = try UnixSocket.connect(path: path)
        defer { socket.close() }
        try socket.write(Array((token + "\n").utf8))
        let granted = CGPreflightPostEventAccess()
        try socket.write(Array((granted ? "granted\n" : "missing\n").utf8))
        guard granted else { return 1 }
        var reader = ScrollRecordReader()
        var poster = ScrollPoster()
        while true {
            let chunk = try socket.read()
            if chunk.isEmpty { return 0 } // the node closed it: the session ended
            for record in try reader.push(chunk) { poster.post(record) }
        }
    }

    private static func usage() -> Int32 {
        warn("usage: PersonaInput --preflight | --request-permission | --socket <path> --token <token> [--phone-screen]")
        return 2
    }

    private static func warn(_ text: String) {
        FileHandle.standardError.write(Data("persona-input: \(text)\n".utf8))
    }
}

/// Scroll records as continuous scroll events at the pointer (RFB has just put it where the fingers are). Events carry
/// whole points; what is left of a fraction goes with the next one, so nothing is lost or added over a gesture.
struct ScrollPoster {
    private var restY = 0.0, restX = 0.0

    mutating func post(_ r: ScrollRecord) {
        guard r.kind == .scroll else { return }
        if r.phase == .began || r.momentum == .begin { restY = 0; restX = 0 } // a new gesture or coast starts clean
        let y = Double(r.dy) + restY, x = Double(r.dx) + restX
        let wholeY = y.rounded(.towardZero), wholeX = x.rounded(.towardZero)
        restY = y - wholeY
        restX = x - wholeX
        guard let e = CGEvent(scrollWheelEvent2Source: nil, units: .pixel, wheelCount: 2,
                              wheel1: Int32(clamping: Int(wholeY)), wheel2: Int32(clamping: Int(wholeX)), wheel3: 0) else { return }
        e.setIntegerValueField(.scrollWheelEventIsContinuous, value: 1)
        e.setIntegerValueField(.scrollWheelEventScrollPhase, value: Int64(r.phase.rawValue))
        e.setIntegerValueField(.scrollWheelEventMomentumPhase, value: Int64(r.momentum.rawValue))
        if let at = CGEvent(source: nil)?.location { e.location = at }
        e.post(tap: .cghidEventTap)
    }
}

exit(PersonaInputMain.run(Array(CommandLine.arguments.dropFirst())))
