import Foundation
import Testing
@testable import Persona
import PersonaWire

/// An upstream refusal and its fallback model (#2528, #4489): what is read (never an older hub's record line), where the
/// refusal's line goes, what the card above the composer says and offers, and a refused going back in words. The
/// tests run in zh-Hans.
@MainActor
struct ModelFallbackTests {
    private let refusal = #"{"subtype":"model_refusal_fallback","original_model":"claude-opus-5-5","fallback_model":"claude-sonnet-5-5","category":"cyber","scope":"session","explanation":"The upstream provider declined this request.","text":"Claude Code is unable to respond to this request with claude-opus-5-5.","raw":"{\"type\":\"system\",\"secret\":\"x\"}"}"#

    private func session(_ extra: String) throws -> Session {
        let json = #"{"machine_id":"m","session_id":"s","title":"t","cwd":"/w","status":"done","state":"idle","agent":"claude"\#(extra)}"#
        return try JSON.decoder.decode(Session.self, from: Data(json.utf8))
    }

    private func item(_ seq: Int, _ kind: ItemKind, role: String, text: String?) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)
    }

    @Test func aRefusalIsReadWithoutTheRecordLine() throws {
        let json = #"{"id":"n","seq":2,"role":"system","kind":"notice","text":"{\"type\":\"system\",\"secret\":\"x\"}","model_refusal":\#(refusal)}"#
        let it = try JSON.decoder.decode(Item.self, from: Data(json.utf8)).publicRefusal()
        let r = try #require(it.modelRefusal)
        #expect(r.isFallback && r.originalModel == "claude-opus-5-5" && r.scope == "session")
        #expect(it.text == "Claude Code is unable to respond to this request with claude-opus-5-5.")
        // What the cache keeps has no `raw`.
        let kept = String(decoding: try JSON.encoder.encode(it), as: UTF8.self)
        #expect(!kept.contains("secret") && !kept.contains("raw"))
        // A malformed refusal doesn't drop the item; with no words the text is the explanation, else the subtype.
        let odd = try JSON.decoder.decode(Item.self, from: Data(#"{"id":"o","seq":3,"role":"system","kind":"notice","text":"x","model_refusal":"nope"}"#.utf8))
        #expect(odd.modelRefusal?.isFallback == false)
        #expect(ModelRefusal(subtype: "model_refusal", explanation: "说明").publicText == "说明")
        #expect(ModelRefusal(subtype: "model_refusal", text: "  ").publicText == "model_refusal")
    }

    @Test func theRefusalIsALineAfterTheExchangeNotAWorkStep() {
        var notice = item(2, .notice, role: "system", text: "Claude Code is unable…")
        notice.modelRefusal = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5", fallbackModel: "claude-sonnet-5-5")
        let blocks = ChatStore.rebuild([item(1, .text, role: "user", text: "做一下"), notice,
                                        item(3, .text, role: "assistant", text: "API Error: 404 model not available")], settled: nil).blocks
        guard case .turn(let t)? = blocks.first else { Issue.record("no turn: \(blocks)"); return }
        #expect(t.trailing.map(\.id) == [ChatRow.notice(notice).id])
        #expect(!t.process.contains { $0.id == ChatRow.notice(notice).id })
    }

    @Test func theRefusalsFactsInOrder() {
        let r = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5", fallbackModel: "claude-sonnet-5-5",
                             category: "cyber", scope: "session", explanation: "上游拒绝了")
        #expect(ModelFallbackText.refusalFacts(r) == ["Claude Code 从 Opus 5.5 自动改用 Sonnet 5.5", "本会话之后的轮次可能继续用它",
                                                      "类别：cyber", "说明：上游拒绝了"])
        #expect(ModelFallbackText.refusalFacts(ModelRefusal(subtype: "model_refusal")) == ["记录里没有说改用了其他模型"])
        #expect(ModelFallbackText.words(r) == nil)
    }

    @Test func aPersonaConversationSaysTheRefusalInOneSentence() {
        let r = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5", fallbackModel: "claude-sonnet-5-5")
        #expect(ModelFallbackText.plainRefusal(r) == "上游拒绝了这次请求，已改用 Sonnet 5.5。")
        #expect(ModelFallbackText.plainRefusal(ModelRefusal(subtype: "model_refusal")) == "上游拒绝了这次请求")
        #expect(ModelFallbackText.detailsLabel == "详情")
    }

    @Test func aTerminalSessionsCardHasOnlyTheModelStep() throws {
        let s = try session(#","model_fallback":\#(refusal),"turn_error":"API Error: 404","turn_error_code":"model_fallback_unavailable""#)
        #expect(s.turnErrorCode == ModelFallbackText.unavailableCode)
        let card = try #require(ModelFallbackText.card(s, headless: false))
        #expect(card.title == "上游拒绝后，这个会话改用了回退模型" && !card.offersRestore && card.details.isEmpty)
        #expect(card.lines == ["原请求模型：Opus 5.5", "回退模型：Sonnet 5.5", "之后的错误：API Error: 404",
                               "终端里的会话请在 Claude Code 里自己输入 /model 换模型；它可能同时改掉新会话的默认模型。"])
    }

    @Test func aPersonaConversationGoesBackAndThenOnlyWaits() throws {
        // One plain sentence and the actions; the record under Details (#5202).
        let s = try session(#","assistant":true,"model_fallback":\#(refusal),"turn_error":"API Error: 404""#)
        let card = try #require(ModelFallbackText.card(s, headless: true))
        #expect(card.title == "上游拒绝了一次请求，这个对话现在用 Sonnet 5.5 回复。" && card.lines.isEmpty && card.offersRestore)
        #expect(card.details == ["原请求模型：Opus 5.5", "回退模型：Sonnet 5.5", "之后的错误：API Error: 404",
                                 "不会重发任何消息；同样的请求可能再次被拒绝。"])
        let pending = try session(#","assistant":true,"model_fallback":\#(refusal),"model_restore":{"state":"next_start","to":"claude-opus-5-5","at":"2026-11-12T00:00:00Z"}"#)
        let waiting = try #require(ModelFallbackText.card(pending, headless: true))
        #expect(waiting.title == "下次启动将使用配置的模型" && !waiting.offersRestore)
        #expect(waiting.lines == ["配置的模型：Opus 5.5"])
        #expect(waiting.details.contains("要等这个会话的下一次回复，才知道实际用的是哪个模型。"))
        #expect(!waiting.details.contains("配置的模型：Opus 5.5"))
        // No fallback model named, or none at all: no card.
        #expect(ModelFallbackText.card(try session(#","model_fallback":{"subtype":"model_refusal_fallback"}"#), headless: true) == nil)
        #expect(ModelFallbackText.card(try session(""), headless: true) == nil)
    }

    @Test func aRefusedGoingBackIsSaidInWords() {
        func refused(_ code: String) -> String {
            ModelFallbackText.restoreError(APIError(status: 409, message: "hub words", fields: ["code": code]))
        }
        #expect(refused("restore_busy") == "这个对话正在工作，没有关闭任何东西。等它停下再试。")
        #expect(refused("restore_nothing") == "这个会话已经没有在用回退模型了。")
        #expect(refused("node_outdated") == "这台电脑上的 personad 太旧，还不会回到配置的模型，需要更新。")
        #expect(refused("restore_gone") == "hub words")
    }

    // MARK: The node's own visible recovery (#7824)

    @Test func recoveryFieldsAreReadAndOddTypesAreTolerated() throws {
        let json = #","model_fallback":{"subtype":"model_refusal_fallback","original_model":"claude-opus-5-5","fallback_model":"claude-sonnet-5-5","category":"quota_exhausted","scope":"session","recovery_reset":"2026-11-12T00:00:00Z","recovery_exhausted":true,"recovery_returning":true,"recovery_tried":"claude-opus-5-5 → claude-sonnet-5-5"}"#
        let s = try session(json)
        let r = try #require(s.modelFallback)
        let reset = try Date("2026-11-12T00:00:00Z", strategy: .iso8601)
        #expect(r.recoveryExhausted && r.recoveryReturning)
        #expect(r.recoveryReset == reset)
        #expect(r.recoveryTried == "claude-opus-5-5 → claude-sonnet-5-5")
        #expect(r.recoveryCategory == .quota)
        // A runtime-written record carries none of the fields: they read as their zero values and render as before.
        let old = try session(#","model_fallback":\#(refusal)"#)
        let runtime = try #require(old.modelFallback)
        #expect(!runtime.recoveryExhausted && !runtime.recoveryReturning)
        #expect(runtime.recoveryReset == nil && runtime.recoveryTried == nil && runtime.recoveryCategory == nil)
        // Values of an unexpected type never drop the session; the fields stay absent.
        let odd = try session(#","model_fallback":{"subtype":"model_refusal_fallback","recovery_exhausted":"yes","recovery_returning":1,"recovery_reset":123}"#)
        let broken = try #require(odd.modelFallback)
        #expect(!broken.recoveryExhausted && !broken.recoveryReturning && broken.recoveryReset == nil)
    }

    @Test func anExhaustedChainExplainsTheFailureAndKeepsRetry() throws {
        let json = #","model_fallback":{"subtype":"model_refusal_fallback","original_model":"claude-opus-5-5","category":"quota_exhausted","scope":"session","recovery_exhausted":true,"recovery_reset":"2026-11-12T00:00:00Z","recovery_tried":"claude-opus-5-5 → claude-sonnet-5-5"}"#
        let s = try session(json)
        // No fallback model on an exhausted record: no fallback card, but a failure-card explanation.
        #expect(ModelFallbackText.card(s, headless: true) == nil)
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = TimeZone(identifier: "UTC")!
        let now = try #require(s.modelFallback?.recoveryReset)
        let recovery = try #require(ModelFallbackText.exhaustedRecovery(s, now: now, calendar: calendar))
        #expect(recovery.causeLine == "试过的模型都失败了：额度耗尽。")
        let time = Session.usageLimitClock(now, now: now, calendar: calendar)
        #expect(recovery.resetLine == String(localized: "Available again: \(time)"))
        #expect(recovery.triedLine == "试过的模型：claude-opus-5-5 → claude-sonnet-5-5")
        // Without a reset time only the cause line is shown; without tried models nothing folded is added.
        let noReset = try session(#","model_fallback":{"subtype":"model_refusal_fallback","category":"safeguards_refusal","recovery_exhausted":true}"#)
        let plain = try #require(ModelFallbackText.exhaustedRecovery(noReset))
        #expect(plain.resetLine == nil && plain.triedLine == nil)
        #expect(plain.causeLine == "试过的模型都失败了：模型拒绝了。")
        // Retry is offered even though the parsed error itself is non-transient (a refusal), while the session can send.
        #expect(ModelFallbackText.offersRetry(transient: false, session: s))
        #expect(ModelFallbackText.offersRetry(transient: false, session: noReset))
        let readOnly = try session(#","capabilities":{"read":true,"send":false},"model_fallback":{"subtype":"model_refusal_fallback","category":"quota_exhausted","recovery_exhausted":true}"#)
        #expect(!ModelFallbackText.offersRetry(transient: false, session: readOnly))
        // A runtime-written record changes nothing on the failure card: the transient rule alone decides Retry.
        let runtime = try session(#","model_fallback":\#(refusal)"#)
        #expect(ModelFallbackText.exhaustedRecovery(runtime) == nil)
        #expect(ModelFallbackText.exhaustedRecovery(try session("")) == nil)
        #expect(!ModelFallbackText.offersRetry(transient: false, session: runtime))
        #expect(ModelFallbackText.offersRetry(transient: true, session: runtime))
        #expect(ModelFallbackText.offersRetry(transient: nil, session: nil))
    }

    @Test func everyRecoveryCategoryHasPlainWords() throws {
        let cases: [(String, String)] = [
            ("quota_exhausted", "额度耗尽"), ("safeguards_refusal", "模型拒绝了"), ("rate_limited", "被限流了"),
            ("server_error", "服务出错"), ("model_unavailable", "模型不可用")
        ]
        for (category, words) in cases {
            let r = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5",
                                 fallbackModel: "claude-sonnet-5-5", category: category)
            #expect(ModelFallbackText.plainRefusal(r) == "上游拒绝了这次请求，已改用 Sonnet 5.5（\(words)）。")
        }
        // A runtime-written record, or a category this build does not know, keeps the earlier sentence.
        let runtime = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5",
                                   fallbackModel: "claude-sonnet-5-5", category: "cyber")
        #expect(ModelFallbackText.plainRefusal(runtime) == "上游拒绝了这次请求，已改用 Sonnet 5.5。")
        let noCategory = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5",
                                      fallbackModel: "claude-sonnet-5-5")
        #expect(ModelFallbackText.plainRefusal(noCategory) == "上游拒绝了这次请求，已改用 Sonnet 5.5。")
        // The Persona conversation's card names the model that answers now and why (the node's record, as projected).
        let s = try session(#","assistant":true,"model_fallback":{"subtype":"model_refusal_fallback","original_model":"claude-opus-5-5","fallback_model":"claude-sonnet-5-5","category":"quota_exhausted","scope":"session","explanation":"the account's quota was exhausted"}"#)
        let card = try #require(ModelFallbackText.card(s, headless: true))
        #expect(card.title == "上游拒绝了这次请求，已改用 Sonnet 5.5（额度耗尽）。" && card.offersRestore && card.lines.isEmpty)
        // A runtime-written record's card keeps its earlier title.
        let old = try session(#","assistant":true,"model_fallback":\#(refusal)"#)
        #expect(ModelFallbackText.card(old, headless: true)?.title == "上游拒绝了一次请求，这个对话现在用 Sonnet 5.5 回复。")
    }

    @Test func aReturningModelSaysTheNextMessageSwitchesBack() throws {
        let r = ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5",
                             fallbackModel: "claude-sonnet-5-5", recoveryReturning: true)
        #expect(ModelFallbackText.returningLine(r) == "Opus 5.5 已恢复，下一条消息会切回")
        #expect(ModelFallbackText.returningLine(
            ModelRefusal(subtype: ModelRefusal.fallbackSubtype, fallbackModel: "claude-sonnet-5-5", recoveryReturning: true)) == nil)
        #expect(ModelFallbackText.returningLine(
            ModelRefusal(subtype: ModelRefusal.fallbackSubtype, originalModel: "claude-opus-5-5",
                         fallbackModel: "claude-sonnet-5-5")) == nil)
        // The line is a visible line of the Persona conversation's fallback card, not folded under Details.
        let s = try session(#","assistant":true,"model_fallback":{"subtype":"model_refusal_fallback","original_model":"claude-opus-5-5","fallback_model":"claude-sonnet-5-5","recovery_returning":true}"#)
        let card = try #require(ModelFallbackText.card(s, headless: true))
        #expect(card.lines == ["Opus 5.5 已恢复，下一条消息会切回"])
        #expect(!card.details.contains("Opus 5.5 已恢复，下一条消息会切回"))
    }
}
