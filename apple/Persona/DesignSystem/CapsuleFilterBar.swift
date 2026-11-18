import SwiftUI

#if os(iOS)
/// One choice of the strip: what its chip draws.
struct CapsuleFilterItemData: Identifiable {
    var id: String
    var title: String
    var count: Int?
    var tone: StatusTone?
    var symbol: String?
    /// The count drawn as a `CountBadge` of this look (zero hides it, 99+ caps it, and the spoken name says "Title, n"),
    /// as the Inbox's section bar draws it (#9053); nil keeps the plain number the Agents filters show.
    var countLook: CountBadge.Look? = nil
}

/// One list-control surface: choices scroll, while its trailing menu stays in reach. Every choice shows its whole name,
/// at every text size and in every language (#9121); a strip that does not fit scrolls, and an edge with more past it
/// fades (`.edgeFade()`).
struct CapsuleFilterBar<Trailing: View>: View {
    var items: [CapsuleFilterItemData]
    var selection: String
    var choose: (String) -> Void
    /// The strip's identifier and its choices' prefix (`<prefix><id>`): the Agents list's by default.
    var scrollID = "agents.filters.scroll"
    var itemIDPrefix = "agents.filter."
    /// The strip scrolls the chosen item into sight whenever the choice changes, by a tap or by the page (a search
    /// showing All, a landing), and when it appears; by the least it can, so the first items stay in sight as far as
    /// they fit, and clear of the edge's fade (the Inbox's section bar, #9053). Off: a tap centres its item, as the
    /// Agents filters do.
    var revealsSelection = false
    /// The hairline and the trailing menu; off while the menu has nothing to offer (the Inbox's grouping under In
    /// Progress and Done, #9053).
    var showsTrailing = true
    @ViewBuilder var trailing: Trailing
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Revealing: where the strip is scrolled to, and what it measured (not observed: scrolling redraws nothing).
    @State private var position = ScrollPosition()
    @State private var geometry = CapsuleStripGeometry()

    var body: some View {
        HStack(spacing: Theme.Space.flush) {
            ScrollViewReader { proxy in
                ScrollView(.horizontal) {
                    HStack(spacing: Theme.Space.xs) {
                        ForEach(items) { item in
                            CapsuleFilterItem(item: item, selected: item.id == selection, idPrefix: itemIDPrefix) {
                                choose(item.id)
                                // revealing, the choice's change scrolls (below), from the layout it makes
                                guard !revealsSelection else { return }
                                withAnimation(reduceMotion ? nil : Theme.Motion.standard) {
                                    proxy.scrollTo(item.id, anchor: .center)
                                }
                            }
                            .id(item.id)
                            .modifier(CapsuleStripFrame(on: revealsSelection, id: item.id, geometry: geometry))
                        }
                    }
                    .padding(.horizontal, Theme.Space.xs)
                    .coordinateSpace(.named(CapsuleStripGeometry.space))
                }
                // A horizontal strip must never inherit the navigation bar's vertical content inset.
                .contentMargins(.vertical, 0, for: .scrollContent)
                .scrollIndicators(.hidden)
                .scaledFrame(height: Theme.Size.tapTarget)
                .frame(minHeight: Theme.Size.tapTarget)
                .accessibilityIdentifier(scrollID)
                // The trailing edge fades while more lies past it, the leading edge once the strip has left its start.
                .edgeFade()
                .modifier(CapsuleStripScroll(on: revealsSelection, position: $position, geometry: geometry))
                .onAppear { if revealsSelection { reveal(selection, animated: false) } }
                .onChange(of: selection) { _, chosen in
                    if revealsSelection { reveal(chosen, animated: true) }
                }
            }
            if showsTrailing {
                Rectangle().fill(Theme.cardStroke)
                    .frame(width: Theme.Size.hairline)
                    .scaledFrame(height: Theme.FilterCapsule.separatorHeight)
                    .accessibilityHidden(true)
                trailing
            }
        }
        .background(Theme.card, in: Capsule())
        .overlay(Capsule().strokeBorder(Theme.cardStroke, lineWidth: Theme.Size.hairline))
        .clipShape(Capsule())
    }

    /// Scrolls `id` wholly into view, clear of the edges' fade, by the least it can. After this update is laid out: on
    /// the strip's first appearance, or a choice the page made as it appeared, nothing is measured yet.
    private func reveal(_ id: String, animated: Bool) {
        Task { @MainActor in
            for _ in 0..<5 where !geometry.measured(id) { try? await Task.sleep(for: .milliseconds(100)) }
            guard let x = geometry.offset(revealing: id) else { return }
            withAnimation(animated && !reduceMotion ? Theme.Motion.standard : nil) { position.scrollTo(x: x) }
        }
    }
}

/// What a revealing strip measured: its chips' frames in its content and the part of the content in view. Not
/// observable, so neither a scroll nor a chip's layout redraws the page around the strip (P7).
final class CapsuleStripGeometry {
    static let space = "capsuleStrip"
    var frames: [String: CGRect] = [:]
    var visible: CapsuleReveal.Visible?
    init() {}

