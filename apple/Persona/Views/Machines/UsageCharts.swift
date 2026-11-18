import Charts
import SwiftUI
import PersonaWire

/// A machine's token usage on its page (docs/server/machines.md "Token usage", #4973): the range, model and launcher filters,
/// the range's sums, throughput per minute (all tokens, or the four kinds stacked; a legend entry hides its kind:
/// cache reads are often nine tenths of it), the cache hit rate, and each model's share. Holding a chart shows that
/// moment in both, as the monitoring charts do.
struct MachineUsageSection: View {
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var phase
    var ref: MachineRef

    @State private var range: UsageRange = Self.initialRange
    @State private var stacked = true
    @State private var modelFilter: String?
    @State private var launcherFilter: String?
    @State private var hidden: Set<UsageKind> = []
    @State private var data: MachineUsageSeries?
    /// The hub has no usage route (404): deploy a newer one.
    @State private var unsupported = false
    @State private var failure: String?
    @State private var selection: Date?

    private static var initialRange: UsageRange {
        #if DEBUG
        // `-openUsageRange 1h|24h|7d`: the section opens on that range (screenshots, UI tests).
        if let r = UserDefaults.standard.string(forKey: "openUsageRange").flatMap(UsageRange.init(rawValue:)) { return r }
        #endif
        return .day
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Text("Token Usage").font(Theme.Font.heading).padding(.horizontal, Theme.Space.xs)
            if unsupported {
                Text("Deploy a newer hub to see this computer's token usage here.")
                    .font(Theme.Font.secondary).foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .card(padding: 14)
            } else {
                VStack(alignment: .leading, spacing: Theme.Space.m) {
                    SegmentedChoice(String(localized: "Time Range"), UsageRange.allCases.map { ($0, $0.title) }, selection: $range)
                        .accessibilityIdentifier("usage-range")
                    filters
                    if let data {
                        UsageTotals(total: data.total)
                        throughput(data)
                        hitRate(data)
                    } else if let failure {
                        Text(failure).font(Theme.Font.secondary).foregroundStyle(.secondary)
                    } else {
                        ProgressView().frame(maxWidth: .infinity).padding(.vertical, Theme.Space.xl)
                    }
                }
                .card(padding: 14)
                if let data, !data.models.isEmpty { modelShares(data) }
            }
        }
        .tabActiveTask(id: Poll(range: range, model: modelFilter, launcher: launcherFilter, active: phase == .active)) {
            guard phase == .active else { return }
            while !Task.isCancelled {
                await load()
                try? await Task.sleep(for: .seconds(range == .hour ? 20 : 60))
            }
        }
        .onChange(of: range) { selection = nil }
    }

    nonisolated private struct Poll: Hashable, Sendable {
        var range: UsageRange
        var model: String?
        var launcher: String?
        var active: Bool
    }

    // MARK: Filters

    private var filters: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            filterMenus
            if data?.launchersKnown == false {
                Text("This computer's node doesn't record launchers yet. Update it to filter by launcher.")
                    .font(Theme.Font.label).foregroundStyle(.secondary)
            }
        }
    }

    private var filterMenus: some View {
        HStack(spacing: Theme.Space.s) {
            Menu {
                Button(String(localized: "All Models")) { modelFilter = nil }
                ForEach(data?.models ?? []) { m in Button(m.name) { modelFilter = m.name } }
            } label: {
                Chip(text: modelFilter ?? String(localized: "All Models"), symbol: Theme.Icon.choices,
                     look: modelFilter == nil ? .neutral : .tint(Theme.accent), shrinks: true)
            }
            .accessibilityIdentifier("usage-model-filter")
            Menu {
                Button(String(localized: "All Launchers")) { launcherFilter = nil }
                ForEach(data?.launchers ?? []) { l in Button(Self.launcherName(l.name)) { launcherFilter = l.name } }
            } label: {
                Chip(text: launcherFilter.map(Self.launcherName) ?? String(localized: "All Launchers"), symbol: Theme.Icon.choices,
                     look: launcherFilter == nil ? .neutral : .tint(Theme.accent), shrinks: true)
            }
            .disabled(data?.launchersKnown == false)
            .accessibilityIdentifier("usage-launcher-filter")
            Spacer(minLength: 0)
        }
        .buttonStyle(.plain)
    }

    /// A launcher's name; the unknown one (a session typed in a terminal) is "Other".
    nonisolated static func launcherName(_ name: String) -> String {
        name.isEmpty ? String(localized: "Other", comment: "Token usage: sessions with no known launcher") : name
    }

    // MARK: Throughput

    private func throughput(_ data: MachineUsageSeries) -> some View {
        let picked = selection.flatMap { UsageChartData.nearest(data.points, to: $0, step: TimeInterval(data.step)) }
        let kinds = UsageKind.allCases.filter { !hidden.contains($0) }
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack {
                Text("Throughput").font(Theme.Font.secondaryStrong)
                Spacer(minLength: Theme.Space.s)
                SegmentedChoice(String(localized: "Chart"), [(false, String(localized: "Total")), (true, String(localized: "By Kind"))],
                                selection: $stacked)
                    .fixedSize()
                    .accessibilityIdentifier("usage-stacked")
            }
            HStack(alignment: .firstTextBaseline) {
                if let picked {
                    Text(picked.date, format: .dateTime.weekday(range == .week ? .abbreviated : .omitted).hour().minute())
                        .font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary)
                } else {
                    Text("Per minute").font(Theme.Font.label).foregroundStyle(.secondary)
                }
                Spacer(minLength: Theme.Space.s)
                // What the chart shows: every token, or the kinds not hidden.
                let counted = stacked ? kinds : UsageKind.allCases
                let at = picked ?? data.points.last(where: { !$0.gap })
                let shown = at.map { at in UsageChartData.perMinute(counted.reduce(Int64(0)) { $0 + at.tokens.value($1) }, data, at: at.t) } ?? 0
                Text(TokenFormat.perMinute(shown)).font(Theme.Font.bodyStrong.monospacedDigit())
                    .contentTransition(.numericText())
            }
            UsageThroughputChart(data: data, kinds: stacked ? kinds : [], domain: domain(data), selection: $selection)
                .overlay { if data.total.total == 0 { emptyNote } }
            if stacked { legend(data, picked: picked) }
            if data.points.contains(where: \.gap) {
                Text("Shaded: the computer was offline. Its usage fills in once it's back.")
                    .font(Theme.Font.label).foregroundStyle(.secondary)
            }
        }
    }

    private var emptyNote: some View {
        VStack(spacing: Theme.Space.s) {
            if modelFilter != nil || launcherFilter != nil {
                Text("No usage for this filter in this range.").font(Theme.Font.secondary).foregroundStyle(.secondary)
                Button(String(localized: "Clear Filters")) { modelFilter = nil; launcherFilter = nil }
                    .font(Theme.Font.secondaryMedium).lineLimit(1)
            } else {
                Text(range == .week ? String(localized: "No usage in the last 7 days.")
                     : String(localized: "No usage in this range. Try a longer one."))
                    .font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
            }
        }
        .padding(Theme.Space.m)
    }

    /// The four kinds with their numbers (at the held moment, per minute; else the range's sums); tapping one hides
    /// or shows it.
    private func legend(_ data: MachineUsageSeries, picked: UsagePoint?) -> some View {
        LazyVGrid(columns: [GridItem(.flexible(), spacing: Theme.Space.l), GridItem(.flexible())], alignment: .leading, spacing: Theme.Space.s) {
            ForEach(UsageKind.allCases) { kind in
                let off = hidden.contains(kind)
                let value = picked.map { TokenFormat.count(UsageChartData.perMinute($0.tokens.value(kind), data, at: $0.t)) }
                    ?? TokenFormat.count(data.total.value(kind))
                Button {
                    withAnimation(Theme.Motion.standard) {
                        if off { hidden.remove(kind) } else if hidden.count < UsageKind.allCases.count - 1 { hidden.insert(kind) }
                    }
                } label: {
                    HStack(spacing: Theme.Space.sm) {
                        Circle().fill(off ? Color.secondary.opacity(0.3) : UsageColor.of(kind))
                            .scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
                        Text(kind.title).font(Theme.Font.caption).foregroundStyle(off ? .tertiary : .primary).lineLimit(1)
                        Spacer(minLength: Theme.Space.xs)
                        Text(value).font(Theme.Font.captionMedium.monospacedDigit()).foregroundStyle(off ? .tertiary : .primary).lineLimit(1)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(kind.title)
                .accessibilityValue(off ? String(localized: "Hidden") : value)
                .accessibilityHint(String(localized: "Shows or hides it in the chart"))
                .accessibilityIdentifier("usage-legend-" + kind.rawValue)
            }
        }
    }

    // MARK: Hit rate

    private func hitRate(_ data: MachineUsageSeries) -> some View {
        let picked = selection.flatMap { UsageChartData.nearest(data.points, to: $0, step: TimeInterval(data.step)) }
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(alignment: .firstTextBaseline) {
                Text("Cache Hit Rate").font(Theme.Font.secondaryStrong)
                Spacer(minLength: Theme.Space.s)
                let rate = picked.map { $0.tokens.hitRate } ?? data.total.hitRate
                Text(rate.map { $0.formatted(.percent.precision(.fractionLength(0))) } ?? "—")
                    .font(Theme.Font.bodyStrong.monospacedDigit())
            }
            UsageHitRateChart(data: data, domain: domain(data), selection: $selection)
            Text("Cache read ÷ (input + cache write + cache read). Steps with very few replies aren't drawn.")
                .font(Theme.Font.label).foregroundStyle(.secondary)
        }
    }

    // MARK: Models

    private func modelShares(_ data: MachineUsageSeries) -> some View {
        let top = Double(data.models.first?.tokens.total ?? 1)
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            Text("By Model").font(Theme.Font.secondaryStrong).foregroundStyle(.secondary).padding(.horizontal, Theme.Space.xs)
            VStack(spacing: 0) {
                ForEach(data.models) { m in
                    Button { modelFilter = modelFilter == m.name ? nil : m.name } label: {
                        VStack(alignment: .leading, spacing: Theme.Space.sm) {
                            HStack(alignment: .firstTextBaseline) {
                                Text(m.name).font(Theme.Font.code).lineLimit(1).truncationMode(.middle)
                                Spacer(minLength: Theme.Space.s)
                                Text(TokenFormat.count(m.tokens.total)).font(Theme.Font.secondaryStrong.monospacedDigit())
                            }
                            HStack(spacing: Theme.Space.s) {
                                MeterBar(fraction: top > 0 ? Double(m.tokens.total) / top : 0, color: .secondary)
                                if let rate = m.tokens.hitRate {
                                    Text("Hit \(rate.formatted(.percent.precision(.fractionLength(0))))")
                                        .font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                                }
                            }
                        }
                        .padding(.vertical, Theme.Space.s)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityHint(String(localized: "Shows only this model's usage"))
                    if m.id != data.models.last?.id { Divider() }
                }
            }
            .card(padding: 14)
        }
    }

    // MARK: Loading

    private func domain(_ data: MachineUsageSeries) -> ClosedRange<Date> {
        let start = data.points.first?.date ?? .now.addingTimeInterval(-range.seconds)
        let end = (data.points.last?.date ?? .now).addingTimeInterval(TimeInterval(data.step))
        return start...max(end, start.addingTimeInterval(1))
    }

    private func load() async {
        guard let api = model.api(ref.group) else { return }
        let key = (range, modelFilter, launcherFilter)
        do {
            let fresh = try await api.machineUsage(ref.id, range: key.0, model: key.1, launcher: key.2)
            guard key == (range, modelFilter, launcherFilter) else { return }
            data = fresh
            failure = nil
            unsupported = false
        } catch {
            if error.isCancellation { return }
            if let e = error as? APIError, e.status == 404, model.machine(ref) != nil {
                unsupported = true
            } else if data == nil {
                failure = error.shown ?? String(localized: "Can't reach the hub. This refreshes automatically once it's back.")
            }
        }
    }
}

