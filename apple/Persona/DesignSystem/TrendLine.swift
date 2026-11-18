import Charts
import SwiftUI

/// A small trend line with no axes (a stock's day in a row, the week's use under a number; an extension's `spark`,
/// UI version 6, #827): a line in a box the size of a word, optionally shaded under it, in the quiet ink unless the
/// caller gives a color. VoiceOver reads where it went and its lowest and highest.
struct TrendLine: View {
    var values: [Double]
    var color: Color = .secondary
    var fill = false
    var width: CGFloat? = Theme.Size.box56
    var height: CGFloat = Theme.Size.iconXS

    /// The marks' names (never shown: the chart is one element for VoiceOver), typed so they are not localized.
    private static let x: String = "x", y: String = "y", floor: String = "floor"

    private var points: [(Int, Double)] { values.enumerated().filter { $0.element.isFinite }.map { ($0.offset, $0.element) } }

    var body: some View {
        let pts = points
        let lo = pts.map(\.1).min() ?? 0, hi = pts.map(\.1).max() ?? 1
        // A flat line sits in the middle rather than on the floor.
        let pad = hi == lo ? max(abs(hi), 1) : (hi - lo) * 0.1
        Chart {
            ForEach(pts, id: \.0) { i, v in
                if fill {
                    AreaMark(x: .value(Self.x, i), yStart: .value(Self.floor, lo - pad), yEnd: .value(Self.y, v))
                        .foregroundStyle(LinearGradient(colors: [color.opacity(0.28), color.opacity(0.02)], startPoint: .top, endPoint: .bottom))
                        .interpolationMethod(.monotone)
                }
                LineMark(x: .value(Self.x, i), y: .value(Self.y, v))
                    .foregroundStyle(color)
                    .interpolationMethod(.monotone)
                    .lineStyle(StrokeStyle(lineWidth: Theme.Size.lineMedium, lineCap: .round, lineJoin: .round))
            }
        }
        .chartXAxis(.hidden)
        .chartYAxis(.hidden)
        .chartLegend(.hidden)
        .chartYScale(domain: (lo - pad)...(hi + pad))
        .chartXScale(domain: 0...max(values.count - 1, 1))
        .scaledFrame(width: width, height: height)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Self.spoken(values))
    }

    /// What VoiceOver says: where it went (from the first to the last), and its lowest and highest.
    static func spoken(_ values: [Double]) -> String {
        let finite = values.filter(\.isFinite)
        guard let first = finite.first, let last = finite.last else { return "" }
        let lo = DataChart.compact(finite.min() ?? first), hi = DataChart.compact(finite.max() ?? first)
        let from = DataChart.compact(first), to = DataChart.compact(last)
        if last > first { return String(localized: "Trending up, from \(from) to \(to), low \(lo), high \(hi)") }
        if last < first { return String(localized: "Trending down, from \(from) to \(to), low \(lo), high \(hi)") }
        return String(localized: "Flat at \(to), low \(lo), high \(hi)")
    }
}
