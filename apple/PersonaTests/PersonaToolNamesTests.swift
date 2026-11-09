import Foundation
import Testing
@testable import Persona

/// Persona's own MCP tools (server `persona`) read as Persona in a run of steps, never as the raw tool name (#2494).
@MainActor
struct PersonaToolNamesTests {
    @Test(arguments: [
        // Persona's common tools get a verb.
        ("mcp__persona__agent_read", "读 Agent 对话"),
        ("mcp__persona__agent_send", "给 Agent 发消息"),
        ("mcp__persona__send_to_agent", "给 Agent 发消息"),
        ("mcp__persona__kb_search", "查知识库"),
        ("mcp__persona__kb_update", "更新知识库"),
        ("mcp__persona__agent_start", "派任务"),
        ("mcp__persona__ask_user", "问你"),
        ("mcp__persona__schedule_followup", "提醒"),
        // The rest read like the web.
        ("mcp__persona__github_read", "调用 Persona · github read"),
        ("mcp__persona__imessage_search", "调用 Persona · imessage search"),
        ("mcp__persona__some_new_tool", "调用 Persona · some new tool"),
        // Tools the generic table names keep their verb.
        ("mcp__persona__conversation_read", "读对话"),
        ("mcp__persona__computer_state", "看电脑"),
        ("mcp__persona__browser_look", "看浏览器"),
        ("mcp__persona__phone_calendar_events", "iPhone"),
        ("mcp__persona__mail_send", "邮件"),
        // Other servers and bare names are untouched.
        ("mcp__github__create_issue", "create_issue"),
        ("agent_read", "agent_read"),
        ("ask_user", "提问"),
        ("AskUserQuestion", "提问"),
        ("Read", "读取"),
    ])
    func toolNamesReadAsTheirVerb(name: String, expected: String) {
        #expect(ToolPresentation.verb(name) == expected, "\(name)")
    }

    @Test func serverOfAFullName() {
        #expect(PersonaToolNames.server("mcp__persona__agent_read") == "persona")
        #expect(PersonaToolNames.server("mcp__plugin_x_y__tool") == "plugin_x_y")
        #expect(PersonaToolNames.server("mcp____tool") == nil)
        #expect(PersonaToolNames.server("Read") == nil)
        #expect(PersonaToolNames.servers == ["persona", LegacyNames.product, "plugin_persona", "plugin_" + LegacyNames.product])
        #expect(PersonaToolNames.isPersona("mcp__persona__kb_get"))
        for server in ["other-product", "personas", "personad"] {
            #expect(!PersonaToolNames.isPersona("mcp__\(server)__kb_get"))
        }
        #expect(!PersonaToolNames.isPersona("mcp__cua-driver__click"))
    }

    /// The full name is what everything else matches on; only the words change.
    @Test func baseNameIsUnchanged() {
        #expect(ToolPresentation.baseName("mcp__persona__agent_read") == "agent_read")
        #expect(ToolPresentation.symbol("mcp__persona__agent_read") == "puzzlepiece.extension")
    }
}
