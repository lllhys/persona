import SwiftUI

extension View {
    /// A button's label as a pill floating over a page, drawn as the Sidebar layout's foot draws its own (#4206): the
    /// prominent one in the Theme Color with white words, as the foot's New Chat; the other on the style's floating
    /// surface (Liquid Glass in the glass styles) with words in the primary color, as the foot's Search and Settings
    /// capsule. At least `Theme.Size.avatarL` tall (46 pt, the foot's), growing with the text, and as wide as it is
    /// offered. The button takes `.buttonStyle(.plain)`, as the foot's do. Over a scrolling page, in a
    /// `floatingFooter`.
    func floatingPill(prominent: Bool) -> some View {
        modifier(FloatingPill(prominent: prominent))
    }

    /// Actions floating at a scrolling page's foot (#9008): `bar` in a `safeAreaInset`, so the page scrolls on behind it
    /// and its last row clears it, with no fill of its own. Under it is only the page, blurred as it goes under and
    /// washed halfway back to `page`, the color behind the page's content (`Theme.groupedPage` for a grouped list): a
    /// material that comes in along `EasedFade`'s curve from a little above the bar, so no band and no line. The material
    /// alone lit a black page's foot gray in dark.
    /// Not `safeAreaBar`, whose system blur this follows: with it, a list beside the iPad's sidebar lost 12 pt of its
    /// leading margin (its cards 8 pt from the sidebar and 20 from the other side), whatever the bar held; and on the
    /// to-do page its automatic edge was the hard one, a lighter band with a line along its top.
    func floatingFooter<Bar: View>(over page: Color, @ViewBuilder _ bar: () -> Bar) -> some View {
        safeAreaInset(edge: .bottom) {
            bar().background { FloatingFooterBlur(page: page) }
        }
    }
}

private struct FloatingPill: ViewModifier {
    let prominent: Bool

    func body(content: Content) -> some View {
        let label = content
            .font(Theme.Font.bodyStrong)
            .foregroundStyle(prominent ? Color.white : Color.primary)
            .padding(.horizontal, Theme.Space.m)
            .scaledFrame(maxWidth: .infinity, minHeight: Theme.Size.avatarL)
            .contentShape(.capsule)
        if prominent {
            label.background(Theme.accent, in: .capsule)
        } else {
            label.surface(interactive: true, in: .capsule)
        }
    }
}

/// The blur under a `floatingFooter`: whole from the bar's lower part down through the bottom safe area, fading out
/// above it to nothing a little over the bar's top. One layer of a fixed size at the page's edge: scrolling never draws
/// it again.
private struct FloatingFooterBlur: View {
    let page: Color

    var body: some View {
        ZStack {
            Rectangle().fill(.ultraThinMaterial)
            page.opacity(0.5)
        }
        .mask {
            LinearGradient(stops: EasedFade.stops(.black, from: 0.4, over: 0.6), startPoint: .bottom, endPoint: .top)
        }
        .padding(.top, -Theme.Space.l)
        .ignoresSafeArea(edges: .bottom)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}
