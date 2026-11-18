package persona.core.domain

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item
import persona.core.contracts.Machine

/** The words for each tool call, as iOS names them (ToolViews.swift `ToolPresentation.verb`, PersonaToolNames.swift). */
class ToolVerbTest {
    private fun verb(name: String?) = toolVerb(name).zh()

    @Test fun claudeCodeToolsKeepTheirWords() {
        assertEquals("运行", verb("Bash"))
        assertEquals("读取", verb("Read"))
        assertEquals("编辑", verb("MultiEdit"))
        assertEquals("查找", verb("LS"))
        assertEquals("提问", verb("AskUserQuestion"))
        assertEquals("监视", verb("Monitor"))
        assertEquals("工具", verb(null))
    }

    @Test fun conversationToolsHaveNames() {
        val names = mapOf(
            "conversations_list" to "查看对话列表",
            "conversation_read" to "读对话",
            "conversation_rename" to "重命名对话",
            "conversation_archive" to "归档对话",
            "conversation_unarchive" to "取消归档",
            "conversation_new" to "新开对话",
            "conversation_send" to "发到对话",
        )
        names.forEach { (name, words) ->
            assertEquals(name, words, verb(name))
            assertEquals(name, words, verb("mcp__persona__$name"))
            // Another server's tool of the same name is that server's (#4339 C1, iOS `ToolNaming.tableName`).
            assertEquals(name, "Other · ${ToolNaming.humanize(name)}", verb("mcp__other__$name"))
        }
    }

    @Test fun computerBrowserPhoneAndMailToolsHaveNames() {
        assertEquals("看电脑", verb("mcp__persona__computer_state"))
        assertEquals("操控电脑", verb("computer_run"))
        assertEquals("操控电脑", verb("mcp__persona__computer_run"))
        assertEquals("用浏览器", verb("mcp__persona__browser_run"))
        assertEquals("看浏览器", verb("browser_look"))
        assertEquals("手机", verb("mcp__persona__phone_calendar_events"))
        assertEquals("手机", verb("phone_reminders_create"))
        assertEquals("邮件", verb("mcp__persona__mail_send"))
        assertEquals("邮件", verb("mail_search"))
    }

    @Test fun personaOwnToolsHaveNames() {
        val names = mapOf(
            "agent_read" to "读 Agent 对话",
            "agent_send" to "给 Agent 发消息", "send_to_agent" to "给 Agent 发消息",
            "agents_list" to "看 Agent 列表", "list_agents" to "看 Agent 列表",
            "agent_start" to "派任务", "task_plan" to "派任务",
            "interaction_answer" to "替你回答",
            "ask_persona" to "交给 Persona",
            "kb_get" to "查知识库", "kb_list" to "查知识库", "kb_search" to "查知识库", "search_knowledge" to "查知识库",
            "kb_update" to "更新知识库",
            "ask_user" to "问你",
            "notify_user" to "通知你",
            "schedule_followup" to "提醒",
            "list_followups" to "看提醒",
            "cancel_followup" to "取消提醒",
            "schedule_routine" to "设定期任务",
            "delete_routine" to "删定期任务",
            "inbox_draft" to "起草回复",
            "persona_settings" to "Persona 设置",
            "view_show" to "画卡片",
            "view_get" to "看卡片",
            "view_update" to "更新卡片",
        )
        names.forEach { (name, words) ->
            assertEquals(name, words, verb("mcp__persona__$name"))
            assertEquals(name, words, verb("mcp__persona__$name"))
            assertEquals(name, words, verb("mcp__plugin_persona__$name"))
        }
        // ask_user outside Persona's server is the question tool's "Ask", as iOS's generic table has it.
        assertEquals("提问", verb("ask_user"))
    }

    @Test fun otherPersonaToolsReadAsACallToPersona() {
        assertEquals("调用 Persona · github read", verb("mcp__persona__github_read"))
        assertEquals("调用 Persona · some new tool", verb("mcp__persona__some_new_tool"))
    }

    // In words, as iOS (#4339 C1): another server's MCP tool as "<Server> · <Tool>", any other name humanized.
    @Test fun unknownToolsReadInWords() {
        assertEquals("GitHub · Create issue", verb("mcp__github__create_issue"))
        assertEquals("GitHub · Agent read", verb("mcp__github__agent_read"))
        assertEquals("Something new", verb("SomethingNew"))
        assertNull(namedToolVerb("mcp__github__create_issue"))
        assertNull(namedToolVerb("SomethingNew"))
    }

