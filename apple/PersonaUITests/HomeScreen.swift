import XCTest

/// The home screen, for the tests that go there or whose launches change the app's icon.
enum HomeScreen {
    /// Taps away the system's alert that the app's icon changed (「你已更改“Persona”的图标」), if it is up; true when it
    /// was. The icon follows Light & Dark (`AppIconModel.follow`): a launch with `-ui.scheme` and the next one without
    /// it each switch it, and the alert then covers the app or the home screen (#9004).
    @discardableResult @MainActor static func dismissIconNote() -> Bool {
        let board = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        guard board.alerts.firstMatch.waitForExistence(timeout: 2) else { return false }
        for label in ["OK", "好"] where board.alerts.buttons[label].exists { // i18n-ignore: the system's own button
            board.alerts.buttons[label].tap()
            sleep(1)
            return true
        }
        return false
    }
}
