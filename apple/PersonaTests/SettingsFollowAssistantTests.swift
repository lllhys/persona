import Foundation
import Testing
@testable import Persona

/// Requests of Settings' first group that went to the default assistant whichever was current (docs/assistant/managers.md
/// "The current assistant"): Skills, Auto-Start Tasks, Outlook sign-in on the mail page, an extension form's launchers
/// and the chat connector's status now go to the assistant the page opened for. The app's client names the current
/// assistant on the assistant's own routes only (`showing`); these pages name it on theirs (`currentAssistantSettings`).
/// Another assistant current: each request names it. The default current, or one assistant (the app's client then
/// names none): none, as before. Made-up values.
@MainActor
struct SettingsFollowAssistantTests {
    private static let header = APIClient.managerHeader

    /// The app's client with "work" current, and with the default current or a hub with one assistant.
    private static let picks: [(shown: String?, want: String?)] = [(shown: "work", want: "work"), (shown: nil, want: nil)]

    private func named(_ requests: [LocalRequest]) -> [String?] { requests.map { $0.headers[Self.header] } }

    @Test func skillsAreTheCurrentAssistants() async {
        for pick in Self.picks {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/skills", json: #"{"machine":"server","skills":[]}"#)
            let page = SkillsModel(api: { hub.client.showing(pick.shown) })
            await page.load()
            await page.load()
            #expect(named(hub.sent("GET", "/api/v1/skills")) == [pick.want, pick.want], "\(pick.shown ?? "default")")
        }
    }

    @Test func autoStartTasksAreTheCurrentAssistants() async {
        for pick in Self.picks {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/task-trust", json: #"{"kinds":[{"key":"k1","machine":"MacBook","cwd":"/work/app","readonly":true}]}"#)
            hub.on("DELETE", "/api/v1/task-trust", json: "{}")
            let page = TaskTrustModel(api: { hub.client.showing(pick.shown) })
            await page.load()
            #expect(page.kinds?.map(\.key) == ["k1"])
            await page.forget(page.kinds ?? [])
            // Read, forget, read again: each names the page's assistant.
            #expect(hub.requests.count == 3)
            #expect(named(hub.requests) == Array(repeating: pick.want, count: 3), "\(named(hub.requests))")
        }
    }

    /// Outlook sign-in on Add Mail Account goes to the mail page's assistant (its client, kept from when it opened), else
    /// to the current one as the app names it.
    @Test func outlookSignInGoesToTheMailPagesAssistant() async throws {
        for pick in Self.picks {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/mail/oauth/microsoft/config", json: #"{"configured":false}"#)
            let app = hub.client.showing(pick.shown)
            let current = try #require(OutlookSignInSection.client(nil, app: app))
            _ = try? await current.microsoftConfig()
            let mailPage = try #require(OutlookSignInSection.client(app.currentAssistantSettings(), app: hub.client.showing("other")))
            _ = try? await mailPage.microsoftConfig()
            #expect(named(hub.requests) == [pick.want, pick.want], "the mail page's own, not whoever is current later")
        }
    }

    private static let chat = #"""
    {"id":"example-chat","name":"Example Chat","kind":"connectors","status":"running","source":"installed","enabled":true,
     "form":{"fields":[{"name":"proxy","type":"object","fields":[{"name":"launcher","type":"string","format":"launcher"}]}]}}
    """#

    /// An extension form's launcher picker lists its own assistant's model providers: they are what the extension can use.
    @Test func anExtensionFormsLaunchersAreItsAssistants() async {
        for manager in ["work", nil] as [String?] {
            let hub = FakeHub()
            hub.on("GET", "/api/v1/plugins/example-chat/-/", json: Self.chat)
            hub.on("GET", "/api/v1/launchers", json: #"{"launchers":[{"name":"claude","source":"builtin"}]}"#)
            let page = ExtensionModel(id: "example-chat", manager: manager, deps: .init(api: { hub.client }))
            await page.load()
            await page.loadLaunchersIfNeeded()
            #expect(page.launchers?.map(\.name) == ["claude"])
            #expect(named(hub.sent("GET", "/api/v1/launchers")) == [manager], "\(manager ?? "default")")
        }
    }

    /// An assistant without the chat connector (its manager has no route for it) shows it not set up, as the web app does,
    /// rather than a status that could not be read; another failed read is no answer.
    @Test func anAssistantWithoutTheChatConnectorShowsItNotSetUp() async {
        let none = FakeHub() // no route: 404
        let missing = await none.client.showing("work").currentAssistantSettings().chatConnectorStatus()
        #expect(missing.absent && missing.status == LarkStatus.absent)
        #expect(ConnectorText.larkSummary(missing.status) == "未配置")
        #expect(named(none.requests) == ["work"])

        let down = FakeHub()
        down.on("GET", "/api/v1/lark/status") { _ in throw APIError(status: 502, message: "bad gateway") }
        let failed = await down.client.chatConnectorStatus()
        #expect(failed.status == nil && !failed.absent, "the row keeps what it had")

        let up = FakeHub()
        up.on("GET", "/api/v1/lark/status", json: #"{"configured":true,"connected":true,"name":"Example"}"#)
        let read = await up.client.showing(nil).currentAssistantSettings().chatConnectorStatus()
        #expect(read.status?.connected == true && !read.absent && named(up.requests) == [nil])
    }

    /// A coded 404 can mean the requested assistant is unknown; only an uncoded route absence means not set up.
    @Test func anUnknownAssistantDoesNotMeanTheChatConnectorIsAbsent() async {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/lark/status") { _ in
            throw APIError(status: 404, message: "unknown assistant", fields: ["code": "no_manager"])
        }
        let result = await hub.client.showing("unknown").currentAssistantSettings().chatConnectorStatus()
        #expect(result.status == nil)
        #expect(!result.absent)
    }
}