    @Test fun serverAndBaseName() {
        assertEquals("persona", toolServer("mcp__persona__agent_read"))
        assertNull(toolServer("mcp__persona"))
        assertNull(toolServer("Bash"))
        assertEquals("agent_read", toolBaseName("mcp__persona__agent_read"))
        assertEquals("Bash", toolBaseName("Bash"))
        assertTrue(isPersonaTool("mcp__plugin_persona__kb_search"))
        assertFalse(isPersonaTool("mcp__persona_extra__kb_search"))
        assertFalse(isPersonaTool("kb_search"))
    }

    // Claude Code's newer tools and the iPhone's, as iOS words them (ToolViews.swift `knownVerb`; iOS's ToolNamingTests
    // `claudeCodeToolsHaveVerbs`, #4339 C1): none of them reads "Send message" or "Cron create" any more.
    @Test fun claudeCodeNewerToolsHaveIOSWords() {
        mapOf(
            "TaskCreate" to "加待办", "TaskUpdate" to "更新待办", "TaskList" to "看待办列表", "TaskGet" to "看待办",
            "TaskStop" to "停止任务", "KillShell" to "停止任务", "KillBash" to "停止任务", "TaskOutput" to "读输出", "BashOutput" to "读输出",
            "SendMessage" to "给 Agent 发消息", "ListAgents" to "看 Agent 列表", "SubagentHandback" to "交回报告",
            "ScheduleWakeup" to "安排下次唤醒", "CronCreate" to "设定期任务", "CronList" to "看定期任务", "CronDelete" to "删定期任务",
            "RemoteTrigger" to "云端定期任务", "LSP" to "查代码", "EnterWorktree" to "进入 worktree", "ExitWorktree" to "离开 worktree",
            "SendUserFile" to "发文件给你", "Artifact" to "发布 Artifact", "PushNotification" to "通知你", "ReportFindings" to "报告审查结果",
            "SendFeedback" to "起草反馈", "EndConversation" to "结束对话", "PowerShell" to "运行", "NotebookRead" to "读取",
            "NotebookEdit" to "编辑", "Skill" to "使用技能", "ToolSearch" to "加载工具", "WaitForMcpServers" to "等 MCP 服务器",
            "ListMcpResourcesTool" to "看 MCP 资源列表", "ReadMcpResourceTool" to "读 MCP 资源", "Workflow" to "运行 workflow",
            "TodoWrite" to "更新待办", "EnterPlanMode" to "进入计划模式", "ExitPlanMode" to "提交计划", "Monitor" to "监视",
            "iphone_status" to "查看 iPhone", "iphone_look" to "看 iPhone", "iphone_do" to "操控 iPhone",
        ).forEach { (name, words) ->
            assertEquals(name, words, verb(name))
            // Persona's wrapper names the same tool by its base name.
            assertEquals(name, words, verb("mcp__persona__$name"))
        }
        // Another server's tool of the same name is that server's.
        assertEquals("Other · Send message", verb("mcp__other__SendMessage"))
    }

    // Three tools say it in English and Japanese too, from the same resources the app reads.
    @Test fun newerToolsReadInEnglishAndJapanese() {
        listOf(
            Triple("SendMessage", "Message agent", "エージェントにメッセージ"),
            Triple("CronCreate", "Schedule task", "定期タスクを設定"),
            Triple("ScheduleWakeup", "Schedule wake-up", "次回の起動を予約"),
            Triple("LSP", "Look up code", "コードを調べる"),
            Triple("iphone_do", "Operate iPhone", "iPhoneを操作"),
        ).forEach { (name, en, ja) ->
            assertEquals(name, en, toolVerb(name).english())
            assertEquals(name, ja, toolVerb(name).ja())
        }
    }

