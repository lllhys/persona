import Foundation
import SwiftUI
import Testing
@testable import Persona

@MainActor struct SentSettingsNavigationTests {
    @Test func theLedgerHasAConnectorsParent() {
        var path = SettingsPage.sentPath
        #expect(path == NavigationPath([SettingsPage.connectors, .sent]))
        path.removeLast()
        #expect(path == NavigationPath([SettingsPage.connectors]))
    }

    @Test func aLegacyTopLevelPathIsNormalizedAndBackKeepsConnectors() {
        let model = AppModel()
        model.settingsPath = NavigationPath([SettingsPage.sent])
        #expect(model.settingsPath == SettingsPage.sentPath)
        model.settingsPath.removeLast()
        #expect(model.settingsPath == NavigationPath([SettingsPage.connectors]))
        model.settingsPath = NavigationPath([SettingsPage.channels])
        #expect(model.settingsPath == NavigationPath([SettingsPage.channels]))
    }

    @Test func legacyAndCanonicalNativeLinksHaveTheSameDestination() {
        for path in ["/sent", "?tab=sent", "/connectors/-sent", "?tab=connectors&connector=-sent"] {
            #expect(SentSettingsLink.matches(URL(string: "persona://settings" + path)!))
        }
        #expect(SentSettingsLink.matches(URL(string: "PERSONA://SETTINGS/sent")!))
        for other in ["persona://settings", "persona://settings/connectors/sent", "persona://settings/connectors/telegram", "persona://settings/sent/extra",
                      "persona://settings?tab=connectors", "persona://settings?tab=channels&connector=-sent",
                      "persona://other/sent", "https://example.com/settings/sent", "other://settings/sent"] {
            #expect(!SentSettingsLink.matches(URL(string: other)!))
        }
    }

    @Test func channelLinksDoNotMistakeTheLedgerForAConnectorID() {
        for path in ["settings/sent", "settings/connectors/-sent"] {
            #expect(ChannelText.page(path) == .sent)
        }
        #expect(ChannelText.page("settings/connectors") == .connectors)
        #expect(ChannelText.page("settings/connectors/telegram") == .connector("telegram"))
        #expect(ChannelText.page("settings/connectors/sent") == .connector("sent"))
        #expect(ChannelText.page("settings/connectors/sent/extra") == nil)
    }
}
