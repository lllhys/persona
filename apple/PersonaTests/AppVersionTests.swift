import Foundation
import Testing
import UIKit
@testable import Persona

/// The app's version as phone_device_info and client_hello report it (#3672).
@MainActor
struct AppVersionTests {
    @Test func readsTheBundleVersionAndBuild() {
        let app = AppVersion(info: ["CFBundleShortVersionString": "1.4.0", "CFBundleVersion": "52"])
        #expect(app.version == "1.4.0" && app.build == "52")
        #expect(app.channel == "debug") // tests run a Debug build
        let none = AppVersion(info: [:])
        #expect(none.version.isEmpty && none.build.isEmpty)
    }

    /// The hub splits the hello's version at "+" into the version and the build: the same text the header carries.
    @Test func theHelloVersionIsTheVersionThenTheBuild() {
        let info = Bundle.main.infoDictionary ?? [:]
        let app = AppVersion(info: info)
        #expect(RequestClientMetadata.current.version == (app.build.isEmpty ? app.version : app.version + "+" + app.build))
    }

    /// client_hello's os: the system's name (iPadOS on an iPad) and version.
    @Test func theHelloOSNamesTheSystem() {
        let os = AppVersion.helloOS
        let name = os["name"] as? String
        #expect(name == (UIDevice.current.userInterfaceIdiom == .pad ? "iPadOS" : "iOS"))
        let v = ProcessInfo.processInfo.operatingSystemVersion
        #expect((os["version"] as? String)?.hasPrefix("\(v.majorVersion).\(v.minorVersion)") == true)
    }

    /// phone_device_info carries them under app_version, app_build and app_channel.
    @Test func deviceInfoReportsTheAppVersion() async throws {
        let content = await DeviceTool.info()
        let text = try #require(content.first?.text)
        let object = try #require(try JSONSerialization.jsonObject(with: Data(text.utf8)) as? [String: Any])
        let app = AppVersion.current
        #expect(object["app_version"] as? String == app.version)
        #expect(object["app_build"] as? String == app.build)
        #expect(object["app_channel"] as? String == app.channel)
    }
}
