import SwiftUI

// MARK: - Long words folded to a few lines

/// Words drawn as Markdown, folded to `lines` lines of body text, with Expand / Collapse under them when there is more
/// (#2010: a long message from another session). `lineLimit` does not reach into Markdown's blocks, so the words are
/// cut at the height of that many body lines and fade out over the last one; words that fit are drawn as they are,
/// with no button. Open or not is kept by `id` (`OpenReports`, in the chat's scope), so a row the lazy list builds
/// again stays as it was left; `id` is also the key the row is pinned by (`chatPinned`), which keeps its top where it
/// is on screen while it opens or folds.
struct LineFold<Words: View>: View {
    var lines: Int
    var id: String
    @ViewBuilder var words: () -> Words
    /// The words' whole height, and the height of `lines` body lines.
    @State private var full: CGFloat = 0
    @State private var cut: CGFloat = 0
    @Environment(\.rowMemoScope) private var scope
    @Environment(\.chatPin) private var pin

    private var key: String { scope + "/" + id }
    /// Something to open or fold: the words run past their lines.
    private var folds: Bool { cut > 0 && full > cut + 1 }

    var body: some View {
        let open = OpenReports.shared.isOpen(key)
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            measured
                .frame(height: folds && !open ? cut : nil, alignment: .top)
                .clipped()
                .mask { if folds && !open { fade } else { Rectangle() } }
                .background(alignment: .top) { probe }
            if folds { FoldToggle(open: open, prefix: "message", action: { flip(open) }) }
        }
    }

    /// The words at their own height, which is read.
    private var measured: some View {
        words()
            .fixedSize(horizontal: false, vertical: true)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { full = $0 }
    }

    /// `lines` lines of body text, unseen: the height the words are cut at (a paragraph has the body's line height).
    private var probe: some View {
        Text(verbatim: Array(repeating: "X", count: lines).joined(separator: "\n"))
            .font(Theme.Font.body)
            .lineHeight(.body)
            .fixedSize(horizontal: false, vertical: true)
            .hidden()
            .accessibilityHidden(true)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { cut = $0 }
    }

    /// Opaque down to the last line shown, which fades out.
    private var fade: some View {
        VStack(spacing: 0) {
            Rectangle()
            LinearGradient(colors: [.black, .clear], startPoint: .top, endPoint: .bottom)
                .frame(height: cut / CGFloat(max(lines, 1)))
        }
    }

    private func flip(_ open: Bool) {
        Haptics.tap()
        toggleInPlace(pin, id, animation: Theme.Motion.state, closing: open) { OpenReports.shared.set(key, !open) }
    }
}
