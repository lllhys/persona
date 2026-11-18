#if os(iOS)
import OSLog
import SwiftUI
import UIKit

/// The list column of a split, driven from UIKit (#4206; iPad and the open iPhone Duo).
///
/// - **Gestures and the drag only.** Whether the column lies beside the page or over it is SwiftUI's, by the page's least
///   width (`SplitPageMin`), never set here: SwiftUI sets the split's behavior and the page's least width back on every
///   update, and a value set here and taken back each time laid the split out again and again until the scene ran out of
///   its 10 seconds (0x8BADF00D, #4206).
/// - **Slid by a finger, as Feishu's** (`drags`), from the window's leading edge while the column is away, from the line
///   beside it while it shows. Drawn from pictures of the screen (`Drag`), the top and side bars in them: as the finger
///   lands, the column's (the window's leading edge to the line) and the page's (the line to the trailing edge; the whole
///   window where the column lies over the page, which stays where it is), each stretched or squeezed between the line
///   and its edge as the line follows the finger, both blurred more as it goes. Let go, it settles out or away by how far
///   it went and how fast, the line springs there, and the pictures fade. A guide line in the theme color marks the line
///   while it moves (as Feishu's). The split itself changes at most once, under
///   the pictures: coming out, as the finger lands (the column's picture is then of the column as it is); going away, as
///   the finger lets go (where the column lies over the page, as it lands, for the page's picture without it). Nothing is
///   laid out while the finger moves, and the split's behavior is never set: laid over the page for an earlier drag, the
///   Agents tab's split (Separate sidebars) took its column away and the column slid on as an empty space.
/// - **Gestures.** A page that can go back keeps the edge for its own swipe back (`SwipeBack`): the column comes from its
///   button there. The line's grab area is a tap target wide and away from the rows, whose swipes (close, delete) stay
///   theirs; a drag starts only sideways. Regular width has no other sideways gesture (the shell's pager is the phone's).
struct SplitColumnControl: UIViewControllerRepresentable {
    /// The finger may slide the column.
    var drags: Bool
    /// The column as UIKit has it, for the split's SwiftUI side: a finger moving it, the split itself.
    var state: SplitColumnState
    /// Where a drag left the column, shown or not, for the split's visibility (it is there already: no animation).
    var settle: (Bool) -> Void

    func makeUIViewController(context: Context) -> Control { Control() }

    func updateUIViewController(_ control: Control, context: Context) {
        control.drags = drags
        control.state = state
        control.settle = settle
        control.apply()
    }

    final class Control: UIViewController, UIGestureRecognizerDelegate {
        var drags = false
        var state = SplitColumnState()
        var settle: (Bool) -> Void = { _ in }
        private weak var split: UISplitViewController?
        private var open: UIPanGestureRecognizer?
        private var close: UIPanGestureRecognizer?
        private var drag: Drag?

        /// A drag under way: where the line between the column and the page is, over pictures of the screen.
        private struct Drag {
            let opening: Bool
            /// The column settles beside the page; else over it, the page staying where it is.
            let beside: Bool
            /// The column's width: the line's place while it shows.
            let width: CGFloat
            /// Over the window, the pictures and the blur over them.
            let overlay: UIView
            let column: Picture
            let page: Picture
            /// Over the page where the column lies over it: the system's dimming, as far as the column is out.
            let dim: UIView?
            let blur: UIVisualEffectView
            /// On the line, over the blur, in the theme color.
            let guide: UIView
            let animator: UIViewPropertyAnimator
            var line: CGFloat
        }

        /// A part of a picture of the screen (`from`, window points), drawn into a strip of the window.
        private struct Picture {
            let strip: UIView
            var image: UIView
            let from: ClosedRange<CGFloat>
        }

        override func loadView() {
            view = UIView()
            view.isUserInteractionEnabled = false
        }

        override func didMove(toParent parent: UIViewController?) {
            super.didMove(toParent: parent)
            if parent == nil { uninstall() } else { apply() }
        }

        override func viewWillLayoutSubviews() {
            super.viewWillLayoutSubviews()
            apply()
        }

        func apply() {
            guard let split = splitViewController else { return }
            install(on: split)
            state.split = split
            // The split's own edge swipe laid the column over the page and left its state unkept: ours replaces it.
            if split.presentsWithGesture == drags { split.presentsWithGesture = !drags }
        }