/// The range's sums: all tokens, output, the cache hit rate, the replies.
struct UsageTotals: View {
    var total: UsageTokens

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            cell(String(localized: "Total [Token Usage]"), TokenFormat.count(total.total))
            cell(UsageKind.output.title, TokenFormat.count(total.output))
            cell(String(localized: "Hit Rate"), total.hitRate.map { $0.formatted(.percent.precision(.fractionLength(0))) } ?? "—")
            cell(String(localized: "Replies"), TokenFormat.count(total.requests))
        }
    }

    private func cell(_ title: String, _ value: String) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            Text(title).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            Text(value).font(Theme.Font.bodyStrong.monospacedDigit()).lineLimit(1).minimumScaleFactor(0.7)
                .contentTransition(.numericText())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// What the usage charts draw from a series: per-minute values, the offline runs, the held point.
nonisolated enum UsageChartData {
    struct Value: Identifiable {
        var id: String { "\(kind.rawValue)@\(date.timeIntervalSince1970)" }
        var kind: UsageKind
        var date: Date
        var value: Double
    }

    /// The sum n of the step starting at t, per minute (the step under way: over the part that had passed, #9205).
    static func perMinute(_ n: Int64, _ data: MachineUsageSeries, at t: Int64) -> Int64 { Int64((Double(n) / data.minutes(at: t)).rounded()) }

    /// Each kind's per-minute value at every step (an offline step is 0, drawn under its shade).
    static func stacked(_ data: MachineUsageSeries, kinds: [UsageKind]) -> [Value] {
        kinds.flatMap { kind in
            data.points.map { p in Value(kind: kind, date: p.date, value: Double(p.tokens.value(kind)) / data.minutes(at: p.t)) }
        }
    }

    /// The runs of offline steps, from the first's start to the last's end.
    static func offline(_ data: MachineUsageSeries) -> [ClosedRange<Date>] {
        var out: [ClosedRange<Date>] = []
        var start: Date?
        for (i, p) in data.points.enumerated() {
            if p.gap, start == nil { start = p.date }
            let ends = !p.gap || i == data.points.count - 1
            if let s = start, ends {
                out.append(s...(p.gap ? p.date.addingTimeInterval(TimeInterval(data.step)) : p.date))
                start = nil
            }
        }
        return out
    }

    /// The hit rate's runs: a step with at least `minReplies` replies is drawn; fewer (or offline) breaks the line.
    static func hitRate(_ data: MachineUsageSeries, minReplies: Int = 3) -> [(segment: Int, date: Date, rate: Double)] {
        var out: [(Int, Date, Double)] = []
        var segment = 0
        for p in data.points {
            guard !p.gap, p.tokens.requests >= minReplies, let r = p.tokens.hitRate else { segment += 1; continue }
            out.append((segment, p.date, r))
        }
        return out.map { (segment: $0.0, date: $0.1, rate: $0.2) }
    }

    /// The step nearest to `date`, within one step; nil over an offline one.
    static func nearest(_ points: [UsagePoint], to date: Date, step: TimeInterval) -> UsagePoint? {
        guard let p = points.min(by: { abs($0.date.timeIntervalSince(date)) < abs($1.date.timeIntervalSince(date)) }),
              abs(p.date.timeIntervalSince(date)) <= step, !p.gap else { return nil }
        return p
    }
}

