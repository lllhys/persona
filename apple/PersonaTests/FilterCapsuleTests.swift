import SwiftUI
import Testing
@testable import Persona

/// The Agents filter capsule draws every filter's whole name (#9121). Before, a filter that was neither chosen nor in the
/// middle of the strip showed its first two characters and an ellipsis, and 「完成未看」 read 「完成…」. Now a chip draws its
/// name whole whether it is chosen or not, in every language and at every text size, and a strip that does not fit scrolls.
/// The scheme runs in zh-Hans: the English and Japanese names come from the string catalog, as `StateNamesTests` reads it.
@MainActor struct FilterCapsuleTests {
    private static let apple = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
    private static let languages = ["en", "zh-Hans", "ja"]
    /// The mean alpha difference of two inks that are the same drawing (anti-aliasing apart), and of a name against the
    /// same name cut to two characters and an ellipsis.
    private static let sameAtMost = 6.0, cutAtLeast = 12.0

    /// The catalog key of each filter's name (`AgentFilter.title`).
    private static let keys: [AgentFilter: String] = [
        .all: "All", .needsYou: "Needs You [State Names]", .unread: "Unread [State Names]", .working: "Working [State Names]",
        .backgroundWaiting: "Waiting in the background", .feishu: "Lark Group [Models e86099]", .terminal: "Terminal",
    ]

    /// The catalog's value of every key in one language.
    private static func catalog(_ language: String) throws -> [String: String] {
        let data = try Data(contentsOf: apple.appending(path: "Shared/Localization/Localizable.xcstrings"))
        let strings = try #require((try JSONSerialization.jsonObject(with: data) as? [String: Any])?["strings"] as? [String: Any])
        var out: [String: String] = [:]
        for (key, entry) in strings {
            let localization = ((entry as? [String: Any])?["localizations"] as? [String: Any])?[language] as? [String: Any]
            if let value = (localization?["stringUnit"] as? [String: Any])?["value"] as? String { out[key] = value }
        }
        return out
    }

    /// Every filter's name in one language.
    private static func names(_ language: String) throws -> [(filter: AgentFilter, title: String)] {
        let catalog = try catalog(language)
        return try AgentFilter.allCases.map { filter in
            let key = try #require(keys[filter], "\(filter) needs its catalog key here, so that its name is checked")
            return (filter, try #require(catalog[key], "\(language) has no \(key)"))
        }
    }

    /// What a view paints: the alpha of its pixels, cropped to the box of those that are clearly painted.
    private struct Ink {
        var width = 0, height = 0
        var alpha: [UInt8] = []

        /// The mean difference in alpha over the two inks laid on one canvas by their top left corners: 0 for the same
        /// drawing, large for two names that differ in a letter or in how far they go.
        func distance(to other: Ink) -> Double {
            let (w, h) = (max(width, other.width), max(height, other.height))
            guard w > 0, h > 0 else { return 255 }
            var total = 0.0
            for y in 0..<h {
                for x in 0..<w {
                    let a = x < width && y < height ? Double(alpha[y * width + x]) : 0
                    let b = x < other.width && y < other.height ? Double(other.alpha[y * other.width + x]) : 0
                    total += abs(a - b)
                }
            }
            return total / Double(w * h)
        }
    }

    private func ink(_ view: some View, _ size: DynamicTypeSize) throws -> Ink {
        let renderer = ImageRenderer(content: view.fixedSize().environment(\.dynamicTypeSize, size).environment(\.colorScheme, .light))
        renderer.scale = 2
        let image = try #require(renderer.cgImage)
        let (w, h) = (image.width, image.height)
        var pixels = [UInt8](repeating: 0, count: w * h * 4)
        let context = try #require(CGContext(data: &pixels, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        context.draw(image, in: CGRect(x: 0, y: 0, width: w, height: h))
        var box = (minX: w, maxX: -1, minY: h, maxY: -1)
        for y in 0..<h { for x in 0..<w where pixels[(y * w + x) * 4 + 3] >= 32 {
            box = (min(box.minX, x), max(box.maxX, x), min(box.minY, y), max(box.maxY, y))
        } }
        guard box.maxX >= box.minX else { return Ink() }
        var alpha: [UInt8] = []
        for y in box.minY...box.maxY { for x in box.minX...box.maxX { alpha.append(pixels[(y * w + x) * 4 + 3]) } }
        return Ink(width: box.maxX - box.minX + 1, height: box.maxY - box.minY + 1, alpha: alpha)
    }

    private func name(_ title: String) -> some View { Text(title).font(Theme.Font.secondaryMedium).lineLimit(1) }
    private func chip(_ title: String, selected: Bool = false) -> some View {
        CapsuleFilterItem(item: .init(id: "filter", title: title), selected: selected) {}
    }

    /// The keys above are the ones `AgentFilter.title` reads: in zh-Hans, the language this scheme runs in, the app's names
    /// are the catalog's, so the English and Japanese names read from the same keys are the ones the capsule draws there.
    @Test func theCatalogKeysAreTheFiltersNames() throws {
        for (filter, title) in try Self.names("zh-Hans") { #expect(filter.title == title, "\(filter)") }
    }

    /// The comparison below tells a cut name from a whole one, and a name with an ellipsis from one without.
    @Test(arguments: [("完成未看", "完成…"), ("工作中", "工作…"), ("Working", "Wo…"), ("作業中", "作業…"), ("Larkグループ", "La…")])
    func theComparisonSeesACutName(whole: String, cut: String) throws {
        let distance = try ink(name(whole), .large).distance(to: ink(name(cut), .large))
        #expect(distance > Self.cutAtLeast, "\(whole) against \(cut): \(distance)")
    }

    /// A chip paints its filter's name, all of it: no name is cut to a few characters and an ellipsis, in any language,
    /// at the default text size and the largest. A name that is not the chip's own would not paint the same pixels.
    @Test(arguments: [DynamicTypeSize.large, .accessibility5])
    func aChipPaintsItsWholeName(size: DynamicTypeSize) throws {
        for language in Self.languages {
            for (filter, title) in try Self.names(language) {
                #expect(!title.contains("…") && !title.contains("..."), "\(language) \(filter)")
                let distance = try ink(name(title), size).distance(to: ink(chip(title), size))
                #expect(distance < Self.sameAtMost, "\(language) \(title) at \(size): \(distance)")
            }
        }
    }

    /// Being chosen changes a chip's look, not its width: the strip does not move under the finger when one is picked.
    @Test func aChipKeepsItsWidthWhenChosen() throws {
        for language in Self.languages {
            for (filter, title) in try Self.names(language) {
                let item = CapsuleFilterItemData(id: filter.rawValue, title: title, count: 12, tone: filter.tone, symbol: filter.symbol)
                func width(selected: Bool) -> CGFloat {
                    UIHostingController(rootView: CapsuleFilterItem(item: item, selected: selected) {}.fixedSize())
                        .sizeThatFits(in: CGSize(width: 10_000, height: 1_000)).width
                }
                #expect(abs(width(selected: false) - width(selected: true)) <= 1, "\(language) \(title)")
            }
        }
    }
}
