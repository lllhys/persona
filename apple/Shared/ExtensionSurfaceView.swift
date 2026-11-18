import SwiftUI
import Charts
import PersonaWire
import WidgetKit

/// No callbacks, WebViews, app renderer, navigation or credentials enter this read-only renderer.
struct ExtensionSurfaceView: View {
    var view: ExtensionView
    var family: String = "large"

    private var fitsCanvas: Bool {
        ExtensionSurfaceContent.fitsCanvas(view, family: family)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: ExtensionSurfaceTokens.spacing) {
            if ExtensionSurfaceContent.usesMediumColumns(view, family: family) {
                let items = view.sections[0].items
                HStack(alignment: .center, spacing: ExtensionSurfaceTokens.spacing) {
                    VStack(alignment: .leading, spacing: ExtensionSurfaceTokens.spacing) {
                        ForEach(Array(items.dropLast().enumerated()), id: \.offset) { _, item in
                            component(item)
                        }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    if let chart = items.last {
                        component(chart)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                }
            } else {
                ForEach(Array(ExtensionSurfaceContent.items(view, family: family).enumerated()), id: \.offset) { _, item in
                    component(item)
                }
            }
        }
        .font(ExtensionSurfaceTokens.body)
        .foregroundStyle(ExtensionSurfaceTokens.ink)
    }

    @ViewBuilder private func component(_ item: ExtensionComponent) -> some View {
        switch item {
        case .text(let text):
            Text(verbatim: String(text.text.prefix(200))).lineLimit(family == "accessory_inline" || family == "accessory_circular" ? 1 : 3)
        case .metric(let metric):
            if family == "accessory_inline" {
                Text(verbatim: metric.label + " " + metric.value)
            } else if fitsCanvas {
                stat(label: metric.label, value: metric.value, detail: metric.detail)
                    .minimumScaleFactor(ExtensionSurfaceTokens.canvasContentMinimumScale)
                    .dynamicTypeSize(...ExtensionSurfaceTokens.canvasContentLargestTypeSize)
            } else {
                stat(label: metric.label, value: metric.value, detail: metric.detail)
            }
        case .progress(let progress):
            switch ExtensionSurfaceContent.progressPresentation(family: family, style: progress.style) {
            case .accessoryGauge:
                let ratio = ExtensionSurfaceContent.progressRatio(progress.ratio)
                let percentage = ratio.formatted(.percent.precision(.fractionLength(0)))
                Gauge(value: ratio) {
                    Text(verbatim: progress.label)
                } currentValueLabel: {
                    Text(verbatim: progress.label.isEmpty ? percentage : progress.label)
                        .font(ExtensionSurfaceTokens.label)
                        .lineLimit(1)
                        .minimumScaleFactor(ExtensionSurfaceTokens.circularLabelMinimumScale)
                        .dynamicTypeSize(...ExtensionSurfaceTokens.circularLabelLargestTypeSize)
                }
                .gaugeStyle(.accessoryCircularCapacity)
                .accessibilityLabel(Text(verbatim: progress.label))
                .accessibilityValue(Text(verbatim: percentage))
            case .ring:
                ExtensionSurfaceProgressRing(progress: progress)
            case .bar:
                if fitsCanvas {
                    bar(progress)
                        .minimumScaleFactor(ExtensionSurfaceTokens.canvasContentMinimumScale)
                        .dynamicTypeSize(...ExtensionSurfaceTokens.canvasContentLargestTypeSize)
                } else {
                    bar(progress)
                }
            }
        case .row(let row):
            HStack(spacing: ExtensionSurfaceTokens.spacing) {
                VStack(alignment: .leading, spacing: ExtensionSurfaceTokens.compactSpacing) {
                    Text(verbatim: row.title).lineLimit(1)
                    if let subtitle = row.subtitle { Text(verbatim: subtitle).font(ExtensionSurfaceTokens.label).foregroundStyle(ExtensionSurfaceTokens.secondaryInk).lineLimit(1) }
                }
                Spacer(minLength: ExtensionSurfaceTokens.compactSpacing)
                if let value = row.value { Text(verbatim: value).lineLimit(1) }
            }
        case .statGrid(let grid):
            HStack(alignment: .top, spacing: ExtensionSurfaceTokens.spacing) {
                ForEach(Array(grid.stats.prefix(family == "small" ? 2 : 4).enumerated()), id: \.offset) { _, value in
                    stat(label: value.label, value: value.value, detail: value.detail)
                }
            }
        case .chart(let chart):
            if fitsCanvas {
                ExtensionSurfaceChart(chart: chart)
                    .minimumScaleFactor(ExtensionSurfaceTokens.canvasContentMinimumScale)
                    .dynamicTypeSize(...ExtensionSurfaceTokens.canvasContentLargestTypeSize)
            } else {
                ExtensionSurfaceChart(chart: chart)
            }
        default: EmptyView()
        }
    }

    private func bar(_ progress: ExtensionProgressComponent) -> some View {
        ProgressView(value: progress.ratio.isFinite ? min(1, max(0, progress.ratio)) : 0) {
            Text(verbatim: progress.label).font(ExtensionSurfaceTokens.label).lineLimit(1)
        }
    }

    private func stat(label: String, value: String, detail: String?) -> some View {
        VStack(alignment: .leading, spacing: ExtensionSurfaceTokens.compactSpacing) {
            Text(verbatim: label).font(ExtensionSurfaceTokens.label).foregroundStyle(ExtensionSurfaceTokens.secondaryInk).lineLimit(1)
            Text(verbatim: value).font(ExtensionSurfaceTokens.value).lineLimit(1)
            if let detail { Text(verbatim: detail).font(ExtensionSurfaceTokens.label).foregroundStyle(ExtensionSurfaceTokens.secondaryInk).lineLimit(1) }
        }
    }
}

nonisolated enum ExtensionSurfaceContent {
    enum ProgressPresentation {
        case bar, ring, accessoryGauge
    }

    static func progressPresentation(family: String, style: String?) -> ProgressPresentation {
        if family == "accessory_circular" { return .accessoryGauge }
        if ["small", "medium", "large"].contains(family), style == "ring" { return .ring }
        return .bar
    }

    /// Inspect the fitted section structure before the general renderer flattens its first two sections.
    /// The progress may be a ring or a bar: stacked with a metric and a chart, either is taller than the medium canvas (#9644).
    static func usesMediumColumns(_ view: ExtensionView, family: String) -> Bool {
        guard family == "medium", view.sections.count == 1 else { return false }
        let items = view.sections[0].items
        let pair: ArraySlice<ExtensionComponent>
        if items.count == 3, case .metric = items[0] {
            pair = items.dropFirst()
        } else {
            pair = items[...]
        }
        guard pair.count == 2,
              case .progress = pair.first,
              case .chart(let chart) = pair.last else { return false }
        return chart.series.count == 1 && chart.kind != "candlestick"
    }

    /// Layouts that fit metric, bar and chart text to the widget's fixed canvas: a small widget, a medium widget's columns
    /// and any Home Screen layout with a ring.
    static func fitsCanvas(_ view: ExtensionView, family: String) -> Bool {
        family == "small" || usesMediumColumns(view, family: family) || items(view, family: family).contains {
            guard case .progress(let progress) = $0 else { return false }
            return progressPresentation(family: family, style: progress.style) == .ring
        }
    }

    static func progressRatio(_ ratio: Double) -> Double {
        ratio.isFinite ? min(1, max(0, ratio)) : 0
    }

    static func items(_ view: ExtensionView, family: String) -> [ExtensionComponent] {
        let all = view.sections.prefix(2).flatMap(\.items)
        if family.hasPrefix("accessory_") {
            return Array(all.filter {
                switch $0 {
                case .metric, .text: true
                case .progress: family != "accessory_inline"
                default: false
                }
            }.prefix(1))
        }
        let limit = family == "small" ? 4 : family == "medium" ? 6 : 12
        return Array(all.filter {
            switch $0 {
            case .metric, .statGrid, .progress, .chart, .text, .row: true
            default: false
            }
        }.prefix(limit))
    }
}

private struct ExtensionSurfaceProgressRing: View {
    var progress: ExtensionProgressComponent

