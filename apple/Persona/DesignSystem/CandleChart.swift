import Charts
import SwiftUI

/// Candlesticks over categories (an extension's `candlestick` chart, UI version 6, #827): a wick from low to high, a
/// body from open to close in the rising or the falling color, and the volume as bars under them on the same axis when
/// every candle has one. Holding (iPhone) or hovering (Mac) a candle reads its open, high, low, close and volume above
/// the plot; nothing held, the last one. Compact (a card): no axis text, no volume. Nothing here knows about
/// extensions: the caller maps its points onto `Candle`.
struct CandleChart: View {
    struct Candle: Hashable, Sendable {
        var label: String
        var open: Double
        var high: Double
        var low: Double
        var close: Double
        var volume: Double?
        var rises: Bool { close >= open }
    }

    var candles: [Candle]
    var rising: Color
    var falling: Color
    /// How a price reads (with its unit).
    var format: (Double) -> String = { DataChart.compact($0) }
    var compact = false

    @State private var picked: String?

    /// The marks' names (never shown: the chart is one element for VoiceOver), typed so they are not localized.
    private static let x: String = "x", y: String = "y", y2: String = "y2"

    private var shown: [Candle] { candles.filter { [$0.open, $0.high, $0.low, $0.close].allSatisfy(\.isFinite) } }
    private var hasVolume: Bool { !candles.isEmpty && candles.allSatisfy { $0.volume != nil } }

    /// The x labels written under the plot: every few, the first and the last always, so they never overlap.
    private var labelled: Set<String> {
        let labels = shown.map(\.label)
        guard labels.count > 2 else { return Set(labels) }
        if compact { return [labels[0], labels[labels.count - 1]] }
        let step = max(1, Int((Double(labels.count - 1) / 5).rounded(.up)))
        return Set(stride(from: 0, to: labels.count, by: step).map { labels[$0] } + [labels[labels.count - 1]])
    }

    var body: some View {
        let items = shown
        let lo = items.map(\.low).min() ?? 0, hi = items.map(\.high).max() ?? 1
        let pad = hi == lo ? max(abs(hi) * 0.01, 1) : (hi - lo) * 0.06
        let plot = compact ? Theme.Size.box80 : Theme.Size.box150
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if !compact { readout(items) }
            VStack(spacing: Theme.Space.xxs) {
                Chart(items, id: \.label) { k in
                    RuleMark(x: .value(Self.x, k.label), yStart: .value(Self.y, k.low), yEnd: .value(Self.y2, k.high))
                        .lineStyle(StrokeStyle(lineWidth: Theme.Size.line))
                        .foregroundStyle(k.rises ? rising : falling)
                        .opacity(picked == nil || picked == k.label ? 1 : 0.4)
                    // A candle that did not move is a hairline, not nothing.
                    RectangleMark(x: .value(Self.x, k.label), yStart: .value(Self.y, min(k.open, k.close)),
                                  yEnd: .value(Self.y2, max(k.open, k.close) + (k.open == k.close ? pad * 0.05 : 0)), width: .ratio(0.6))
                        .foregroundStyle(k.rises ? rising : falling)
                        .opacity(picked == nil || picked == k.label ? 1 : 0.4)
                }
                .chartYScale(domain: (lo - pad)...(hi + pad))
                .chartXAxis { axis }
                // Its labels go under the volume when there is one; a card has none.
                .chartXAxis(hasVolume || compact ? .hidden : .automatic)
                .chartYAxis {
                    AxisMarks(position: .leading, values: .automatic(desiredCount: 4)) { v in
                        AxisGridLine(stroke: StrokeStyle(lineWidth: Theme.Size.hairline)).foregroundStyle(Theme.Fill.chip)
                        AxisValueLabel { if let d = v.as(Double.self) { Text(DataChart.compact(d)) } }
                            .font(Theme.Font.label).foregroundStyle(Color.secondary)
                    }
                }
                .chartYAxis(compact ? .hidden : .automatic)
                .chartXSelection(value: $picked)
                .scaledFrame(height: plot)
                if hasVolume && !compact {
                    Chart(items, id: \.label) { k in
                        BarMark(x: .value(Self.x, k.label), y: .value(Self.y, k.volume ?? 0), width: .ratio(0.6))
                            .foregroundStyle((k.rises ? rising : falling).opacity(0.5))
                            .opacity(picked == nil || picked == k.label ? 1 : 0.4)
                    }
                    .chartXAxis { axis }
                    .chartYAxis {
                        AxisMarks(position: .leading, values: .automatic(desiredCount: 2)) { v in
                            AxisValueLabel { if let d = v.as(Double.self) { Text(DataChart.compact(d)) } }
                                .font(Theme.Font.label).foregroundStyle(Color.secondary)
                        }
                    }
                    .chartXSelection(value: $picked)
                    .scaledFrame(height: Theme.Size.box64)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(items.map(spoken).joined(separator: String(localized: "; ", comment: "Separator between chart series")))
        }
    }

    private var axis: some AxisContent {
        let shown = labelled
        return AxisMarks { v in
            AxisValueLabel {
                let l = v.as(String.self) ?? ""
                Text(shown.contains(l) ? l : "")
            }
            .font(Theme.Font.label).foregroundStyle(Color.secondary)
        }
    }

    /// The held candle's prices (the last one when none is held), the close in its color.
    private func readout(_ items: [Candle]) -> some View {
        let k = picked.flatMap { p in items.first { $0.label == p } } ?? items.last
        return HStack(spacing: Theme.Space.m) {
            if let k {
                Text(k.label).font(Theme.Font.labelStrong).foregroundStyle(.secondary)
                field(String(localized: "Open [Candle]"), format(k.open))
                field(String(localized: "High [Candle]"), format(k.high))
                field(String(localized: "Low [Candle]"), format(k.low))
                field(String(localized: "Close [Candle]"), format(k.close)).foregroundStyle(k.rises ? rising : falling)
                if let v = k.volume { field(String(localized: "Volume [Candle]"), DataChart.compact(v)) }
            } else {
                Text(" ").font(Theme.Font.labelStrong)
            }
        }
        .lineLimit(1)
        .minimumScaleFactor(0.7)
        .accessibilityHidden(true)
    }

    private func field(_ name: String, _ value: String) -> some View {
        HStack(spacing: Theme.Space.xxs) {
            Text(name).foregroundStyle(.secondary)
            Text(value).monospacedDigit()
        }
        .font(Theme.Font.label)
    }

    private func spoken(_ k: Candle) -> String {
        let prices = String(localized: "\(k.label): open \(format(k.open)), high \(format(k.high)), low \(format(k.low)), close \(format(k.close))")
        guard let v = k.volume else { return prices }
        return prices + String(localized: ", volume \(DataChart.compact(v))")
    }
}
