import SwiftUI

/// Settings › Appearance › Sidebar (#6332, #4206): how the iPhone and iPad lay out Persona and the Agents.
///
/// - `sidebar`, Merged: everything in one sidebar, as the web app has it: the main conversation, the pages, Persona's
///   chats and the Agents list. What is picked in it is the page beside it (docs/apple/ios.md「Sidebar layout」).
/// - `separate`: Assistant and Agents as two pages, the Assistant | Agents switch at the top on an iPhone, the
///   Persona | Agents tabs on an iPad, as before the choice existed; on iPhone Duo, on both displays, a tab bar of the
///   pages' symbols over their names, down the system's side column, or along the foot of the inner display upright
///   (#4231, `TabSwitchLayout`).
/// - Nothing picked (`effective`): Merged where the sidebar sits beside the page (regular width: the iPad, the open
///   iPhone Duo), Separate at compact width (an iPhone, the folded Duo), as #4206 designed it; picked, either one at
///   every width (the iPhone's drawer is Merged at compact width, #6332).
///
/// This device's own choice (not the hub's), read where the app's root is chosen (`RootView`); a change takes effect at
/// once. The Mac (frozen) and Apple Vision Pro keep their own navigation and never show the choice.
enum NavigationLayout: String, CaseIterable, Identifiable {
    case sidebar, separate

    /// The UserDefaults key; `-app.navigation sidebar` at launch picks the sidebar, `-app.navigation ""` nothing (UI
    /// tests, screenshots).
    static let key = "app.navigation"

    /// The layout in effect: the one picked (`key`'s raw value), else Merged at regular width and Separate at compact
    /// width (#4206: the owner's delegate picked "Merged where wide", 2026-11-14).
    static func effective(picked: String, regular: Bool) -> NavigationLayout {
        NavigationLayout(rawValue: picked) ?? (regular ? .sidebar : .separate)
    }

    var id: String { rawValue }

    /// The choice's name, as the Settings row's value shows it.
    var title: String {
        switch self {
        case .separate: String(localized: "Separate [Navigation Layout]")
        case .sidebar: String(localized: "Merged [Navigation Layout]")
        }
    }

    /// One sentence under the choice on the Navigation page.
    var summary: String {
        switch self {
        case .separate:
            String(localized: "Assistant and Agents each have a page. On iPhone you switch at the top, on iPhone Duo with a tab bar down the side or along the bottom, and on iPad with the Persona and Agents tabs.")
        case .sidebar:
            String(localized: "The main chat, pages, Persona chats and Agents are all in one sidebar, as in the web app. On iPhone it slides in from the left; on iPad it stays on the left.")
        }
    }

    #if os(iOS)
    /// The layout in effect in this app's window now, for Settings (read when it draws, not observed): its width as the
    /// app's root has it, not the Settings sheet's, which can be compact on an iPad.
    @MainActor static func effectiveNow(picked: String) -> NavigationLayout {
        let regular = UIApplication.shared.connectedScenes.contains { scene in
            (scene as? UIWindowScene)?.traitCollection.horizontalSizeClass == .regular
        }
        return effective(picked: picked, regular: regular)
    }
    #endif

    /// Where the Sidebar layout is built (#6332): the iPhone (the drawer) and the iPad (the column). The Navigation row
    /// shows only where it does something.
    static var offered: Bool {
        #if os(iOS)
        true
        #else
        false
        #endif
    }
}
