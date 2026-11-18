import XCTest

/// The style a UI test runs in. The app keeps the style the last run left (`ui.style` in UserDefaults), and since the
/// BeautifulUI style draws its own components, a test that does not say which style it wants finds whichever looks
/// the previous test left. `launchPinned()` passes it: Liquid Glass by default, another one with
/// `TEST_RUNNER_PERSONA_UI_STYLE=<style>` (`beautiful` runs a suite in the BeautifulUI style), unless the test passes
/// `-ui.style` itself. `UITestStyleTests` checks every launch pins one.
enum UITestStyle {
    static var name: String { ProcessInfo.processInfo.environment["PERSONA_UI_STYLE"] ?? "glass" }
    static var isBeautiful: Bool { name == "beautiful" }

    /// The launch arguments with the run's style, unless they name one already: the later `-ui.style` wins, so adding
    /// it anyway ran a test that asked for Clay or BeautifulUI in Liquid Glass (#5683, `UITestStylePinTests`).
    static func pinned(_ arguments: [String]) -> [String] {
        arguments.contains("-ui.style") ? arguments : arguments + ["-ui.style", name]
    }

    /// The screenshot banner (#9508) pinned as well: a system screenshot (XCUIScreen, attachments) of a chat would show
    /// it instead of the top bar, so it is off unless the test names it, or the run asks for it
    /// (`TEST_RUNNER_PERSONA_SCREENSHOT_BANNER=1`: the bars' own tests with the bars in the capture layer, as a fresh
    /// install has them).
    static func bannerPinned(_ arguments: [String]) -> [String] {
        if arguments.contains("-ui.screenshotBanner") { return arguments }
        let on = ProcessInfo.processInfo.environment["PERSONA_SCREENSHOT_BANNER"] == "1"
        return arguments + ["-ui.screenshotBanner", on ? "YES" : "NO"]
    }
}

extension XCUIApplication {
    /// Launches in the run's style (or the one the test passes) without overriding the app's navigation defaults, and
    /// in the Separate layout unless the test names one (`-app.navigation`): with nothing picked an iPad starts in the
    /// Merged sidebar (#4206), and the tests written for the tabs and the shell would find another app. `navigation:
    /// false` leaves the layout to the app (a test of the setting itself, which a launch argument would pin).
    func launchPinned(navigation: Bool = true) {
        launchArguments = UITestStyle.bannerPinned(UITestStyle.pinned(launchArguments))
        launchPersona(navigation: navigation)
    }

    /// Demo launches start clean so a pairing left by an earlier test cannot override Demo Mode. The layout is pinned as
    /// `launchPinned`'s, for the tests that name their own style.
    func launchPersona(navigation: Bool = true) {
        if navigation, !launchArguments.contains("-app.navigation") { launchArguments += ["-app.navigation", "separate"] }
        if Self.wantsDemo(launchArguments) { launchArguments += ["-openWelcome", "YES"] }
        launch()
    }

    /// SwiftUI can inherit the drawer container's identifier onto its footer buttons on compact screens.
    @MainActor var drawerSettingsButton: XCUIElement {
        let identified = buttons["sidebar-settings"].firstMatch
        if identified.exists { return identified }
        return buttons.matching(NSPredicate(format: "identifier == %@ AND label == %@", "assistant-sidebar", "设置")).firstMatch
    }

    @MainActor var drawerNewChatButton: XCUIElement {
        let identified = buttons["sidebar-new"].firstMatch
        if identified.exists { return identified }
        return buttons.matching(NSPredicate(format: "identifier == %@ AND label == %@", "assistant-sidebar", "新对话")).firstMatch
    }

    /// Launched into Demo Mode and not paired by the test itself.
    static func wantsDemo(_ args: [String]) -> Bool {
        let demo = zip(args, args.dropFirst()).contains { $0 == "-demo.on" && $1 == "YES" }
        return demo && !args.contains("-pairURL") && !args.contains("-openWelcome")
    }
}