    var body: some View {
        let ratio = ExtensionSurfaceContent.progressRatio(progress.ratio)
        let percentage = ratio.formatted(.percent.precision(.fractionLength(0)))
        HStack(spacing: ExtensionSurfaceTokens.spacing) {
            ZStack {
                Circle()
                    .strokeBorder(ExtensionSurfaceTokens.progressTint.opacity(ExtensionSurfaceTokens.ringTrackOpacity),
                                  lineWidth: ExtensionSurfaceTokens.ringStroke)
                if ratio > 0 {
                    Circle().trim(from: 0, to: ratio)
                        .stroke(ExtensionSurfaceTokens.progressTint,
                                style: StrokeStyle(lineWidth: ExtensionSurfaceTokens.ringStroke, lineCap: .round))
                        .rotationEffect(ExtensionSurfaceTokens.ringStartAngle)
                        .padding(ExtensionSurfaceTokens.ringInset)
                        .widgetAccentable()
                }
                Text(verbatim: percentage)
                    .font(ExtensionSurfaceTokens.ringNumber)
                    .lineLimit(1)
                    .minimumScaleFactor(ExtensionSurfaceTokens.ringNumberMinimumScale)
                    .dynamicTypeSize(...ExtensionSurfaceTokens.ringNumberLargestTypeSize)
                    .frame(width: ExtensionSurfaceTokens.ringInnerSize)
            }
            .frame(width: ExtensionSurfaceTokens.ringSize, height: ExtensionSurfaceTokens.ringSize)
            .accessibilityHidden(true)
            Text(verbatim: progress.label)
                .font(ExtensionSurfaceTokens.label)
                .lineLimit(2)
                .minimumScaleFactor(ExtensionSurfaceTokens.ringLabelMinimumScale)
                .dynamicTypeSize(...ExtensionSurfaceTokens.canvasContentLargestTypeSize)
                .frame(maxWidth: .infinity, alignment: .leading)
                .frame(height: ExtensionSurfaceTokens.ringSize)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: progress.label))
        .accessibilityValue(Text(verbatim: percentage))
    }
}

