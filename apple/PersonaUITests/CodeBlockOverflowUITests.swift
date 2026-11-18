import UIKit
import XCTest

/// A reply's code block keeps its long lines inside it (#8212): they scroll sideways within the block and nothing is
/// drawn past its trailing edge. Against scripts/mock/mock_hub.py with MOCK_CODE_WIDE=1: an answer whose code block has
/// lines far wider than any column. The case it was found in is the iPhone Duo, whose displays (the outer one, the inner
/// one on its side) have the status bar down their trailing edge: run it on the Duo simulator, folded or open on its
/// side; on other devices it checks the same strip, where there is no such safe area.
/// Light and dark, one after the other. PERSONA_SHOTS: a directory that gets the screenshots.
final class CodeBlockOverflowUITests: XCTestCase {
    private let chat = "macbook/a1f3c2d4-0000-4000-8000-0000000c1e30"

    @MainActor func testLongLinesStayInsideTheBlock() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_CODE_WIDE=1 (PERSONA_MOCK_HUB)")
        }
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        for scheme in ["light", "dark"] {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", chat, "-ui.scheme", scheme]
            app.launchPinned()
            let copy = app.buttons["code-block-copy"]
            XCTAssertTrue(copy.waitForExistence(timeout: 20), "\(scheme): the answer's code block never showed")
            let code = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "Background: the last round")).firstMatch
            XCTAssertTrue(code.waitForExistence(timeout: 5), "\(scheme): the code block's lines are not there")
            let window = app.windows.firstMatch.frame
            let settled = expectation(for: NSPredicate { _, _ in copy.isHittable && copy.frame.maxX <= window.maxX },
                                      evaluatedWith: nil)
            wait(for: [settled], timeout: 10)
            dismissIconAlert()
            let shot = app.screenshot()
            save(shot, "code-wide-\(scheme)")
            // The block's trailing edge is Copy's plus the bar's inner margin; a few points more keep its rounded corner
            // and antialiasing out of the strip that must stay empty. Rows: the code's lines, short of the block's padding.
            let edge = copy.frame.maxX + 20
            let rows = code.frame.minY.rounded(.up) ... code.frame.maxY.rounded(.down)
            let ink = inkPoints(in: shot.image, x: edge ... window.maxX, y: rows, dark: scheme == "dark")
            XCTAssertEqual(ink, 0, "\(scheme): the code's lines are drawn past the block's edge (\(ink) text points)")
            app.terminate()
        }
    }

    /// 「你已更改“Persona”的图标」 (the icon follows Light & Dark) is SpringBoard's, over the app.
    @MainActor private func dismissIconAlert() {
        let alert = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
        guard alert.waitForExistence(timeout: 2) else { return }
        for label in ["好", "OK"] where alert.buttons[label].exists { alert.buttons[label].tap(); break }
        _ = alert.waitForNonExistence(timeout: 3)
    }

    /// How many points in that strip look like text: gray, dark in the light scheme and light in the dark one (not the
    /// blue of the Duo's side controls).
    private func inkPoints(in image: UIImage, x: ClosedRange<CGFloat>, y: ClosedRange<CGFloat>, dark: Bool) -> Int {
        guard let cg = image.cgImage, x.upperBound > x.lowerBound, y.upperBound > y.lowerBound else { return 0 }
        let scale = CGFloat(cg.width) / image.size.width
        let width = cg.width, height = cg.height
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        let drawn = pixels.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8,
                                          bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return false }
            context.draw(cg, in: CGRect(x: 0, y: 0, width: width, height: height))
            return true
        }
        guard drawn else { return 0 }
        var ink = 0
        let columns = max(0, Int(x.lowerBound * scale)) ..< min(width, Int(x.upperBound * scale))
        let lines = max(0, Int(y.lowerBound * scale)) ..< min(height, Int(y.upperBound * scale))
        for row in lines {
            for column in columns {
                let i = (row * width + column) * 4
                let luminance = 0.299 * Double(pixels[i]) + 0.587 * Double(pixels[i + 1]) + 0.114 * Double(pixels[i + 2])
                let spread = Int(max(pixels[i], pixels[i + 1], pixels[i + 2])) - Int(min(pixels[i], pixels[i + 1], pixels[i + 2]))
                if (dark ? luminance > 150 : luminance < 110), spread < 40 { ink += 1 }
            }
        }
        return ink
    }

    @MainActor private func save(_ shot: XCUIScreenshot, _ name: String) {
        let a = XCTAttachment(screenshot: shot)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? shot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }
}
