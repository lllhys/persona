import SwiftUI

/// System surfaces have a deliberately small, app-independent token vocabulary, also available on watchOS 10.
nonisolated enum ExtensionSurfaceTokens {
    static let actionIcon = "puzzlepiece.extension" // icon: widgets compile this file without Theme; the same symbol as Theme.Icon.skills
    static let spacing: CGFloat = 8
    static let compactSpacing: CGFloat = 4
    static let chartHeight: CGFloat = 56
    static let chartLine: CGFloat = 2
    static let chartBar: CGFloat = 12
    static let chartRadius: CGFloat = 4
    static let chartWash = 0.1
    static let circularLabelMinimumScale = 0.3
    // Matches Theme.interfaceLargestTypeSize without importing the app's theme into system surfaces.
    static let circularLabelLargestTypeSize = DynamicTypeSize.xxxLarge
    static let ringSize: CGFloat = 50
    static let ringStroke: CGFloat = 7
    static let ringInset = ringStroke / 2
    static let ringInnerSize = ringSize - ringStroke * 2
    static let ringStartAngle = Angle.degrees(-90)
    static let ringTrackOpacity = 0.15
    static let ringNumber = Font.system(.subheadline, design: .rounded, weight: .semibold)
    static let ringNumberMinimumScale = 0.5
    static let ringNumberLargestTypeSize = DynamicTypeSize.xxxLarge
    static let ringLabelMinimumScale = 0.5
    // Small widgets, medium columns and ring layouts fit metric, bar label and chart text to the fixed widget canvas:
    // a value shrinks before it is cut short, and every line stays visible at AX sizes.
    static let canvasContentMinimumScale = 0.5
    static let canvasContentLargestTypeSize = DynamicTypeSize.xxxLarge
    static let progressTint = Color.accentColor
    static let label = Font.system(.caption)
    static let body = Font.system(.body)
    static let value = Font.system(.title2, design: .rounded, weight: .semibold)
    static let ink = Color.primary
    static let secondaryInk = Color.secondary
    static let surface = Color.clear
}