/// Tokens per minute over the range: the kinds stacked from the bottom (input, output, cache write, cache read), or
/// every token as one line when `kinds` is empty; offline time shaded.
struct UsageThroughputChart: View {
    var data: MachineUsageSeries
    var kinds: [UsageKind]
    var domain: ClosedRange<Date>
    @Binding var selection: Date?

    var body: some View {
        let picked = selection.flatMap { UsageChartData.nearest(data.points, to: $0, step: TimeInterval(data.step)) }
        Chart {
            ForEach(Array(UsageChartData.offline(data).enumerated()), id: \.offset) { _, run in
                RectangleMark(xStart: .value(String(localized: "Time"), run.lowerBound), xEnd: .value(String(localized: "Time"), run.upperBound))
                    .foregroundStyle(Color.primary.opacity(0.06))
            }
            if kinds.isEmpty {
                ForEach(data.points, id: \.t) { p in
                    let v = Double(p.tokens.total) / data.minutes(at: p.t)
                    AreaMark(x: .value(String(localized: "Time"), p.date), y: .value(String(localized: "Tokens"), v))
                        .foregroundStyle(UsageColor.total.opacity(0.12))
                        .interpolationMethod(.monotone)
                    LineMark(x: .value(String(localized: "Time"), p.date), y: .value(String(localized: "Tokens"), v))
                        .foregroundStyle(UsageColor.total)
                        .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                        .interpolationMethod(.monotone)
                }
            } else {
                ForEach(UsageChartData.stacked(data, kinds: kinds)) { v in
                    AreaMark(x: .value(String(localized: "Time"), v.date), y: .value(String(localized: "Tokens"), v.value), stacking: .standard)
                        .foregroundStyle(by: .value(String(localized: "Kind"), v.kind.title))
                        .interpolationMethod(.monotone)
                }
            }
            if let picked {
                RuleMark(x: .value(String(localized: "Selected [Metric Charts 45c21a]"), picked.date))
                    .foregroundStyle(Color.secondary.opacity(0.5))
                    .lineStyle(StrokeStyle(lineWidth: 1))
            }
        }
        .chartForegroundStyleScale(domain: kinds.map(\.title), range: kinds.map(UsageColor.of))
        .chartLegend(.hidden)
        .usageAxes(domain: domain, range: data.range) { TokenFormat.count(Int64($0)) }
        .chartXSelection(value: $selection)
        .scaledFrame(height: Theme.Size.box150)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(localized: "Token throughput"))
        .accessibilityValue(summary)
        .accessibilityIdentifier("usage-throughput")
    }

    private var summary: String {
        let peak = data.points.map { UsageChartData.perMinute($0.tokens.total, data, at: $0.t) }.max() ?? 0
        return String(localized: "\(TokenFormat.count(data.total.total)) tokens in this range, peak \(TokenFormat.perMinute(peak))")
    }
}