    func measured(_ id: String) -> Bool { frames[id] != nil && visible != nil }

    func offset(revealing id: String) -> CGFloat? {
        guard let frame = frames[id], let visible else { return nil }
        return CapsuleReveal.offset(revealing: frame.minX...frame.maxX, in: visible, fade: Theme.Size.fadeEdge)
    }
}

/// Where a strip scrolls so a chip is wholly in view and clear of the edges' fade (`.edgeFade()`), by the least it can.
nonisolated enum CapsuleReveal {
    /// The content in view: its offset, the width shown, and how far the content scrolls.
    struct Visible: Equatable {
        var offset: CGFloat
        var width: CGFloat
        var minOffset: CGFloat
        var maxOffset: CGFloat
    }

    /// The offset that shows `chip` (its span in the content) clear of the fade at both edges, held to the content's
    /// ends (an end has no fade); nil when it already is.
    static func offset(revealing chip: ClosedRange<CGFloat>, in visible: Visible, fade: CGFloat) -> CGFloat? {
        guard visible.width > 0 else { return nil }
        // as `.edgeFade()` draws it: never more than a quarter of a narrow strip
        let margin = min(fade, visible.width / 4)
        var x = visible.offset
        if chip.upperBound + margin > x + visible.width { x = chip.upperBound + margin - visible.width }
        if chip.lowerBound - margin < x { x = chip.lowerBound - margin }
        x = min(max(x, visible.minOffset), max(visible.minOffset, visible.maxOffset))
        return abs(x - visible.offset) < 0.5 ? nil : x
    }
}

/// A revealing strip's chip records its frame in the strip's content; others do nothing.
private struct CapsuleStripFrame: ViewModifier {
    var on: Bool
    var id: String
    var geometry: CapsuleStripGeometry

    func body(content: Content) -> some View {
        if on {
            content.onGeometryChange(for: CGRect.self) { $0.frame(in: .named(CapsuleStripGeometry.space)) } action: {
                geometry.frames[id] = $0
            }
        } else {
            content
        }
    }
}

/// A revealing strip takes its scroll position from `position` and records what is in view; others are left as they are.
private struct CapsuleStripScroll: ViewModifier {
    var on: Bool
    @Binding var position: ScrollPosition
    var geometry: CapsuleStripGeometry

    func body(content: Content) -> some View {
        if on {
            content
                .scrollPosition($position)
                .onScrollGeometryChange(for: CapsuleReveal.Visible.self) { g in
                    CapsuleReveal.Visible(offset: g.contentOffset.x, width: g.containerSize.width,
                                          minOffset: -g.contentInsets.leading,
                                          maxOffset: g.contentSize.width + g.contentInsets.trailing - g.containerSize.width)
                } action: { _, visible in geometry.visible = visible }
        } else {
            content
        }
    }
}

/// One choice: its dot or mark, its whole name on one line and its count. Its width is the name's, whether it is chosen or not.
struct CapsuleFilterItem: View {
    var item: CapsuleFilterItemData
    var selected: Bool
    var idPrefix = "agents.filter."
    var action: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var ink: Color { selected ? Theme.accent : .primary }

    var body: some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.xs) {
                if let tone = item.tone {
                    if let symbol = item.symbol {
                        ThemedIcon(symbol).font(Theme.Font.label).foregroundStyle(tone.dot)
                    } else {
                        Circle().fill(tone.dot).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                    }
                }
                Text(item.title)
                    .lineLimit(1)
                    .fixedSize()
                if let count = item.count {
                    if let look = item.countLook {
                        CountBadge(count: count, look: look)
                    } else {
                        Text("\(count)").monospacedDigit()
                            .foregroundStyle(selected ? ink : .secondary)
                            .contentTransition(.numericText(value: Double(count)))
                            .fixedSize()
                    }
                }
            }
            .font(Theme.Font.secondaryMedium)
            .foregroundStyle(ink)
            .padding(.horizontal, Theme.Space.s)
            .padding(.vertical, Theme.Space.xs)
            .background(selected ? Theme.FilterCapsule.selection : .clear, in: Capsule())
            .padding(.vertical, Theme.Space.xs)
            .scaledFrame(minWidth: Theme.Size.tapTarget, minHeight: Theme.Size.tapTarget)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .animation(reduceMotion ? nil : Theme.Motion.standard, value: selected)
        .accessibilityIdentifier(idPrefix + item.id)
        .accessibilityLabel(spokenName)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    /// The title with its count: a badge's only while it shows one, the exact number past 99 too.
    private var spokenName: String {
        guard let count = item.count else { return item.title }
        guard item.countLook != nil else { return "\(item.title) \(count)" }
        return count > 0 ? item.title + String(localized: ", ") + String(count) : item.title
    }
}
#endif
