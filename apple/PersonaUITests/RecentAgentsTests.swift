import XCTest

/// The flat activity timeline against the standard mock hub (no extra scenarios).
/// TEST_RUNNER_PERSONA_RECENT_HUB=<mock URL>; runs in the iPhone's Shell.
final class RecentAgentsTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!
    private let widget = "给天气 App 加主屏幕小组件"
    private let orders = "排查 /orders 接口变慢"
    private let release = "整理发布说明"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let url = ProcessInfo.processInfo.environment["PERSONA_RECENT_HUB"] else {
            throw XCTSkip("needs the standard mock hub (PERSONA_RECENT_HUB)")
        }
        hub = url
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.name = "failure"
            shot.lifetime = .keepAlways
            add(shot)
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "view tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
        super.tearDown()
    }

    @MainActor private func api(_ path: String, pins: Bool = false) async throws -> [String: Any] {
        var request = URLRequest(url: URL(string: hub + path)!, timeoutInterval: 10)
        if pins {
            request.httpMethod = "PUT"
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = Data("{\"spaces\":[],\"sessions\":[]}".utf8)
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200, path)
        return try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
    }

    @MainActor private func launch(fresh: Bool) async throws {
        if fresh {
            _ = try await api("/mock/reset")
            _ = try await api("/api/v1/pins", pins: true)
            addUIInterruptionMonitor(withDescription: "notifications") { alert in
                for label in ["允许", "Allow"] where alert.buttons[label].exists {
                    alert.buttons[label].tap()
                    return true
                }
                return false
            }
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "chats", "-dev.openChat", "", "-tab", "dev"]
            + (fresh ? ["-openAgentsFresh", "YES", "-openChatsFresh", "YES"] : [])
        app.launchPinned()
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.buttons
            .matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 2) { allow.tap() }
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 20))
    }

    @MainActor private func menu() -> XCUIElement {
        app.buttons.matching(identifier: "shell-agents").allElementsBoundByIndex.first(where: \.isHittable)
            ?? app.buttons["shell-agents"].firstMatch
    }

    @MainActor private func choose(_ title: String) {
        menu().tap()
        let item = app.buttons[title].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), title)
        item.tap()
    }

    @MainActor private func recent() {
        choose("最近使用")
        choose("细列表")
        XCTAssertTrue(row(widget).waitForExistence(timeout: 10))
    }

    @MainActor private func row(_ title: String) -> XCUIElement {
        app.buttons.matching(identifier: "agents.compactRow")
            .matching(NSPredicate(format: "label CONTAINS %@", "、" + title + "、")).firstMatch
    }

    @MainActor private func ordered(_ titles: [String], file: StaticString = #filePath, line: UInt = #line) {
        var previous = -CGFloat.infinity
        for title in titles {
            let r = row(title)
            XCTAssertTrue(r.waitForExistence(timeout: 8), title, file: file, line: line)
            XCTAssertGreaterThan(r.frame.minY, previous, "activity order: \(title)", file: file, line: line)
            previous = r.frame.minY
        }
    }

    @MainActor private func shot(_ name: String) throws {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let path = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["PERSONA_STYLE"] ?? "system"
            let layout = "shell"
            try screenshot.pngRepresentation.write(to: URL(fileURLWithPath: path).appendingPathComponent("\(name)-\(layout)-\(style).png"))
        }
    }

    @MainActor func testRecentChoiceMixesProjectsAndSurvivesRelaunch() async throws {
        try await launch(fresh: true)
        recent()
        ordered([widget, orders, "补齐网络层的单元测试", release])
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "weather，")).firstMatch.exists)
        menu().tap()
        XCTAssertFalse(app.buttons["全部折叠"].exists)
        XCTAssertFalse(app.buttons["全部展开"].exists)
        app.buttons["最近使用"].firstMatch.tap()
        try shot("recent-compact")

        app.terminate()
        try await launch(fresh: false)
        ordered([widget, orders, "补齐网络层的单元测试", release])
        choose("头像") // back from Compact List: the rows as they were (#2530: Avatar, the default style)
        let card = app.buttons.matching(identifier: "agents.row").firstMatch
        app.bringAboveTabBar(card)
        XCTAssertTrue(card.exists)
        try shot("recent-cards")
    }

    @MainActor func testReadingAReplyKeepsItInTheTimeline() async throws {
        try await launch(fresh: true)
        recent()
        let title = "博客迁移到 Astro" // This mock session has a real reply to load and mark read.
        app.bringAboveTabBar(row(title))
        row(title).tap()
        let back = app.buttons["返回"].firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 10))
        let deadline = Date.now.addingTimeInterval(10)
        var seen = false
        repeat {
            seen = (try await api("/mock/seen")["seen"] as? [String] ?? [])
                .contains("a1f3c2d4-0000-4000-8000-000000000003")
            if !seen { try await Task.sleep(for: .milliseconds(200)) }
        } while !seen && Date.now < deadline
        XCTAssertTrue(seen, "the hub received the read receipt")
        back.tap()
        ordered(["照片同步脚本支持 HEIC", title, "检查 PR #42 的改动"])
        try shot("recent-after-reading")
        app.bringAboveTabBar(row("检查 PR #42 的改动"))
        XCTAssertTrue(row("检查 PR #42 的改动").exists, "idle history is directly accessible")
        app.bringAboveTabBar(row("菜谱网站加搜索"))
        XCTAssertTrue(row("菜谱网站加搜索").exists, "offline history is directly accessible")
        try shot("recent-idle-and-offline")
    }

    @MainActor func testLiveActivityReordersAcrossProjects() async throws {
        try await launch(fresh: true)
        recent()
        ordered([widget, orders])
        _ = try await api("/mock/churn?n=2&ms=200&steps=0")
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline {
            if row(orders).exists && row(widget).exists && row(orders).frame.minY < row(widget).frame.minY { break }
            try await Task.sleep(for: .milliseconds(200))
        }
        ordered([orders, "补齐网络层的单元测试", release, widget])
        try shot("recent-live-update")
    }

    @MainActor func testRecentKeepsMachineFilteringAndSearch() async throws {
        try await launch(fresh: true)
        recent()
        choose("MacBook")
        XCTAssertTrue(row(widget).waitForExistence(timeout: 8))
        XCTAssertFalse(row(orders).exists, "other machines are filtered out")
        let field = app.revealAgentsSearch()
        field.tap()
        field.typeText("菜谱")
        XCTAssertTrue(row("菜谱网站加搜索").waitForExistence(timeout: 8))
        XCTAssertFalse(row(widget).exists)
        XCTAssertFalse(app.buttons.matching(identifier: "agents.quietSpace").firstMatch.exists)
        try shot("recent-search-offline")
    }
}