/// The cache hit rate over the range, one line broken where there were too few replies to say.
struct UsageHitRateChart: View {
    var data: MachineUsageSeries
    var domain: ClosedRange<Date>
    @Binding var selection: Date?

    var body: some View {
        let rates = UsageChartData.hitRate(data)
        let low = min(0.5, ((rates.map(\.rate).min() ?? 1) * 10).rounded(.down) / 10)
        let picked = selection.flatMap { UsageChartData.nearest(data.points, to: $0, step: TimeInterval(data.step)) }
        Chart {
            ForEach(rates, id: \.date) { r in
                LineMark(x: .value(String(localized: "Time"), r.date), y: .value(String(localized: "Hit Rate"), r.rate),
                         series: .value(String(localized: "Segment"), r.segment))
                    .foregroundStyle(UsageColor.hitRate)
                    .lineStyle(StrokeStyle(lineWidth: 2, lineCap: .round, lineJoin: .round))
                    .interpolationMethod(.monotone)
            }
            if let picked, let r = picked.tokens.hitRate, picked.tokens.requests >= 3 {
                RuleMark(x: .value(String(localized: "Selected [Metric Charts 45c21a]"), picked.date))
                    .foregroundStyle(Color.secondary.opacity(0.5))
                    .lineStyle(StrokeStyle(lineWidth: 1))
                PointMark(x: .value(String(localized: "Time"), picked.date), y: .value(String(localized: "Hit Rate"), r))
                    .foregroundStyle(UsageColor.hitRate)
            }
        }
        .chartYScale(domain: low...1)
        .usageAxes(domain: domain, range: data.range) { $0.formatted(.percent.precision(.fractionLength(0))) }
        .chartXSelection(value: $selection)
        .scaledFrame(height: Theme.Size.box80)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(localized: "Cache Hit Rate"))
        .accessibilityValue(data.total.hitRate.map { $0.formatted(.percent.precision(.fractionLength(0))) } ?? "—")
        .accessibilityIdentifier("usage-hit-rate")
    }
}

