import Foundation
import Testing
import UIKit
@testable import Persona

/// How wide Export as Image draws a message (#2216, `ShareCardWidth`, `ShareCardBlocks`).
struct ShareCardWidthTests {
    @Test func plainTextKeepsAPhonesWidth() {
        #expect(ShareCardWidth.width(natural: 0, height: 600) == ShareCardWidth.base)
        // A table or code that fits in the phone's width changes nothing.
        #expect(ShareCardWidth.width(natural: 250, height: 600) == ShareCardWidth.base)
    }

    @Test func tablesAndCodeWidenItToTheirOwnWidthUpToTheCap() {
        #expect(ShareCardWidth.width(natural: 400, height: 900) == 400 + ShareCardWidth.chrome)
        #expect(ShareCardWidth.width(natural: 2000, height: 900) == ShareCardWidth.cap) // wider: its cells wrap
    }

    @Test func aThinStripWidensTowardAReadableLine() {
        let base = ShareCardWidth.base
        #expect(ShareCardWidth.width(natural: 0, height: ShareCardWidth.tallest * base) == base)
        let tall = ShareCardWidth.width(natural: 0, height: 1400)
        #expect(tall > base && tall < ShareCardWidth.cap)
        // However tall, never past the cap.
        #expect(ShareCardWidth.width(natural: 0, height: 20000) == ShareCardWidth.cap)
    }

    /// The sample reply with a five-column table and a long line of code: wider than a phone, within the cap, and the
    /// same in light and dark.
    @MainActor @Test func aReplyWithATableAndCodeIsDrawnWider() throws {
        let light = try #require(MessageShareCard.render(ShareCardExportTests.message, dark: false))
        let dark = try #require(MessageShareCard.render(ShareCardExportTests.message, dark: true))
        #expect(light.size.width > ShareCardWidth.base && light.size.width <= ShareCardWidth.cap)
        #expect(light.size.width == dark.size.width)
        #expect(ShareCardWidth.natural(ShareCardExportTests.reply) > ShareCardWidth.base - ShareCardWidth.chrome)
    }

    /// No transparent band above or below the card (its gradient reaches every edge).
    @MainActor @Test(arguments: [false, true]) func theGradientFillsTheImage(dark: Bool) throws {
        let image = try #require(MessageShareCard.render(ShareCardExportTests.message, dark: dark)?.cgImage)
        for y in [0, image.height / 2, image.height - 1] {
            #expect(try alpha(image, x: image.width / 2, y: y) == 255, "row \(y) of \(image.height)")
        }
    }

    private func alpha(_ image: CGImage, x: Int, y: Int) throws -> UInt8 {
        var pixel: [UInt8] = [0, 0, 0, 0]
        let space = CGColorSpaceCreateDeviceRGB()
        let ctx = try #require(CGContext(data: &pixel, width: 1, height: 1, bitsPerComponent: 8, bytesPerRow: 4, space: space,
                                         bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.draw(image, in: CGRect(x: -x, y: -(image.height - 1 - y), width: image.width, height: image.height))
        return pixel[3]
    }

    @Test func theWideBlocksAreTablesAndFencedCode() {
        let text = """
        Intro line | not a table

        | A | B |
        | --- | :-: |
        | 1 | 2 |

        - a list
        ```swift
        let a = 1
        ```
        After.
        """
        #expect(ShareCardBlocks.wide(text) == ["| A | B |\n| --- | :-: |\n| 1 | 2 |", "```swift\nlet a = 1\n```"])
        #expect(ShareCardBlocks.wide("Just words\nand more") == [])
        #expect(ShareCardBlocks.wide("```\nnever closed") == ["```\nnever closed"])
        #expect(ShareCardBlocks.isDelimiter("---|---") && ShareCardBlocks.isDelimiter("| :--- | ---: |"))
        #expect(!ShareCardBlocks.isDelimiter("| a | b |") && !ShareCardBlocks.isDelimiter("---"))
    }
}
