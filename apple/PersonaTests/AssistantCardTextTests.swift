import Foundation
import Testing
@testable import Persona
import PersonaWire

/// The assistant's cards where a reply is read as plain text (#2024): the watch and Siri get the card's words.
struct AssistantCardTextTests {
    private let decision = """
        先说结论：选 B。

        ```persona-view
        {"title":"选哪个方案","fallback":"三个方案：A 便宜、B 最快、C 最稳；我建议 B。",
         "sections":[{"items":[{"type":"row","title":"B","action":{"id":"b","reply":"选 B"}}]}]}
        ```

        有问题再说。
        """

    @Test func aViewIsItsFallback() {
        #expect(AssistantCardText.replacingCards(decision) == """
            先说结论：选 B。

            三个方案：A 便宜、B 最快、C 最稳；我建议 B。

            有问题再说。
            """)
    }

    @Test func withoutFallbackItsTitleOrAMark() {
        let titled = "```persona-view\n{\"title\":\"进度\",\"sections\":[]}\n```"
        #expect(AssistantCardText.replacingCards(titled) == "〔进度〕")
        let bare = "```persona-view\n{\"type\":\"metric\",\"label\":\"步数\",\"value\":\"8,432\"}\n```"
        #expect(AssistantCardText.replacingCards(bare) == AssistantCardText.mark)
        let empty = "```persona-view\n{\"fallback\":\"  \",\"sections\":[]}\n```"
        #expect(AssistantCardText.replacingCards(empty) == AssistantCardText.mark)
    }

    @Test func anOldCardIsItsTitle() {
        let card = "今天：\n```persona-card\n{\"type\":\"stat\",\"title\":\"今天步数\",\"value\":8432,\"unit\":\"步\"}\n```"
        #expect(AssistantCardText.replacingCards(card) == "今天：\n〔今天步数〕")
    }

    @Test func brokenOrStreamingIsAMarkNotJSON() {
        let broken = "```persona-view\n{\"fallback\": \"x\",\n```\n后面"
        #expect(AssistantCardText.replacingCards(broken) == "〔卡片〕\n后面")
        let streaming = "看这个：\n```persona-view\n{\"title\":\"选哪个\",\"sections\":[{\"items\":["
        #expect(AssistantCardText.replacingCards(streaming) == "看这个：\n〔卡片〕")
    }

    @Test func otherCodeStaysAndFencesAreCommonMark() {
        let code = "```json\n{\"fallback\":\"not a card\"}\n```"
        #expect(AssistantCardText.replacingCards(code) == code)
        // A longer fence, a tilde fence, an info string after the language, an indented closing fence.
        let long = "````persona-view title\n{\"fallback\":\"四个反引号\"}\n````"
        #expect(AssistantCardText.replacingCards(long) == "四个反引号")
        let tilde = "~~~persona-view\n{\"fallback\":\"波浪线\"}\n  ~~~"
        #expect(AssistantCardText.replacingCards(tilde) == "波浪线")
        // Not a fence: indented four spaces (an indented code block), or a language that only starts the same.
        let indented = "    ```persona-view\n    {\"fallback\":\"x\"}\n    ```"
        #expect(AssistantCardText.replacingCards(indented) == indented)
        let other = "```persona-viewer\n{\"fallback\":\"x\"}\n```"
        #expect(AssistantCardText.replacingCards(other) == other)
    }

    @Test func exampleCardsInsideOtherFencesStayCode() {
        let example = "````markdown\n示例：\n```persona-view\n{\"fallback\":\"这是示例，不是卡片\"}\n```\n````"
        let live = "```persona-view\n{\"fallback\":\"实际卡片\"}\n```"
        #expect(AssistantCardText.replacingCards(example) == example)
        #expect(AssistantCardText.replacingCards(example + "\n" + live) == example + "\n实际卡片")
        // An unfinished ordinary code block still owns every line after its opening fence.
        let streamingExample = "````markdown\n```persona-view\n{\"fallback\":\"示例\"}\n```"
        #expect(AssistantCardText.replacingCards(streamingExample) == streamingExample)
    }

    @Test func lineEndingsAndLanguageCase() {
        for ending in ["\n", "\r\n", "\r"] {
            for language in ["persona-view", "PERSONA-VIEW", "Persona-View"] {
                let input = ["前文", "```" + language, "{\"fallback\":\"进度 50%\"}", "```", "后文", ""].joined(separator: ending)
                let expected = ["前文", "进度 50%", "后文", ""].joined(separator: ending)
                #expect(AssistantCardText.replacingCards(input) == expected)
                #expect(!WatchText.shown(input).contains("fallback"))
            }
        }
    }

    @Test func shorterOrMixedClosingFencesDoNotFinishACard() {
        let shorter = "````persona-view\n{\"fallback\":\"未闭合\"}\n```"
        let mixed = "~~~persona-view\n{\"fallback\":\"未闭合\"}\n```"
        #expect(AssistantCardText.replacingCards(shorter) == AssistantCardText.mark)
        #expect(AssistantCardText.replacingCards(mixed) == AssistantCardText.mark)
    }

    @Test func twoCardsInOneReply() {
        let two = "```persona-view\n{\"fallback\":\"一\"}\n```\n中间\n```persona-view\n{\"fallback\":\"二\"}\n```"
        #expect(AssistantCardText.replacingCards(two) == "一\n中间\n二")
    }

    @Test func theWatchAndSiriReadTheWords() {
        let watch = WatchText.shown(decision)
        #expect(watch.contains("我建议 B"))
        #expect(!watch.contains("sections") && !watch.contains("{"))
        let siri = AskPersonaText.spoken(decision)
        #expect(siri.contains("我建议 B") && !siri.contains("sections"))
    }
}
