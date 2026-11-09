import Foundation
import Testing
@testable import Persona

@MainActor
struct LarkOutboxTests {
    private func mine(_ text: String, at: Date) -> LarkMessage {
        LarkMessage(id: UUID().uuidString, position: "1", senderName: "我", isMine: true, type: "TEXT", text: text,
                    createdAt: at, recalled: false, replyCount: 0)
    }

    @Test func aSentMessageGivesWayToItsCopy() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now)
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [], now: now).count == 1)
        #expect(LarkOutbox.remaining([o], messages: [mine(" 好的\n", at: now.addingTimeInterval(1))], now: now).isEmpty)
    }

    @Test func sendingAndFailedOnesStay() {
        let now = Date()
        let sending = LarkOutgoing(text: "好的", at: now)
        var failed = LarkOutgoing(text: "好的", at: now)
        failed.state = .failed("超时")
        let left = LarkOutbox.remaining([sending, failed], messages: [mine("好的", at: now)], now: now)
        #expect(left.count == 2)
    }

    @Test func oneCopyAnswersOneMessage() {
        let now = Date()
        var a = LarkOutgoing(text: "收到", at: now)
        var b = LarkOutgoing(text: "收到", at: now)
        a.state = .sent
        b.state = .sent
        let left = LarkOutbox.remaining([a, b], messages: [mine("收到", at: now)], now: now)
        #expect(left.map(\.id) == [b.id])
    }

    @Test func anOldMessageWithTheSameWordsIsNotItsCopy() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now)
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [mine("好的", at: now.addingTimeInterval(-3600))], now: now).count == 1)
    }

    @Test func aSentMessageGoesAfterAWhileAnyway() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now.addingTimeInterval(-LarkOutbox.copyWait - 1))
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [], now: now).isEmpty)
    }

    @Test func aRetryKeepsTheMessagesID() {
        var o = LarkOutgoing(text: "好的", at: Date(timeIntervalSince1970: 0))
        o.state = .failed("超时")
        let id = o.id, clientID = o.clientID
        o.retry(at: Date(timeIntervalSince1970: 60))
        #expect(o.id == id && o.clientID == clientID)
        #expect(o.state == .sending && o.at == Date(timeIntervalSince1970: 60))
    }

    // MARK: Replies

    private func quoted(_ id: String) -> LarkReplyTo { LarkReplyTo(id: id, senderName: "李雷", text: "周六去吗") }

    @Test func aQuotedReplyGivesWayToItsCopyOnce() {
        let now = Date()
        var o = LarkOutgoing(text: "去", at: now, replyTo: quoted("m1"))
        o.state = .sent
        var copy = mine("去", at: now.addingTimeInterval(1))
        copy.replyTo = quoted("m1")
        #expect(LarkOutbox.remaining([o], messages: [copy], now: now).isEmpty)
        // A copy that quotes another message is not this one's.
        var other = copy
        other.replyTo = quoted("m9")
        #expect(LarkOutbox.remaining([o], messages: [other], now: now).count == 1)
        // A hub that leaves the quote out still answers it.
        #expect(LarkOutbox.remaining([o], messages: [mine("去", at: now.addingTimeInterval(1))], now: now).isEmpty)
        // Two quoted replies with the same words and one copy: one stays.
        var p = LarkOutgoing(text: "去", at: now, replyTo: quoted("m1"))
        p.state = .sent
        #expect(LarkOutbox.remaining([o, p], messages: [copy], now: now).map(\.id) == [p.id])
    }

    @Test func theSendingRowDrawsTheQuoteButAThreadReplyDoesNot() {
        let quote = LarkOutgoing(text: "去", replyTo: quoted("m1"))
        #expect(quote.message(like: nil).replyTo == quoted("m1"))
        let thread = LarkOutgoing(text: "去", replyTo: quoted("m1"), thread: true)
        #expect(thread.message(like: nil).replyTo == nil)
        #expect(LarkOutgoing(text: "去").message(like: nil).replyTo == nil)
    }

    @Test func aRetriedReplyKeepsItsIDAndWhatItAnswers() {
        var o = LarkOutgoing(text: "去", at: Date(timeIntervalSince1970: 0), replyTo: quoted("m1"), thread: true)
        o.state = .failed("超时")
        let clientID = o.clientID
        o.retry(at: Date(timeIntervalSince1970: 60))
        #expect(o.clientID == clientID && o.replyTo == quoted("m1") && o.thread)
        #expect(o.state == .sending)
    }

    private func bodies(_ hub: FakeHub, _ path: String) throws -> [NSDictionary] {
        try hub.sent("POST", path).map { r in (try JSONSerialization.jsonObject(with: r.body ?? Data()) as? NSDictionary) ?? [:] }
    }

    @Test func aReplyGoesToTheMessageWithThreadAndClientID() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/lark/messages/m1/replies", json: #"{"ok":true,"message_id":"m9"}"#)
        hub.on("POST", "/api/v1/lark/conversations/f1/messages", json: "{}")
        let inThread = LarkOutgoing(text: "报名", replyTo: quoted("m1"), thread: true)
        let quotedReply = LarkOutgoing(text: "去", replyTo: quoted("m1"))
        try await LarkOutbox.deliver(inThread, feed: "f1", api: hub.client)
        try await LarkOutbox.deliver(inThread, feed: "f1", api: hub.client) // the retry
        try await LarkOutbox.deliver(quotedReply, feed: "f1", api: hub.client)
        let sent = try bodies(hub, "/api/v1/lark/messages/m1/replies")
        let id = inThread.id.uuidString.lowercased()
        #expect(sent == [["text": "报名", "thread": true, "client_id": id], ["text": "报名", "thread": true, "client_id": id],
                         ["text": "去", "thread": false, "client_id": quotedReply.clientID]])
        #expect(try hub.sent("POST", "/api/v1/lark/conversations/f1/messages").isEmpty, "a reply is not a plain send")
    }

    @Test func aPlainMessageStillGoesToTheChat() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/lark/conversations/f1/messages", json: "{}")
        let o = LarkOutgoing(text: "收到")
        try await LarkOutbox.deliver(o, feed: "f1", api: hub.client)
        #expect(try bodies(hub, "/api/v1/lark/conversations/f1/messages") == [["text": "收到", "client_id": o.clientID]])
    }
}

