import Foundation
import SwiftUI
import Testing
import UIKit
@testable import Persona

/// An inbox item's chat reads, sends and summarizes through the item's manager (docs/assistant/managers.md "Grouping"):
/// every request names it, a person or direct chat opened from it stays on it, the user it reads as and the messages it
/// keeps are its own, and the pictures and media the chat loads by URL name it too. The default's chat names none, as
/// before. Made-up values.
@MainActor
struct LarkChatManagerTests {
    private static let header = "X-Persona-Manager"
    private static let messages = "/api/v1/lark/conversations/f1/messages"

    /// A client like the app's for the hub-level group, on a host of its own (the messaging source is kept per host).
    private func client(_ hub: FakeHub) -> APIClient {
        var c = hub.client
        c.config = ServerConfig(url: URL(string: "https://chat-manager.invalid")!, token: "t")
        return c
    }

    private func fakeHub() -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", Self.messages, json: #"{"messages":[],"has_older":false}"#)
        hub.on("POST", Self.messages, json: "{}")
        hub.on("POST", "/api/v1/assistant/conversations", json: #"{"session":{"machine_id":"server","session_id":"s-1"}}"#)
        return hub
    }

    // MARK: requests

    @Test func theChatReadsAndSendsThroughItsItemsManager() async throws {
        let hub = fakeHub()
        let api = try #require(LarkChatView.client(client(hub), manager: "m-2", account: 2))
        _ = try await api.larkMessages("f1")
        _ = try await api.larkSend("f1", text: "On my way")
        let read = hub.sent("GET", Self.messages), sent = hub.sent("POST", Self.messages)
        #expect(read.map { $0.headers[Self.header] } == ["m-2"])
        #expect(sent.map { $0.headers[Self.header] } == ["m-2"])
        #expect(read.first?.query["ol_account"] == "2", "still the chat's account")
    }

    @Test func theSummaryStartsOnTheChatsManager() async throws {
        let hub = fakeHub()
        // The app's client while the Persona page shows yet another manager: the chat's names its own.
        let api = try #require(LarkChatView.client(client(hub).showing("m-3"), manager: "m-2", account: nil))
        let s = try await api.showing(nil).startConversation(prompt: "Summarize this chat")
        #expect(hub.sent("POST", "/api/v1/assistant/conversations").map { $0.headers[Self.header] } == ["m-2"])
        #expect(s.manager == "m-2", "the conversation is the chat's manager's")
    }

    @Test func theDefaultsChatNamesNone() async throws {
        let hub = fakeHub()
        // As the app's client is while the Persona page shows another manager.
        let api = try #require(LarkChatView.client(client(hub).showing("m-2"), manager: nil, account: nil))
        _ = try await api.larkMessages("f1")
        _ = try await api.larkSend("f1", text: "On my way")
        _ = try await api.showing(nil).startConversation(prompt: "Summarize this chat")
        let all = hub.sent("GET", Self.messages) + hub.sent("POST", Self.messages) + hub.sent("POST", "/api/v1/assistant/conversations")
        #expect(all.count == 3 && all.allSatisfy { $0.headers[Self.header] == nil }, "the default's names none")
    }

