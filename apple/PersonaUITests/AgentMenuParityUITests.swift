import XCTest

/// #9278: an Agent's row and its detail More menu show the same ordered actions.
/// Demo data is synthetic and never touches a real hub or a real Agent.
final class AgentMenuParityUITests: XCTestCase {
    private var app: XCUIApplication!
    private let title = "给天气 App 加主屏幕小组件" // i18n-ignore: bundled synthetic fixture

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch(_ extra: [String] = []) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-app.tab", "chats", "-tab", "dev",
                               "-dev.openChat", "", "-openAgentsFresh", "YES", "-dev.grouping", "space",
                               "-holdChromeOpen", "YES", "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"] + extra
        app.launchPinned()
        let anyRow = app.buttons.matching(identifier: "agents.row").firstMatch
        XCTAssertTrue(anyRow.waitForExistence(timeout: 20), "no Agent row in the list")
        if !row.exists {
            app.revealAgentsSearch()
            let search = app.searchFields.firstMatch
            XCTAssertTrue(search.waitForExistence(timeout: 5), "Agents search did not open")
            search.tap()
            search.typeText("天气") // i18n-ignore: bundled synthetic fixture title
        }
        XCTAssertTrue(row.waitForExistence(timeout: 10), "the example Agent is not in the list")
        app.bringAboveTabBar(row)
        sleep(1)
    }

    @MainActor private var row: XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == 'agents.row' AND label CONTAINS %@", title)).firstMatch
    }

    @MainActor private func openRowMenu() -> [String] {
        row.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["置顶"].waitForExistence(timeout: 5), "the row's long-press menu did not open")
        return menuLabels()
    }

    @MainActor private func openDetailMenu() -> [String] {
        row.tap()
        let more = app.buttons["chat.more"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 10), "the detail's More button did not appear")
        more.tap()
        XCTAssertTrue(app.buttons["置顶"].waitForExistence(timeout: 5), "detail has no Pin action")
        return menuLabels()
    }

    @MainActor private func menuLabels() -> [String] {
        let wanted: [String: String] = [
            "Git": "git", "文件": "files", "全部子 Agent（1）": "subagents", "空间里的所有 Agent": "space",
            "连接实时会话": "attach", "中断": "interrupt", "标为已看": "markSeen", "接管": "takeover",
            "在新会话中继续": "continueInNewSession", "重命名 Agent…": "rename", "置顶": "pin", "取消置顶": "pin",
            "屏蔽通知": "mute", "取消屏蔽通知": "mute", "关联到待办…": "linkTodo", "移动到其他待办…": "moveTodo",
            "取消关联": "unlinkTodo", "停止循环": "stopLoop", "交给 Persona": "handToPersona",
            "拷贝目录": "copyDirectory", "终端": "terminal", "拷贝恢复命令": "copyResume",
            "在终端里恢复": "resume", "拆分窗格": "splitPane", "关闭标签页…": "closeTab",
            "关闭 Agent…": "close", "删除对话…": "delete",
        ]
        var seen: [String] = []
        let menu = app.collectionViews.containing(NSPredicate(format: "label IN %@", Array(wanted.keys))).firstMatch
        XCTAssertTrue(menu.waitForExistence(timeout: 5), "no system action menu:\n\(app.debugDescription)")
        for _ in 0..<8 {
            let labels = menu.buttons.allElementsBoundByIndex.compactMap { wanted[$0.label] }
            let before = seen
            for label in labels where !seen.contains(label) { seen.append(label) }
            print("Agent menu entries: \(seen.joined(separator: ","))")
            if before == seen, seen.count > 3 { break }
            menu.swipeDown()
            sleep(1)
        }
        return seen
    }

    @MainActor private func shot(_ name: String) {
        let image = app.screenshot()
        let attachment = XCTAttachment(screenshot: image)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testSameAgentRowAndDetailHaveTheSameOrderedMenu() {
        launch()
        let rowItems = openRowMenu()
        shot("agent-row-menu-zh")
        app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.1)).tap()
        let detailItems = openDetailMenu()
        shot("agent-detail-menu-zh")
        XCTAssertEqual(detailItems, rowItems, "the detail More menu differs from the row's long-press menu")
        XCTAssertTrue(rowItems.contains("pin"))
    }

    @MainActor func testMenusInDarkAndLargestText() {
        for (name, args) in [
            ("dark", ["-ui.scheme", "dark"]),
            ("ax5", ["-ui.scheme", "light", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"]),
        ] {
            launch(args)
            _ = openRowMenu()
            shot("agent-row-menu-zh-" + name)
            app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.1)).tap()
            _ = openDetailMenu()
            shot("agent-detail-menu-zh-" + name)
        }
    }

    @MainActor func testDetailPinAndRowFilesNavigationWork() {
        launch()
        _ = openDetailMenu()
        app.buttons["置顶"].tap()
        app.buttons.matching(NSPredicate(format: "label == %@", "返回")).firstMatch.tap()
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["取消置顶"].waitForExistence(timeout: 5), "detail's Pin did not reach the same Agent's row")
        app.buttons["文件"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["files.pane"].waitForExistence(timeout: 10),
                      "the row's Files action must open the Agent's Files page, not only its chat")
    }
}
