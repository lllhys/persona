#if DEBUG && os(iOS)
import SwiftUI
import UIKit

/// The Sidebar layout's page as UIKit moves it (#8359), for UI tests (`probe.sidebar-page`, with `-probes YES`): how many
/// animated navigation transitions (a push or a pop) have run, how deep the deepest stack is, and the root the sidebar
/// picked. A pick that replaces the root runs no transition; a drill-down pushes one.
@MainActor @Observable final class NavMotionWatch: NSObject {
    static let shared = NavMotionWatch()
    private(set) var transitions = 0
    private(set) var depth = 0
    @ObservationIgnored private var link: CADisplayLink?
    @ObservationIgnored private var moving = false

    func start() {
        guard link == nil else { return }
        let l = CADisplayLink(target: self, selector: #selector(tick))
        l.add(to: .main, forMode: .common)
        link = l
    }

    /// Each frame: a navigation controller with an animated transition under way counts once, when it starts.
    @objc private func tick() {
        var animating = false
        var deepest = 0
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            for window in scene.windows where !window.isHidden {
                Self.visit(window.rootViewController) { nav in
                    deepest = max(deepest, nav.viewControllers.count)
                    if let c = nav.transitionCoordinator, c.isAnimated { animating = true }
                }
            }
        }
        if animating, !moving { transitions += 1 }
        moving = animating
        if depth != deepest { depth = deepest }
    }

    private static func visit(_ vc: UIViewController?, _ found: (UINavigationController) -> Void) {
        guard let vc else { return }
        if let nav = vc as? UINavigationController { found(nav) }
        for child in vc.children { visit(child, found) }
        if let presented = vc.presentedViewController, presented.presentingViewController === vc { visit(presented, found) }
    }
}

/// `probe.sidebar-page`'s value.
nonisolated struct SidebarPageShown: Encodable, Equatable {
    var root = ""
    var path = 0
    var transitions = 0
    var depth = 0
}

/// Writes `probe.sidebar-page` beside the Sidebar layout's page (`ChatsView`, roots mode); the Separate layout's Agents
/// tab writes `probe.sidebar-page-agents` (#8603), so the two tabs' pages are told apart.
struct SidebarPageProbe: View {
    let root: SidebarPageRoot
    let path: Int
    var name = "sidebar-page"

    var body: some View {
        let watch = NavMotionWatch.shared
        Color.clear
            .frame(width: Theme.Size.line, height: Theme.Size.line)
            .allowsHitTesting(false)
            .screenProbe(name, SidebarPageShown(root: root.probeName, path: path, transitions: watch.transitions, depth: watch.depth))
            .onAppear { watch.start() }
    }
}

extension SidebarPageRoot {
    /// The root by name: `conversation`, a place's (`inbox`, `recordings`…), `health`, `messages`, `agent`, or the type
    /// of what the Agents list opened.
    var probeName: String {
        guard let value else { return "conversation" }
        switch value.base {
        case let route as ShellRoute: return "\(route)"
        case let route as SidebarRoute: return "\(route)"
        case is SessionKey: return "agent"
        default: return String(describing: type(of: value.base))
        }
    }
}
#endif