    /// Two managers may be signed in as two users: the user one's chat reads as (which messages are mine, whom a send
    /// expects) is never the other's, the default's included.
    @Test func eachManagersChatKeepsItsOwnUser() async throws {
        let hub = FakeHub(), me = "/api/v1/messaging/chat/me"
        hub.on("GET", me) { r in Data((r.headers["X-Persona-Manager"] == "m-2" ? #"{"user_id":"u-2"}"# : #"{"user_id":"u-1"}"#).utf8) }
        var api = hub.client
        api.config = ServerConfig(url: URL(string: "https://chat-user-\(UUID().uuidString.lowercased()).invalid")!, token: "t")
        let named = try #require(LarkChatView.client(api, manager: "m-2", account: nil))
        let home = try #require(LarkChatView.client(api, manager: nil, account: nil))
        let read = try await named.messagingUser("chat", refresh: true)
        let homeRead = try await home.messagingUser("chat")
        let homeRefreshed = try await home.messagingUser("chat", refresh: true)
        let namedKept = try await named.messagingUser("chat")
        #expect(read == "u-2")
        #expect(homeRead == "u-1", "the default's asks for its own")
        #expect(homeRefreshed == "u-1")
        #expect(namedKept == "u-2", "the default's doesn't replace the named one's")
        #expect(hub.sent("GET", me).count == 3)
    }

    // MARK: pages opened from the chat

    @Test func theRouteToAChatCarriesItsManager() {
        let chat = LarkConversation(id: "oc_1", name: "Ada", isGroup: false)
        #expect(FeishuRoute.chat(chat, manager: "m-2").manager == "m-2")
        #expect(FeishuRoute.chat(chat).manager == nil, "the default's: nothing named")
        #expect(FeishuRoute.chat(chat, manager: "m-2") != FeishuRoute.chat(chat), "two managers' copies are two pages")
    }

    /// A person opened from the chat's document or from another profile, on a stack outside the chat.
    @Test func theRouteToAPersonCarriesItsManager() {
        let ada = LarkPersonRef(id: "ou_1", name: "Ada")
        #expect(FeishuRoute.profile(ada, manager: "m-2").manager == "m-2")
        #expect(FeishuRoute.profile(ada).manager == nil, "the default's: nothing named")
        #expect(FeishuRoute.profile(ada, manager: "m-2") != FeishuRoute.profile(ada), "two managers' copies are two pages")
    }

    // MARK: messages kept on disk

    @Test func aNamedManagersPageKeepsItsOwnMessages() {
        let chat = LarkConversation(id: "oc_1", name: "Ada", isGroup: false)
        #expect(LarkMessage.cacheName(chat, manager: nil) == "lark-chat-" + chat.key, "the default's: the one background sync keeps")
        #expect(LarkMessage.cacheName(chat, manager: "m-2") != LarkMessage.cacheName(chat), "never the default's copy")
        #expect(LarkMessage.cacheName(chat, manager: "m-2") != LarkMessage.cacheName(chat, manager: "m-3"))
        #expect(LarkMessage.cacheName(chat, manager: "m-2").hasPrefix("lark-chat-"), "cleared with the rest when what chats show changes")
    }

    // MARK: pictures and media

    #if os(iOS)
    /// The picture view itself, in a window: its bytes are kept on disk only under the URL that names the chat's manager,
    /// so it draws only when it asks for that one (the unnamed path has nothing on disk and no hub to answer it).
    @Test func aPictureInTheChatLoadsThroughItsManager() async throws {
        let path = "/api/v1/lark/raw/images/chat-manager-" + UUID().uuidString.lowercased()
        let named = LarkMedia.url(path, manager: "m-2")
        let kept = URL.cachesDirectory.appending(path: "lark-media", directoryHint: .isDirectory)
            .appending(path: LarkMedia.cacheKey(named, size: nil))
        try FileManager.default.createDirectory(at: kept.deletingLastPathComponent(), withIntermediateDirectories: true)
        try UIGraphicsImageRenderer(size: CGSize(width: 2, height: 2)).pngData { _ in }.write(to: kept)
        defer { try? FileManager.default.removeItem(at: kept) }

        let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 200, height: 200))
        window.rootViewController = UIHostingController(rootView: LarkRemoteImage(url: path).frame(width: 40, height: 40)
            .environment(\.larkManager, "m-2"))
        window.makeKeyAndVisible()
        defer { window.isHidden = true; window.rootViewController = nil }
        for _ in 0..<150 where LarkMedia.shared.cached(named) == nil { try await Task.sleep(for: .milliseconds(20)) }
        #expect(LarkMedia.shared.cached(named) != nil, "loaded through the chat's manager")
        #expect(LarkMedia.shared.cached(path) == nil, "never through the default's")
    }
    #endif

    @Test func thePlayerStreamsThroughTheChatsManager() throws {
        let hub = try #require(URL(string: "https://hub.example.com"))
        let path = "/api/v1/lark/media/video_1"
        #expect(LarkMedia.streamURL(path, hub: hub, manager: "m-2").absoluteString == "https://hub.example.com/api/v1/lark/media/video_1?manager=m-2")
        #expect(LarkMedia.streamURL(path, hub: hub, manager: "m 2&x").absoluteString == "https://hub.example.com/api/v1/lark/media/video_1?manager=m%202%26x")
        #expect(LarkMedia.streamURL(path, hub: hub, manager: nil) == hub.appending(path: path), "the default's: as before")
        #expect(LarkMedia.streamURL(path, hub: hub, manager: "") == hub.appending(path: path))
        #expect(LarkMedia.streamURL("https://cdn.example.com/v.mp4", hub: hub, manager: "m-2").absoluteString == "https://cdn.example.com/v.mp4", "a CDN's: as it is")
    }

    @Test func thePlayerKeepsTheMediaPathsQuery() throws {
        let hub = try #require(URL(string: "https://hub.example.com"))
        let path = "/api/v1/messaging/chat/media/video_1?account=2"
        #expect(LarkMedia.streamURL(path, hub: hub, manager: "m-2").absoluteString
                == "https://hub.example.com/api/v1/messaging/chat/media/video_1?account=2&manager=m-2", "the account still named")
        #expect(LarkMedia.streamURL("/api/v1/lark/media/video_1?manager=m-3", hub: hub, manager: "m-2").absoluteString
                == "https://hub.example.com/api/v1/lark/media/video_1?manager=m-3", "one that names a manager already: named once")
        let under = try #require(URL(string: "https://hub.example.com/persona/"))
        #expect(LarkMedia.streamURL(path, hub: under, manager: "m-2").absoluteString
                == "https://hub.example.com/persona/api/v1/messaging/chat/media/video_1?account=2&manager=m-2", "a hub under a path")
        #expect(LarkMedia.streamURL(path, hub: hub, manager: nil) == hub.appending(path: path), "the default's: as before")
        #expect(!LarkMedia.streamURL("//cdn.example.com/v.mp4", hub: hub, manager: "m-2").absoluteString.contains("manager="),
                "another host's: url(_:manager:) leaves it alone")
    }
}
