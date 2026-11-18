import Foundation
import Testing
@testable import Persona

@MainActor
struct AgentsTabNavigationTests {
    private let agent = SessionKey(machineID: "example-computer", sessionID: "example-session")

    @Test func explicitSelectionCancelsAnAgentOpenAndItsRestoreMarker() {
        let old = AppModel.rememberedChat.flatMap(SessionKey.init(path:))
        defer { AppModel.rememberChat(old) }
        let model = AppModel()
        model.open(agent)
        AppModel.rememberChat(agent)
        model.selectHomeTab(.assistant)
        let before = model.agentsListReveal
        model.selectHomeTab(.chats)
        #expect(model.selectedTab == .chats)
        #expect(model.openChat == nil)
        #expect(AppModel.rememberedChat == nil)
        #expect(model.agentsListReveal > before)
    }

    @Test func reselectingAgentsStillRequestsItsList() {
        let old = AppModel.rememberedChat.flatMap(SessionKey.init(path:))
        defer { AppModel.rememberChat(old) }
        let model = AppModel()
        model.selectedTab = .chats
        AppModel.rememberChat(agent)
        let revision = model.agentsNavigationRevision
        model.selectHomeTab(.chats)
        #expect(model.agentsListReveal == revision + 1)
        #expect(AppModel.rememberedChat == nil)
        model.selectHomeTab(.chats)
        #expect(model.agentsListReveal == revision + 2)
    }

    @Test func notificationAndLinkOpensDoNotRequestTheList() {
        let old = AppModel.rememberedChat.flatMap(SessionKey.init(path:))
        defer { AppModel.rememberChat(old) }
        let model = AppModel()
        model.selectHomeTab(.chats)
        let before = model.agentsListReveal
        model.open(agent, latestAnswer: true)
        #expect(model.selectedTab == .chats)
        #expect(model.openChat == agent)
        #expect(model.latestAnswerFor == agent)
        #expect(model.agentsListReveal == before)
        model.openChat = nil
        model.selectHomeTab(.assistant)
        model.openLinked(agent, item: "example-message")
        #expect(model.openChat == agent)
        #expect(model.messageJump?.key == agent)
        #expect(model.messageJump?.item == "example-message")
        #expect(model.agentsListReveal == before)
    }

    @Test func aConsumedTargetedOpenStillInvalidatesAnOlderListReset() {
        let old = AppModel.rememberedChat.flatMap(SessionKey.init(path:))
        defer { AppModel.rememberChat(old) }
        let model = AppModel()
        model.selectHomeTab(.chats)
        let request = model.agentsListReveal
        model.open(agent)
        model.openChat = nil // ChatsView has already taken the target before the delayed reset runs.
        #expect(model.agentsListReveal == request)
        #expect(model.agentsNavigationRevision != request)
    }

    @Test func choosingAssistantKeepsItsConversationAndTheAgentRestoreState() {
        let old = AppModel.rememberedChat.flatMap(SessionKey.init(path:))
        defer { AppModel.rememberChat(old) }
        let model = AppModel()
        let conversation = SessionKey(machineID: "example-computer", sessionID: "example-conversation")
        model.assistantCurrent = conversation
        model.selectedTab = .chats
        AppModel.rememberChat(agent)
        model.selectHomeTab(.assistant)
        #expect(model.assistantCurrent == conversation)
        #expect(model.agentsListReveal == 0)
        #expect(AppModel.rememberedChat == agent.path)
    }
}