/// Reply in the long-press menu: where the answer goes, and the line the quote bar shows.
@MainActor
struct LarkReplyTests {
    private func message(_ id: String = "m1", text: String = "周六去吗", type: String = "TEXT", recalled: Bool = false) -> LarkMessage {
        LarkMessage(id: id, position: "1", senderName: "李雷", isMine: false, type: type, text: text, createdAt: nil, recalled: recalled, replyCount: 0)
    }

    @Test func aChatQuotesATopicGroupOpensTheThreadAThreadUsesItsBox() {
        #expect(LarkReply.route(for: message(), inTopicGroup: false, inThread: false) == .quote)
        #expect(LarkReply.route(for: message(), inTopicGroup: true, inThread: false) == .openThread)
        #expect(LarkReply.route(for: message(), inTopicGroup: false, inThread: true) == .threadBox)
        #expect(LarkReply.route(for: message(), inTopicGroup: true, inThread: true) == .threadBox)
    }

    @Test func nothingToAnswerHasNoReply() {
        #expect(LarkReply.route(for: message(recalled: true), inTopicGroup: false, inThread: false) == nil)
        #expect(LarkReply.route(for: message(type: "SYSTEM"), inTopicGroup: false, inThread: false) == nil)
        #expect(LarkReply.route(for: message("outgoing-1"), inTopicGroup: false, inThread: false) == nil)
    }

    @Test func theSummaryIsOneLineOfTheText() {
        #expect(LarkReply.summary(of: message(text: "**集合**地点\n\n  北门  \n见")) == "集合地点 北门 见")
        #expect(LarkReply.summary(of: message(text: "", type: "IMAGE")) == "[图片]")
        #expect(LarkReply.summary(of: message(text: "  ", type: "INTERACTIVE")) == "[消息]")
        let q = LarkReply.quote(message(text: "第一行\n第二行"))
        #expect(q == LarkReplyTo(id: "m1", senderName: "李雷", text: "第一行 第二行"))
    }
}