/// Monochrome labelled small multiples: series identity never depends on hue or rank. System widgets cannot hover.
/// Every mark has a VoiceOver table equivalent; the endpoint stays visible without interaction.
private struct ExtensionSurfaceChart: View {
    var chart: ExtensionChartComponent
    var body: some View {
        VStack(alignment: .leading, spacing: ExtensionSurfaceTokens.compactSpacing) {
            if let title = chart.title { Text(verbatim: title).font(ExtensionSurfaceTokens.label).lineLimit(1) }
            if chart.series.count > 1 {
                Text(verbatim: chart.series.compactMap(\.name).joined(separator: " · "))
                    .font(ExtensionSurfaceTokens.label).foregroundStyle(ExtensionSurfaceTokens.secondaryInk)
            }
            ForEach(Array(chart.series.prefix(4).enumerated()), id: \.offset) { _, series in
                let points = Array(series.points.filter { $0.value.isFinite }.prefix(64))
                if let name = series.name { Text(verbatim: name).font(ExtensionSurfaceTokens.label).lineLimit(1) }
                if chart.kind == "candlestick" {
                    // Preserve the OHLC meaning instead of inventing a close-price line.
                    ForEach(Array(points.prefix(4).enumerated()), id: \.offset) { _, point in
                        Text(verbatim: [point.label, point.open.map { String($0) }, point.high.map { String($0) }, point.low.map { String($0) }, point.close.map { String($0) }].compactMap { $0 }.joined(separator: " · "))
                            .font(ExtensionSurfaceTokens.label).lineLimit(1)
                    }
                } else {
                    Chart(Array(points.enumerated()), id: \.offset) { index, point in
                        if chart.kind == "line" || chart.kind == "area" {
                            if chart.kind == "area" {
                                AreaMark(x: .value(String(localized: "Index"), index), y: .value(String(localized: "Value"), point.value))
                                    .foregroundStyle(ExtensionSurfaceTokens.ink.opacity(ExtensionSurfaceTokens.chartWash))
                            }
                            LineMark(x: .value(String(localized: "Index"), index), y: .value(String(localized: "Value"), point.value))
                                .lineStyle(StrokeStyle(lineWidth: ExtensionSurfaceTokens.chartLine, lineCap: .round, lineJoin: .round))
                                .foregroundStyle(ExtensionSurfaceTokens.ink)
                        } else {
                            // Pie/donut data is compared on a common zero baseline, with labels retained.
                            BarMark(x: .value(String(localized: "Index"), index), y: .value(String(localized: "Value"), point.value), width: .fixed(ExtensionSurfaceTokens.chartBar))
                                .cornerRadius(ExtensionSurfaceTokens.chartRadius)
                                .foregroundStyle(ExtensionSurfaceTokens.ink)
                        }
                    }
                    .chartXAxis(.hidden).chartYAxis(.hidden)
                    .frame(height: ExtensionSurfaceTokens.chartHeight)
                    .accessibilityRepresentation {
                        VStack {
                            ForEach(Array(points.enumerated()), id: \.offset) { _, point in
                                Text(verbatim: point.label + ": " + (point.display ?? String(point.value)) + (chart.unit ?? ""))
                            }
                        }
                    }
                    if let last = points.last {
                        Text(verbatim: last.label + ": " + (last.display ?? String(last.value)) + (chart.unit ?? ""))
                            .font(ExtensionSurfaceTokens.label).lineLimit(1)
                    }
                }
            }
        }
    }
}