    // What a call did, from its input (iOS `ToolPresentation.verb(_ call:)`; ToolNamingTests `callsSayWhatTheyDid`).
    @Test fun callsSayWhatTheyDid() {
        fun said(name: String, input: String) = callToolVerb(name, Json.parseToJsonElement(input))?.zh()
        assertEquals("完成待办", said("TaskUpdate", """{"taskId":"3","status":"completed"}"""))
        assertEquals("开始待办", said("TaskUpdate", """{"taskId":"3","status":"in_progress"}"""))
        assertEquals("删除待办", said("TaskUpdate", """{"taskId":"3","status":"deleted"}"""))
        assertNull("another status keeps the tool's own words", said("TaskUpdate", """{"taskId":"3","owner":"me"}"""))
        assertEquals("加单元格", said("NotebookEdit", """{"edit_mode":"insert"}"""))
        assertEquals("删单元格", said("NotebookEdit", """{"edit_mode":"delete"}"""))
        assertEquals("改单元格", said("NotebookEdit", "{}"))
        assertEquals("跳到定义", said("LSP", """{"operation":"goToDefinition"}"""))
        assertEquals("找实现", said("LSP", """{"operation":"goToImplementation"}"""))
        assertEquals("找引用", said("LSP", """{"operation":"findReferences"}"""))
        assertEquals("看符号信息", said("LSP", """{"operation":"hover"}"""))
        assertEquals("列出符号", said("LSP", """{"operation":"documentSymbol"}"""))
        assertEquals("搜符号", said("LSP", """{"operation":"workspaceSymbol"}"""))
        listOf("prepareCallHierarchy", "incomingCalls", "outgoingCalls").forEach {
            assertEquals(it, "看调用层级", said("LSP", """{"operation":"$it"}"""))
        }
        assertNull(said("LSP", """{"operation":"somethingNew"}"""))
        assertEquals("停止循环", said("ScheduleWakeup", """{"stop":true}"""))
        assertNull("a flag that is not true", said("ScheduleWakeup", """{"stop":false,"delaySeconds":60}"""))
        assertNull("a string is no flag", said("ScheduleWakeup", """{"stop":"true"}"""))
        assertEquals("移除 worktree", said("ExitWorktree", """{"action":"remove"}"""))
        assertNull(said("ExitWorktree", """{"action":"keep"}"""))
        assertEquals("看 Artifact 列表", said("Artifact", """{"action":"list"}"""))
        assertNull(said("Artifact", """{"action":"publish"}"""))
        assertNull(said("Bash", """{"command":"ls"}"""))
        // Persona's wrapper names the same tool; another server's tool of the same name is that server's.
        assertEquals("完成待办", said("mcp__persona__TaskUpdate", """{"status":"completed"}"""))
        assertNull(said("mcp__other__TaskUpdate", """{"status":"completed"}"""))
        assertNull(callToolVerb("TaskUpdate", null))
    }

    // The subjects that are words of their own (iOS `ToolPresentation.subject`): several findings by their count, a wake-up
    // without a reason by its delay as a work line writes time.
    @Test fun someSubjectsAreWords() {
        val two = """{"findings":[{"file":"a.swift","summary":"Crashes on nil"},{"file":"b.swift","summary":"Leaks"}]}"""
        assertEquals("2 条发现", subject("ReportFindings", two))
        assertEquals("2 findings", toolSubjectText(Item("x", kind = "tool_use", toolName = "ReportFindings", toolInput = Json.parseToJsonElement(two))).english())
        assertEquals("指摘2件", toolSubjectText(Item("x", kind = "tool_use", toolName = "ReportFindings", toolInput = Json.parseToJsonElement(two))).ja())
        assertEquals("Nil crash", subject("ReportFindings", """{"findings":[{"summary":"Crashes","short_summary":"Nil crash"}]}"""))
        assertEquals("", subject("ReportFindings", """{"findings":[]}"""))
        assertEquals("wait for CI", subject("ScheduleWakeup", """{"delaySeconds":300,"reason":"wait for CI"}"""))
        assertEquals("5 分", subject("ScheduleWakeup", """{"delaySeconds":300}"""))
        assertEquals("5 分", subject("ScheduleWakeup", """{"delaySeconds":300,"reason":""}"""))
        assertEquals("5 分", subject("ScheduleWakeup", """{"delaySeconds":"300"}"""))
        assertEquals("45 秒", subject("ScheduleWakeup", """{"delaySeconds":45}"""))
        assertEquals("1 分 20 秒", subject("ScheduleWakeup", """{"delaySeconds":80}"""))
        assertEquals("1 小时 5 分", subject("ScheduleWakeup", """{"delaySeconds":3900}"""))
        assertEquals("0 秒", subject("ScheduleWakeup", """{"delaySeconds":0}"""))
        assertEquals("", subject("ScheduleWakeup", "{}"))
        assertEquals("", subject("ScheduleWakeup", """{"delaySeconds":"soon"}"""))
        assertEquals("5 min", toolSubjectText(Item("x", kind = "tool_use", toolName = "ScheduleWakeup", toolInput = Json.parseToJsonElement("""{"delaySeconds":300}"""))).english())
        assertEquals("5分", toolSubjectText(Item("x", kind = "tool_use", toolName = "ScheduleWakeup", toolInput = Json.parseToJsonElement("""{"delaySeconds":300}"""))).ja())
        // Another server's tool of the same name is that server's: its input is read by the field that says the most.
        assertEquals("", subject("mcp__other__ScheduleWakeup", """{"delaySeconds":300}"""))
    }

