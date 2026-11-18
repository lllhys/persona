import CoreGraphics

/// The 「+」 panel's (`AttachSheet`) sizes that depend on the screen: its tab bar's width and the grid's cell.
nonisolated enum AttachLayout {
    /// The tab bar's width for `tabs` tabs on a screen `width` wide.
    static func barWidth(tabs: Int, width: CGFloat) -> CGFloat {
        let widths = Theme.Attach.barWidths
        if tabs >= 2, tabs - 2 < widths.count { return min(widths[tabs - 2], width - Theme.Attach.barInset * 2) }
        return width - Theme.Attach.barInset * 2
    }

    /// The grid's cell side: `columns` across `width`, `Theme.Attach.gridSpacing` apart, rounded down to the pixel of a
    /// screen `scale` times the point. Every cell, the camera's too, is this square.
    static func cell(width: CGFloat, columns: Int = Theme.Attach.columns, scale: CGFloat = 3) -> CGFloat {
        let side = (width - Theme.Attach.gridSpacing * CGFloat(columns - 1)) / CGFloat(columns)
        return (side * scale).rounded(.down) / scale
    }
}
