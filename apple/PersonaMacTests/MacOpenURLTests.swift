import Foundation
import Testing
@testable import Persona

/// `persona://` links handed to the Mac app (another app, a shortcut, the browser): `persona://open?machine=&session=`
/// opens that chat, as the iPhone does for its Live Activity's links; `persona://pair?…` pairs; anything else is ignored.
struct MacOpenURLTests {
    @Test func onlyPersonaSchemeRoutesIgnoringCase() {
        for scheme in ["persona", "PERSONA"] {
            #expect(MacOpenURL.route(URL(string: "\(scheme)://open?machine=mac&session=abc-1")!)
                    == .session(SessionKey(machineID: "mac", sessionID: "abc-1")))
            #expect(MacOpenURL.route(URL(string: "\(scheme)://open")!) == .app)
            #expect(MacOpenURL.route(URL(string: "\(scheme)://pair?url=https%3A%2F%2Fhub.example.com&token=t")!) == .pair)
            #expect(MacOpenURL.route(URL(string: "\(scheme)://settings")!) == .ignore)
        }
    }
    @Test func openLinksNameAChat() {
        #expect(MacOpenURL.route(URL(string: "persona://open?machine=mac&session=abc-1")!) == .session(SessionKey(machineID: "mac", sessionID: "abc-1")))
    }

    /// The Live Activity's link for the whole fleet: just the app.
    @Test func openWithoutASessionIsTheApp() {
        #expect(MacOpenURL.route(URL(string: "persona://open")!) == .app)
        #expect(MacOpenURL.route(URL(string: "persona://open?machine=mac")!) == .app)
    }

    @Test func pairLinksPair() {
        #expect(MacOpenURL.route(URL(string: "persona://pair?url=https%3A%2F%2Fhub.example.com&token=t")!) == .pair)
    }

    /// A computer's link (「添加电脑」, Add computer) is Persona Link's, on the computer joining: never a pairing of this app (#4198).
    @Test func computerLinksGoToPersonaLink() {
        for scheme in ["persona", "PERSONA"] {
            let url = URL(string: "\(scheme)://link?url=http%3A%2F%2F192.0.2.10%3A18660&token=jp_x&machine=macbook")!
            #expect(MacOpenURL.route(url) == .computerLink)
            #expect(ServerConfig(pairURL: url.absoluteString) == nil)
            #expect(ServerConfig.isComputerLink(url))
        }
        #expect(!ServerConfig.isComputerLink(URL(string: "persona://pair?url=https%3A%2F%2Fhub.example.com&token=t")!))
    }

    /// Persona Link is found by its executable among the scheme's handlers (its bundle id is configurable), then where
    /// `mac-link.sh install` puts it; this app itself is not it.
    @Test func personaLinkIsFoundByItsExecutable() {
        let url = URL(string: "persona://link?token=t")!
        let mac = URL(fileURLWithPath: "/Applications/Persona.app")
        let link = URL(fileURLWithPath: "/Applications/Persona Link.app")
        let info: (URL) -> [String: Any]? = { u in
            u == mac ? ["CFBundleExecutable": "PersonaMac"] : u == link ? ["CFBundleExecutable": "PersonaLink"] : nil
        }
        #expect(PersonaLinkHandoff.app(for: url, handlers: [mac, link], fallbacks: [], info: info) == link)
        #expect(PersonaLinkHandoff.app(for: url, handlers: [mac], fallbacks: [link], info: info) == link)
        #expect(PersonaLinkHandoff.app(for: url, handlers: [mac], fallbacks: [], info: info) == nil)
    }

    @Test func sentSettingsLinksOpenTheNestedRecordsPage() {
        for path in ["/sent", "?tab=sent", "/connectors/-sent", "?tab=connectors&connector=-sent"] {
            #expect(MacOpenURL.route(URL(string: "persona://settings" + path)!) == .sentRecords)
        }
        #expect(MacOpenURL.route(URL(string: "persona://settings/connectors/telegram")!) == .ignore)
    }

    @MainActor @Test func sentLinksReachTheAppModelsNestedSettingsPath() {
        let model = AppModel()
        for path in ["/sent", "?tab=sent", "?tab=connectors&connector=-sent"] {
            model.openURL(URL(string: "persona://settings" + path)!)
            #expect(model.settingsPath == SettingsPage.sentPath)
            model.settingsPath.removeLast()
            var parent = SettingsPage.sentPath
            parent.removeLast()
            #expect(model.settingsPath == parent)
        }
    }

    @Test func otherLinksAreIgnored() {
        for other in ["other-product", "personas", "personad"] {
            #expect(MacOpenURL.route(URL(string: "\(other)://open?machine=a&session=b")!) == .ignore)
        }
        #expect(MacOpenURL.route(URL(string: "https://example.com/open?machine=a&session=b")!) == .ignore)
        #expect(MacOpenURL.route(URL(string: "persona://settings")!) == .ignore)
    }
}
