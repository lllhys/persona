import XCTest

/// The action bar above the assistant's composer (#3393): Needs You and In Progress open the trays that list what the
/// dock's pills used to (tasks and agents of no task, each in one of them; the board behind each tray's All).
extension XCUIApplication {
    /// Opens the tray of `entry` (`needs-you`, `running`) unless it is open, and returns its entry.
    @MainActor @discardableResult
    func openActionTray(_ entry: String, timeout: TimeInterval = 20) -> XCUIElement {
        let button = buttons["action-bar.\(entry)"]
        XCTAssertTrue(button.waitForExistence(timeout: timeout), "no \(entry) in the action bar:\n\(debugDescription)")
        if !button.isSelected { button.tap() }
        _ = otherElements["action-tray.\(entry)"].waitForExistence(timeout: 5)
        return button
    }

    /// The task rows of the open tray.
    var trayTasks: XCUIElementQuery { buttons.matching(identifier: "action-tray.task") }
    /// The agent rows of the open tray.
    var trayAgents: XCUIElementQuery { buttons.matching(identifier: "action-tray.agent") }

    /// Waits until the open tray lists at least `count` rows of `rows`, up to `timeout` (the tasks, the sessions and
    /// their states come in separate pulls).
    @MainActor func waitForRows(_ rows: XCUIElementQuery, atLeast count: Int, timeout: TimeInterval = 20) {
        let deadline = Date.now.addingTimeInterval(timeout)
        while rows.count < count && Date.now < deadline { usleep(200_000) }
    }
}
