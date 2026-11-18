import SwiftUI

/// A grid of squares in the theme color's heat steps (docs/design/DESIGN_SYSTEM.md 3.15, `Theme.Heat`): the token
/// calendar's days, a column a week, and its last week's hours, a row a day. One Canvas draws them; a tap picks the
/// square under the finger (tapping the picked one again lets it go). It keeps no state: its caller says what is
/// outlined and picked.
struct HeatGrid: View {
    /// A square: its column and row.
    nonisolated struct Cell: Hashable, Sendable {
        var column: Int
        var row: Int
    }

    /// The steps, column by column (`levels[column][row]`): 0 no usage, 1 to 4 the color's steps, below 0 no square (a
    /// day still to come).
    var levels: [[Int]]
    var rows: Int
    /// A square's size; its width is the column's.
    var cell: CGSize
    var gap: CGFloat
    /// Today's square: a thin outline.
    var outlined: Cell? = nil
    var picked: Cell? = nil
    /// The square a tap picked, nil when it let the picked one go; nil: the grid takes no taps.
    var pick: ((Cell?) -> Void)? = nil

    var body: some View {
        let width = CGFloat(levels.count) * (cell.width + gap) - gap
        let height = CGFloat(rows) * (cell.height + gap) - gap
        Canvas { ctx, _ in
            for (c, column) in levels.enumerated() {
                for (r, level) in column.prefix(rows).enumerated() where level >= 0 {
                    let rect = frame(Cell(column: c, row: r))
                    let shape = Path(roundedRect: rect, cornerRadius: Theme.Heat.radius)
                    ctx.fill(shape, with: .color(Theme.Heat.fill(level)))
                }
            }
            if let outlined, outlined != picked, exists(outlined) {
                let rect = frame(outlined).insetBy(dx: Theme.Heat.todayLine / 2, dy: Theme.Heat.todayLine / 2)
                ctx.stroke(Path(roundedRect: rect, cornerRadius: Theme.Heat.radius), with: .color(.primary), lineWidth: Theme.Heat.todayLine)
            }
            if let picked, exists(picked) {
                // Outside the square, so its color still shows whole.
                let rect = frame(picked).insetBy(dx: -Theme.Heat.pickedLine / 2, dy: -Theme.Heat.pickedLine / 2)
                ctx.stroke(Path(roundedRect: rect, cornerRadius: Theme.Heat.radius + Theme.Heat.pickedLine / 2), with: .color(.primary),
                           lineWidth: Theme.Heat.pickedLine)
            }
        }
        .frame(width: max(width, 0), height: max(height, 0))
        .contentShape(.rect)
        .gesture(SpatialTapGesture().onEnded { tap in
            guard let pick, let hit = cell(at: tap.location) else { return }
            Haptics.tap()
            pick(hit == picked ? nil : hit)
        }, including: pick == nil ? .none : .all)
    }

    private func frame(_ c: Cell) -> CGRect {
        CGRect(x: CGFloat(c.column) * (cell.width + gap), y: CGFloat(c.row) * (cell.height + gap), width: cell.width, height: cell.height)
    }

    private func exists(_ c: Cell) -> Bool {
        levels.indices.contains(c.column) && c.row < rows && levels[c.column].indices.contains(c.row) && levels[c.column][c.row] >= 0
    }

    /// The square nearest a point within the grid (a tap in a gap takes the square it is closest to).
    func cell(at p: CGPoint) -> Cell? {
        let c = Int(((p.x + gap / 2) / (cell.width + gap)).rounded(.down))
        let r = Int(((p.y + gap / 2) / (cell.height + gap)).rounded(.down))
        let hit = Cell(column: min(max(c, 0), levels.count - 1), row: min(max(r, 0), rows - 1))
        return exists(hit) ? hit : nil
    }
}

/// The steps' key under a heat grid: what the faintest and fullest mean, the five squares between.
struct HeatLegend: View {
    var low: String
    var high: String

    var body: some View {
        HStack(spacing: Theme.Space.xs) {
            Text(low)
            HStack(spacing: Theme.Heat.gap) {
                ForEach(0..<(Theme.Heat.steps.count + 1), id: \.self) { level in
                    RoundedRectangle(cornerRadius: Theme.Heat.radius)
                        .fill(Theme.Heat.fill(level))
                        .frame(width: Theme.Heat.cell, height: Theme.Heat.cell)
                }
            }
            Text(high)
        }
        .font(Theme.Font.label)
        .foregroundStyle(.secondary)
        .lineLimit(1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(localized: "From \(low) to \(high)", comment: "A heat grid's legend: what its faintest and fullest squares mean"))
    }
}