    // A computer tool's subject names the Mac as the user knows it, not as the tool wrote it (iOS `ComputerSteps.machineName`).
    @Test fun aComputerToolsSubjectNamesTheMac() {
        val machines = listOf(Machine("macbook", "MacBook"))
        fun named(name: String, input: String) = toolSubjectText(Item("x", kind = "tool_use", toolName = name, toolInput = Json.parseToJsonElement(input)),
            machine = { ComputerSteps.machineName(it, machines) }).zh()
        assertEquals("MacBook · Calculator", named("mcp__persona__computer_state", """{"machine":"macbook","app":"Calculator"}"""))
        assertEquals("MacBook · 整个屏幕", named("mcp__persona__computer_state", """{"machine":"macbook"}"""))
        assertEquals("MacBook", named("mcp__persona__computer_run", """{"machine":"macbook"}"""))
        assertEquals("MacBook", named("mcp__persona__browser_look", """{"machine":"MACBOOK"}"""))
        assertEquals("MacBook · open()", named("mcp__persona__browser_run", """{"machine":"macbook","script":"open()\nnext()"}"""))
        assertEquals("no machine goes by it: as the tool said", "studio · Calculator", named("mcp__persona__computer_state", """{"machine":"studio","app":"Calculator"}"""))
        // Without a name for it the subject is as before.
        assertEquals("macbook · Calculator", subject("mcp__persona__computer_state", """{"machine":"macbook","app":"Calculator"}"""))
    }

    // The iPhone's tools name the phone they act on (iOS: its steps are not read here yet).
    @Test fun theIPhonesToolsNameThePhone() {
        assertEquals("iPhone 16", subject("mcp__persona__iphone_look", """{"phone":"iPhone 16","query":"x"}"""))
        assertEquals("看看设置", subject("mcp__persona__iphone_look", """{"query":"看看设置"}"""))
        assertEquals("iPhone 16", subject("mcp__persona__iphone_do", """{"phone":"iPhone 16","steps":[{"action":"tap"}]}"""))
        assertEquals("", subject("mcp__persona__iphone_status", "{}"))
    }

    // Which tools keep their words as the row's title, with the subject under them (the wrench says nothing of the step).
    @Test fun theRowsThatKeepTheirWords() {
        listOf("SendMessage", "CronCreate", "ScheduleWakeup", "LSP", "ReportFindings", "TaskStop", "iphone_do", "mcp__persona__SendMessage").forEach {
            assertTrue(it, rowKeepsWords(it))
        }
        // The tools the chat's own icons name keep the subject as the title.
        listOf("Read", "Edit", "Bash", "PowerShell", "NotebookRead", "TaskCreate", "TaskUpdate", "TaskGet", "Monitor", "Skill", "mcp__github__SendMessage").forEach {
            assertFalse(it, rowKeepsWords(it))
        }
    }

    private fun subject(name: String, input: String) = toolSubjectText(persona.core.contracts.JsonCodec.decodeFromString<persona.core.contracts.Item>(
        """{"id":"x","kind":"tool_use","tool_name":"$name","tool_input":$input}""")).zh()

    @Test fun namedToolsSayWhatTheyAreAbout() {
        assertEquals("功能 | 登录页", subject("mcp__persona__conversation_rename", """{"kind":"功能","topic":"登录页"}"""))
        assertEquals("第一行", subject("mcp__persona__conversation_send", """{"text":"第一行\n第二行"}"""))
        assertEquals("abcdef12", subject("mcp__persona__conversation_archive", """{"id":"abcdef1234567"}"""))
        assertEquals("macbook · Calculator", subject("mcp__persona__computer_state", """{"machine":"macbook","app":"Calculator"}"""))
        assertEquals("macbook · 整个屏幕", subject("mcp__persona__computer_state", """{"machine":"macbook"}"""))
        assertEquals("整个屏幕", subject("computer_state", "{}"))
        assertEquals("先处理哪项？", subject("mcp__persona__ask_user", """{"questions":[{"question":"先处理哪项？","options":[]}]}"""))
        assertEquals("", subject("mcp__persona__ask_user", "{}"))
        assertEquals("等构建", subject("Monitor", """{"description":"等构建","command":"tail -f log"}"""))
        assertEquals("tail -f log", subject("Monitor", """{"command":"tail -f log\nnext"}"""))
        assertEquals("macbook", subject("mcp__persona__computer_run", """{"machine":"macbook","steps":[]}"""))
        assertEquals("macbook · open()", subject("mcp__persona__browser_run", """{"machine":"macbook","script":"  open()\nnext()"}"""))
        assertEquals("calendar events", subject("mcp__persona__phone_calendar_events", "{}"))
        assertEquals("send", subject("mcp__persona__mail_send", "{}"))
        // Another server's tool: the field that says the most (iOS `ToolNaming.subject`, #4339 C1).
        assertEquals("x", subject("mcp__github__create_issue", """{"title":"x"}"""))
    }
}
