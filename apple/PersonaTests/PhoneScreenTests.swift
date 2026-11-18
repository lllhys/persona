import CoreGraphics
import Foundation
import Testing
@testable import Persona

/// Phone Screen on the phone (#9314): finding its display on a picture where displays have different scales, and the
/// session's start / windows / place / stop against a stand-in hub.
@MainActor
struct PhoneScreenTests {
    private let main = MachineDisplay(id: 1, x: 0, y: 0, width: 1920, height: 1080, main: true, scale: 1)

    private func phone(_ x: Double, _ y: Double, _ w: Double = 540, _ h: Double = 950) -> MachineDisplay {
        MachineDisplay(id: 23, x: x, y: y, width: w, height: h, scale: 2)
    }

    private func frame(_ ds: [VNCDisplay], _ id: String) -> CGRect? { ds.first { $0.id == id }?.frame }

    /// Measured on macOS 26.6 (docs/extensions/computer-use.md "Phone Screen"): a 2× display beside a 1× main one is
    /// drawn at its own pixels, the edge touching main at main's pixel coordinate. Every placement measured comes out at
    /// the framebuffer's size and puts the display where Screen Sharing drew it.
    @Test func displaysOfDifferentScalesLieWhereScreenSharingDrawsThem() {
        // Right of main, 540 × 1021 pt: framebuffer 3000 × 2042.
        var ds = VNCDisplays.layout(server: [], machine: [main, phone(1920, 0, 540, 1021)], framebuffer: CGSize(width: 3000, height: 2042))
        #expect(frame(ds, "display-23") == CGRect(x: 1920, y: 0, width: 1080, height: 2042))
        #expect(frame(ds, "display-1") == CGRect(x: 0, y: 0, width: 1920, height: 1080))
        // Left of main: 3000 × 1900, main moved right by the display's pixels.
        ds = VNCDisplays.layout(server: [], machine: [main, phone(-540, 0)], framebuffer: CGSize(width: 3000, height: 1900))
        #expect(frame(ds, "display-23") == CGRect(x: 0, y: 0, width: 1080, height: 1900))
        #expect(frame(ds, "display-1") == CGRect(x: 1080, y: 0, width: 1920, height: 1080))
        // Below: 1920 × 2980. Above: 1920 × 2980, main under it. Offset right by 130 pt: 3000 × 2030.
        ds = VNCDisplays.layout(server: [], machine: [main, phone(0, 1080)], framebuffer: CGSize(width: 1920, height: 2980))
        #expect(frame(ds, "display-23") == CGRect(x: 0, y: 1080, width: 1080, height: 1900))
        ds = VNCDisplays.layout(server: [], machine: [main, phone(0, -950)], framebuffer: CGSize(width: 1920, height: 2980))
        #expect(frame(ds, "display-23") == CGRect(x: 0, y: 0, width: 1080, height: 1900))
        #expect(frame(ds, "display-1") == CGRect(x: 0, y: 1900, width: 1920, height: 1080))
        ds = VNCDisplays.layout(server: [], machine: [main, phone(1920, 130)], framebuffer: CGSize(width: 3000, height: 2030))
        #expect(frame(ds, "display-23") == CGRect(x: 1920, y: 130, width: 1080, height: 1900))
    }

    /// The rule is trusted only when it explains the picture: another size (the display not drawn yet), or a display
    /// without a scale (an older node), offers nothing rather than a wrong crop.
    @Test func aLayoutThatDoesNotExplainThePictureOffersNothing() {
        #expect(VNCDisplays.layout(server: [], machine: [main, phone(1920, 0)], framebuffer: CGSize(width: 1920, height: 1080)).isEmpty)
        var old = phone(1920, 0)
        old.scale = 0
        #expect(VNCDisplays.anchored([main, old], framebuffer: CGSize(width: 3000, height: 1900)).isEmpty)
    }

