import SwiftUI
import Testing
@testable import Persona

/// The watch home list's row label (`WatchRowLabelStyle`, #6046): every row's symbol stays inside the one icon column,
/// so no symbol runs into its title and every title starts at the same place, at the default and the largest text size.
@MainActor
struct WatchRowLabelStyleTests {
    /// What the home list's rows draw: all-day recording's states, Mark Meeting (the widest), Meeting Recording and
    /// Record, Meeting Minutes.
    static let symbols = [Theme.Icon.allDay, Theme.Icon.allDayPaused, Theme.Icon.error, Theme.Icon.meetingMark,
                          Theme.Icon.recording, Theme.Icon.minutes]

    @Test(arguments: [DynamicTypeSize.large, .xxxLarge, .accessibility3])
    func everySymbolStaysInTheColumn(size: DynamicTypeSize) throws {
        var titleStarts = Set<Int>()
        for symbol in Self.symbols {
            let row = try Row(symbol: symbol, size: size)
            let gap = try #require(row.titleLeft) - (try #require(row.iconRight))
            // The style's gap, less a pixel of antialiasing: a symbol wider than the column eats into it.
            #expect(CGFloat(gap) >= Theme.Space.sm * Row.scale - 1, "\(symbol) at \(size): \(gap) px from its title")
            titleStarts.insert(try #require(row.titleLeft))
        }
        #expect(titleStarts.count == 1, "titles start at \(titleStarts.sorted()) at \(size)")
    }
}

/// A row drawn with the style: a white symbol and a red block standing for its title, on black.
private struct Row {
    static let scale: CGFloat = 2
    /// The symbol's rightmost and the title's leftmost pixel column.
    let iconRight: Int?, titleLeft: Int?

    @MainActor init(symbol: String, size: DynamicTypeSize) throws {
        let view = Label {
            Color.red.frame(width: Theme.Space.s, height: Theme.Space.s)
        } icon: {
            Image(systemName: symbol).foregroundStyle(.white)
        }
        .labelStyle(.watchRow)
        .font(Theme.Font.secondaryMedium)
        .environment(\.dynamicTypeSize, size)
        .background(Color.black)
        let r = ImageRenderer(content: view)
        r.scale = Self.scale
        let image = try #require(r.cgImage)
        let width = image.width, height = image.height
        var buf = [UInt8](repeating: 0, count: width * height * 4)
        let ctx = try #require(CGContext(data: &buf, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                                         space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        ctx.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
        var iconRight: Int?, titleLeft: Int?
        for y in 0..<height {
            for x in 0..<width {
                let i = (y * width + x) * 4
                let (red, green, blue) = (Int(buf[i]), Int(buf[i + 1]), Int(buf[i + 2]))
                if red > 200, green < 80, blue < 80 {
                    titleLeft = min(titleLeft ?? x, x)
                } else if green > 60, blue > 60 {
                    iconRight = max(iconRight ?? x, x)
                }
            }
        }
        self.iconRight = iconRight
        self.titleLeft = titleLeft
    }
}