        // MARK: Gestures

        private func install(on split: UISplitViewController) {
            guard self.split !== split else { return }
            uninstall()
            self.split = split
            let open = UIPanGestureRecognizer(target: self, action: #selector(moved(_:)))
            open.maximumNumberOfTouches = 1
            open.delegate = self
            let close = UIPanGestureRecognizer(target: self, action: #selector(moved(_:)))
            close.maximumNumberOfTouches = 1
            close.delegate = self
            split.view.addGestureRecognizer(open)
            split.view.addGestureRecognizer(close)
            self.open = open
            self.close = close
        }

        private func uninstall() {
            if let open { open.view?.removeGestureRecognizer(open) }
            if let close { close.view?.removeGestureRecognizer(close) }
            open = nil
            close = nil
            split = nil
        }

        private func shown(_ split: UISplitViewController) -> Bool { split.displayMode != .secondaryOnly }

        func gestureRecognizerShouldBegin(_ recognizer: UIGestureRecognizer) -> Bool {
            guard drags, drag == nil, let split, split.view.window != nil, let pan = recognizer as? UIPanGestureRecognizer else { return false }
            if recognizer === open {
                // A page that can go back keeps its edge (SwipeBack); the column comes from its button there.
                // From the window's leading edge, a finger's width in, to the right.
                let v = pan.velocity(in: split.view)
                let x = pan.location(in: split.view).x - pan.translation(in: split.view).x
                return !shown(split) && x <= Theme.Size.tapTarget / 2 && v.x > 0 && abs(v.x) > abs(v.y) * 1.5 && !canGoBack(split)
            }
            guard recognizer === close, shown(split) else { return false }
            let v = pan.velocity(in: split.view)
            // The line beside the column: a tap target wide, half over the column and half over the page.
            let line = split.primaryColumnWidth
            let x = pan.location(in: split.view).x - pan.translation(in: split.view).x
            // To the left only: on the line, a drag to the right is the page's swipe back (its edge is the line).
            guard v.x < 0, abs(v.x) > abs(v.y) * 1.5 else { return false }
            return abs(x - line) <= Theme.Size.tapTarget / 2
        }

        /// The list's scroll, its rows' swipes and the page's own drags wait for ours to fail. The line's drag fails at the
        /// first move anywhere but on the line, or moving up and down; the edge's anywhere but at the screen's edge
        /// (`gestureRecognizerShouldBegin`). Without this the list's pan, or the page's, took the touch first.
        func gestureRecognizer(_ recognizer: UIGestureRecognizer, shouldBeRequiredToFailBy other: UIGestureRecognizer) -> Bool {
            guard let split, recognizer === close || recognizer === open, other !== open, other !== close,
                  other is UIPanGestureRecognizer, let view = other.view else { return false }
            // The page's own swipe back is not made to wait: where it can go back, ours does not begin at all.
            if recognizer === open, other is UIScreenEdgePanGestureRecognizer { return false }
            return view.isDescendant(of: split.view)
        }

        /// A page pushed in the detail column: its stack's swipe back owns the edge.
        private func canGoBack(_ split: UISplitViewController) -> Bool {
            guard let page = split.viewController(for: .secondary) else { return false }
            var queue: [UIViewController] = [page]
            while let next = queue.popLast() {
                if let nav = next as? UINavigationController, nav.viewControllers.count > 1, nav.view.window != nil { return true }
                queue.append(contentsOf: next.children)
            }
            return false
        }

        @objc private func moved(_ pan: UIPanGestureRecognizer) {
            guard let split else { return }
            switch pan.state {
            case .began: begin(split, opening: pan === open)
            case .changed: follow(pan.translation(in: split.view).x)
            case .ended: end(split, velocity: pan.velocity(in: split.view).x)
            case .cancelled, .failed: end(split, velocity: 0)
            default: break
            }
        }

        // MARK: The drag

        private func begin(_ split: UISplitViewController, opening: Bool) {
            guard let window = split.view.window, let root = window.rootViewController?.view else { return }
            // The pictures leave the capture layer out as a screenshot does: the bars in them, not the screenshot banner
            // (#9508), until the pictures are gone.
            ScreenshotBanner.beginReveal()
            // A picture of the frame already drawn still leaves them out: with bars in the layer, one after it updates.
            let fresh = ScreenshotBanner.hasLayers
            guard let screen = root.snapshotView(afterScreenUpdates: fresh),
                  let screenAgain = root.snapshotView(afterScreenUpdates: false) else {
                ScreenshotBanner.endReveal()
                return
            }
            let width = split.primaryColumnWidth
            let full = 0...window.bounds.width
            // Beside the page where it keeps its least width (`SplitPageMin`); elsewhere the column lies over the page.
            let beside = state.fitsBeside ?? Self.beside(split)
            state.dragging = true
            let overlay = UIView(frame: window.bounds)
            overlay.isUserInteractionEnabled = false
            overlay.clipsToBounds = true
            window.addSubview(overlay)
            // The screen as it is, first, over whatever changes under it.
            var page = Self.picture(screen, from: opening || !beside ? full : width...full.upperBound, in: overlay)
            var dim: UIView?
            if !beside {
                let shade = UIView(frame: overlay.bounds)
                shade.backgroundColor = .black
                shade.alpha = 0
                shade.isUserInteractionEnabled = false
                overlay.addSubview(shade)
                dim = shade
            }
            var column = Self.picture(screenAgain, from: 0...width, in: overlay)
            if opening {
                // Out under the pictures now: the column's picture is then of the column as it is.
                UIView.performWithoutAnimation {
                    split.show(.primary)
                    split.view.layoutIfNeeded()
                }
                settle(true)
                if let now = root.snapshotView(afterScreenUpdates: true) { Self.swap(&column, to: now) }
            } else if !beside {
                // Over the page: the page's picture without the column over it.
                UIView.performWithoutAnimation {
                    split.hide(.primary)
                    split.view.layoutIfNeeded()
                }
                if let now = root.snapshotView(afterScreenUpdates: true) { Self.swap(&page, to: now) }
            }
            let blur = UIVisualEffectView(effect: nil)
            blur.frame = overlay.bounds
            blur.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            overlay.addSubview(blur)
            let animator = UIViewPropertyAnimator(duration: 1, curve: .linear) {
                blur.effect = UIBlurEffect(style: .systemUltraThinMaterial)
            }
            animator.pausesOnCompletion = true
            animator.fractionComplete = 0
            let guide = UIView()
            guide.backgroundColor = UIColor(Theme.accent)
            guide.isUserInteractionEnabled = false
            overlay.addSubview(guide)
            let d = Drag(opening: opening, beside: beside, width: width, overlay: overlay, column: column, page: page, dim: dim,
                         blur: blur, guide: guide, animator: animator, line: opening ? 0 : width)
            place(d)
            drag = d
        }

        private func follow(_ dx: CGFloat) {
            guard var d = drag else { return }
            d.line = min(d.width, max(0, (d.opening ? 0 : d.width) + dx))
            // The pictures blur as the finger moves, from nothing as it lands.
            d.animator.fractionComplete = min(1, abs(dx) / Theme.Split.blurReach) * Theme.Split.dragBlur
            place(d)
            drag = d
        }

        private func end(_ split: UISplitViewController, velocity: CGFloat) {
            guard let d = drag else { return }
            // A fling decides; else past halfway.
            let shows = abs(velocity) > Theme.Split.fling ? velocity > 0 : d.line > d.width / 2
            // The split as the column settles, under the pictures, once.
            if shows != (split.displayMode != .secondaryOnly) {
                UIView.performWithoutAnimation {
                    if shows { split.show(.primary) } else { split.hide(.primary) }
                    split.view.layoutIfNeeded()
                }
            }
            state.dragging = false
            settle(shows)
            var settled = d
            settled.line = shows ? d.width : 0
            let distance = settled.line - d.line
            let fade = {
                UIView.animate(withDuration: Theme.Split.fade) {
                    d.overlay.alpha = 0
                } completion: { _ in
                    d.animator.stopAnimation(true)
                    d.overlay.removeFromSuperview()
                    self.drag = nil
                    ScreenshotBanner.endReveal()
                }
            }
            // Reduce Motion: the line where it settles at once, no spring; the pictures only fade.
            guard !UIAccessibility.isReduceMotionEnabled else {
                place(settled)
                fade()
                return
            }
            UIView.animate(springDuration: Theme.Split.settle, bounce: 0, initialSpringVelocity: distance == 0 ? 0 : velocity / distance) {
                self.place(settled)
            } completion: { _ in fade() }
        }

        /// The pictures where the line is: the column's from the window's leading edge to the line; the page's from the
        /// line to the trailing edge beside the column, the whole window under it.
        private func place(_ d: Drag) {
            let width = d.overlay.bounds.width
            Self.draw(d.page, into: d.beside ? d.line...max(d.line, width) : 0...width)
            d.dim?.alpha = d.width > 0 ? Theme.Split.overDim * d.line / d.width : 0
            Self.draw(d.column, into: 0...d.line)
            let guide = Theme.Split.guide
            d.guide.frame = CGRect(x: min(max(d.line - guide / 2, 0), width - guide), y: 0, width: guide, height: d.overlay.bounds.height)
        }

        /// A strip of `overlay` showing `from` of the screen's picture, below the blur and the strips made before it.
        private static func picture(_ image: UIView, from: ClosedRange<CGFloat>, in overlay: UIView) -> Picture {
            let strip = UIView(frame: overlay.bounds)
            strip.clipsToBounds = true
            strip.isUserInteractionEnabled = false
            strip.addSubview(image)
            overlay.addSubview(strip)
            return Picture(strip: strip, image: image, from: from)
        }

        /// The same part of a newer picture of the screen.
        private static func swap(_ p: inout Picture, to image: UIView) {
            image.transform = p.image.transform
            image.bounds = p.image.bounds
            image.center = p.image.center
            p.strip.addSubview(image)
            p.image.removeFromSuperview()
            p.image = image
        }

        /// Draws `p`'s part of the screen into `into` (window points), stretched or squeezed across.
        private static func draw(_ p: Picture, into: ClosedRange<CGFloat>) {
            let height = p.strip.superview?.bounds.height ?? 0
            let span = into.upperBound - into.lowerBound
            p.strip.frame = CGRect(x: into.lowerBound, y: 0, width: span, height: height)
            let source = p.from.upperBound - p.from.lowerBound
            let scale = source > 0 ? max(span / source, 0.001) : 1
            p.image.transform = CGAffineTransform(scaleX: scale, y: 1)
            p.image.center = CGPoint(x: (p.image.bounds.width / 2 - p.from.lowerBound) * scale, y: height / 2)
        }

        /// The column beside the page, not over it.
        private static func beside(_ split: UISplitViewController) -> Bool {
            split.displayMode == .oneBesideSecondary || split.displayMode == .twoBesideSecondary
        }
    }
}

/// The list column's own Hide Sidebar, where the column is slid by a finger (`SplitColumnControl`): SwiftUI leaves out
/// the system's button with the split's own edge swipe, and the Separate layout's Agents list had no other way to fold
/// (#4206). Shown with it gone, the page's Show List and the chat's ‹ bring the list back. Nothing outside a split.
struct SplitHideListButton: View {
    @Environment(\.splitShowList) private var showList

    var body: some View {
        if let hide = showList?.hide {
            Button(action: hide) { Label("Hide Sidebar", systemImage: Theme.Icon.sidebar) }
                .accessibilityIdentifier("split-hide-list")
        }
    }
}

/// A split's list column as UIKit has it (`SplitColumnControl`), read where it is needed, not observed: a drag redraws
/// nothing. `fitsBeside` reads the split's own size, as a turn lays it out: SwiftUI's geometry for the new size can come
/// after the split's own change of columns, and a list the turn put away was kept as closed (#4206).
final class SplitColumnState {
    /// A finger is moving the column: the split lays it over the page meanwhile, which is not its posture.
    var dragging = false
    weak var split: UISplitViewController?

    /// The page beside the column is at least its least width at the split's size now (`SplitColumns.fitsBeside`); nil
    /// before the split is laid out.
    @MainActor var fitsBeside: Bool? {
        guard let width = split?.view.bounds.width, width > 0 else { return nil }
        return SplitColumns.fitsBeside(width: width)
    }
}
#endif