    /// Scrolling on the phone display goes by its own points (2 pixels each), not the picture's average.
    @Test func theShownDisplaysScaleIsItsOwn() {
        let ds = [main, phone(1920, 0)], fb = CGSize(width: 3000, height: 1900)
        #expect(VNCDisplays.pixelsPerMacPoint(ds, framebuffer: fb, shown: "display-23") == 2)
        #expect(VNCDisplays.pixelsPerMacPoint(ds, framebuffer: fb, shown: "display-1") == 1)
        #expect(VNCDisplays.pixelsPerMacPoint(ds, framebuffer: fb) == 1)
    }

    @Test func displaysAndPhoneScreenDecode() throws {
        let json = #"{"enabled":true,"screen_sharing":true,"displays":[{"id":1,"x":0,"y":0,"width":1920,"height":1080,"main":true,"scale":1}],"phone_screen":{"version":1,"permission":"granted"}}"#
        let c = try JSONDecoder().decode(MachineComputer.self, from: Data(json.utf8))
        #expect(c.phoneScreen?.offered == true && c.phoneScreen?.granted == true)
        #expect(c.displays.first?.scale == 1)
        let old = try JSONDecoder().decode(MachineComputer.self, from: Data(#"{"enabled":true,"displays":[{"id":1,"x":0,"y":0,"width":10,"height":10}]}"#.utf8))
        #expect(old.phoneScreen == nil && old.displays.first?.scale == 0)
    }

    /// Start once per connection and shape, the window list and a placement, then stop; a new connection starts again.
    @Test func theSessionFollowsTheConnection() async throws {
        var asked: [PhoneScreenRequest] = []
        let display = phone(1920, 0, 540, 1021)
        let w = PhoneScreenWindow(pid: 10, window: 3, app: "Terminal", bundle: nil, title: "zsh", minimized: nil, placed: nil)
        let link = PhoneScreenLink(ref: MachineRef(group: "g", id: "mac")) { req in
            asked.append(req)
            switch req.op {
            case "start": return PhoneScreenReply(display: display, displays: [self.main, display])
            case "windows": return PhoneScreenReply(windows: [w])
            case "place": return PhoneScreenReply(display: display, placed: w)
            default: return PhoneScreenReply()
            }
        }
        link.start(pair: "P", size: CGSize(width: 402, height: 760))
        try await until { link.phase == .on }
        #expect(link.display == display && link.displays.count == 2)
        link.start(pair: "P", size: CGSize(width: 402, height: 760)) // the same again: nothing asked
        await link.loadWindows()
        #expect(link.windows == [w])
        #expect(await link.place(w))
        #expect(link.placed == w)
        link.start(pair: "R", size: CGSize(width: 402, height: 760)) // reconnected: the Mac ended the old one
        try await until { asked.filter { $0.op == "start" }.count == 2 && link.phase == .on }
        #expect(link.placed == nil)
        link.stop()
        try await until { asked.last?.op == "stop" }
        #expect(link.phase == .off)
        #expect(asked.map(\.op) == ["start", "windows", "place", "start", "stop"])
        #expect(asked[0].width == 402 && asked[0].height == 760 && asked[2].pid == 10 && asked[2].window == 3 && asked[4].pair == "R")
    }

    /// The page going without the small window: its Phone Screen ends (the Mac puts its windows back) and nothing is
    /// kept for a later page to take. (Into the small window it stays on: VNCTests' Phone Screen Picture in Picture.)
    @Test func thePageGoingWithoutTheSmallWindowEndsIt() async throws {
        var asked: [String] = []
        let display = phone(1920, 0, 540, 1021)
        let ref = MachineRef(group: "g", id: "mac-no-pip")
        let link = PhoneScreenLink(ref: ref) { req in
            asked.append(req.op)
            return req.op == "start" ? PhoneScreenReply(display: display, displays: [self.main, display]) : PhoneScreenReply()
        }
        link.start(pair: "P", size: CGSize(width: 402, height: 760))
        try await until { link.phase == .on }
        #expect(!VNCPiP.shared.keepPhone(link))
        try await until { asked.last == "stop" }
        #expect(link.phase == .off)
        #expect(VNCPiP.shared.takePhone(ref) == nil)
    }

    /// A refusal shows why in plain words and leaves the mode off on the Mac.
    @Test func aRefusalSaysWhy() async throws {
        let link = PhoneScreenLink(ref: MachineRef(group: "g", id: "mac")) { _ in
            throw APIError(status: 403, message: "no", fields: ["code": "permission"])
        }
        link.start(pair: "P", size: CGSize(width: 402, height: 760))
        try await until { if case .failed = link.phase { true } else { false } }
        #expect(link.phase == .failed(PhoneScreenLink.message(APIError(status: 403, message: "no", fields: ["code": "permission"]))))
    }

    /// The keyboard up (#9500): the picture keeps the view's width at its own size, its bottom under the keyboard, moved
    /// up only as far as needed to keep where the user tapped above it; with the keyboard down it fills the view.
    @Test func thePictureFillsTheWidthAndTheKeyboardCoversIt() {
        let picture = CGSize(width: 1080, height: 2042) // 540 × 1021 pt at 2×
        let full = CGRect(x: 0, y: 0, width: 402, height: 760)
        #expect(abs(VNCZoom.phoneFit(picture, in: full, pointer: nil, margin: 40).width - 402) < 0.5)
        let short = CGRect(x: 0, y: 0, width: 402, height: 420) // the keyboard and its row took the rest
        let top = VNCZoom.phoneFit(picture, in: short, pointer: CGPoint(x: 0.5, y: 0.2), margin: 40)
        #expect(top.minX == 0 && top.minY == 0 && top.width == 402 && abs(top.height - 402.0 * 2042 / 1080) < 0.01)
        // Tapped low on the picture: it moves up just enough to keep that point 40 pt above the view's bottom.
        let low = VNCZoom.phoneFit(picture, in: short, pointer: CGPoint(x: 0.5, y: 0.6), margin: 40)
        #expect(abs((low.minY + 0.6 * low.height) - (420 - 40)) < 0.01 && low.width == 402)
        // At the very bottom it never moves past the picture's end.
        let bottom = VNCZoom.phoneFit(picture, in: short, pointer: CGPoint(x: 0.5, y: 1), margin: 40)
        #expect(abs(bottom.maxY - 420) < 0.01)
        // A tall keyboard leaves a view wider than tall: still the whole width, the picture under it.
        let shorter = CGRect(x: 0, y: 0, width: 402, height: 300)
        #expect(VNCZoom.phoneFit(picture, in: shorter, pointer: nil, margin: 40).width == 402)
    }

    /// A display that widened for one window and narrowed for the next (#9500): each place answer's arrangement replaces
    /// the one from start, so the picture is found again; a helper that sends the display alone has it swapped in.
    @Test func theArrangementFollowsEachPlacement() async throws {
        let wide = MachineDisplay(id: 23, x: 1920, y: 0, width: 800, height: 1512, scale: 2)
        let w = PhoneScreenWindow(pid: 10, window: 3, app: "Wide", bundle: nil, title: "w", minimized: nil, placed: nil)
        var sendsDisplays = true
        let link = PhoneScreenLink(ref: MachineRef(group: "g", id: "mac")) { req in
            switch req.op {
            case "start": return PhoneScreenReply(display: self.phone(1920, 0, 540, 1021), displays: [self.main, self.phone(1920, 0, 540, 1021)])
            case "place": return PhoneScreenReply(display: wide, displays: sendsDisplays ? [self.main, wide] : nil, placed: w)
            default: return PhoneScreenReply()
            }
        }
        link.start(pair: "P", size: CGSize(width: 402, height: 760))
        try await until { link.phase == .on }
        #expect(await link.place(w))
        #expect(link.displays.contains(wide))
        // Now the picture (3520 × 3024, measured) holds the widened display.
        let laid = VNCDisplays.layout(server: [], machine: link.displays, framebuffer: CGSize(width: 3520, height: 3024))
        #expect(laid.first { $0.id == "display-23" }?.frame == CGRect(x: 1920, y: 0, width: 1600, height: 3024))
        sendsDisplays = false
        #expect(PhoneScreenLink.replacing(wide, in: [main, phone(1920, 0, 540, 1021)]) == [main, wide])
    }

    private func until(_ ok: @escaping () -> Bool) async throws {
        for _ in 0..<200 where !ok() { try await Task.sleep(for: .milliseconds(10)) }
        #expect(ok())
    }
}