private extension View {
    /// The usage charts' axes: times below (days for the week), values on the right, hairline grids.
    func usageAxes(domain: ClosedRange<Date>, range: String, format: @escaping (Double) -> String) -> some View {
        chartXScale(domain: domain)
            .chartXAxis {
                AxisMarks(values: .automatic(desiredCount: 4)) { _ in
                    AxisGridLine(stroke: StrokeStyle(lineWidth: 0.5)).foregroundStyle(MetricColor.grid)
                    if range == UsageRange.week.rawValue {
                        AxisValueLabel(format: .dateTime.weekday(.abbreviated), collisionResolution: .greedy)
                            .font(Theme.Font.label).foregroundStyle(Color(.secondaryLabel))
                    } else {
                        AxisValueLabel(format: .dateTime.hour().minute(), collisionResolution: .greedy)
                            .font(Theme.Font.label).foregroundStyle(Color(.secondaryLabel))
                    }
                }
            }
            .chartYAxis {
                AxisMarks(position: .trailing, values: .automatic(desiredCount: 3)) { v in
                    AxisGridLine(stroke: StrokeStyle(lineWidth: 0.5)).foregroundStyle(MetricColor.grid)
                    AxisValueLabel { if let d = v.as(Double.self) { Text(format(d)) } }
                        .font(Theme.Font.label).foregroundStyle(Color(.secondaryLabel))
                }
            }
            .chartPlotStyle { $0.clipped() }
    }
}
