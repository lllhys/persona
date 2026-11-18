package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem

/** A step of Persona's own, the computer's, the phone's and mail tools reads as iOS names it (#2753). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class ToolPresentationTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources

    private fun tool(name: String, vararg fields: Pair<String, String>) = Item("call", kind = "tool_use", toolName = name,
        toolInput = buildJsonObject { fields.forEach { (key, value) -> put(key, value) } })

    // The whole screen follows the app's language, not iOS's hard-coded Chinese.
    @Test @Config(qualifiers = "ja") fun wholeScreenIsInTheAppsLanguage() {
        assertEquals("macbook · 画面全体", toolPresentation(res, tool("mcp__persona__computer_state", "machine" to "macbook")).title)
    }

    @Test fun namedToolsShowTheirWordsNotTheirRawName() {
        toolPresentation(res, tool("mcp__persona__computer_run", "machine" to "macbook")).let {
            assertEquals("操控电脑", it.verb)
            assertEquals("macbook", it.title)
        }
        toolPresentation(res, tool("mcp__persona__conversation_rename", "kind" to "功能", "topic" to "登录页")).let {
            assertEquals("重命名对话", it.verb)
            assertEquals("功能 | 登录页", it.title)
        }
        // Nothing to say about it: the words are the title too.
        assertEquals("归档对话", toolPresentation(res, tool("mcp__persona__conversation_archive")).title)
        assertEquals("用浏览器", toolPresentation(res, tool("mcp__persona__browser_run")).verb)
        assertEquals("看电脑", toolPresentation(res, tool("computer_state")).verb)
        assertEquals("macbook · 整个屏幕", toolPresentation(res, tool("mcp__persona__computer_state", "machine" to "macbook")).title)
        assertEquals("手机", toolPresentation(res, tool("mcp__persona__phone_device_info")).verb)
        assertEquals("device info", toolPresentation(res, tool("mcp__persona__phone_device_info")).title)
        assertEquals("邮件", toolPresentation(res, tool("mcp__persona__mail_send")).verb)
        assertEquals("读 Agent 对话", toolPresentation(res, tool("mcp__persona__agent_read")).verb)
        assertEquals("调用 Persona · github read", toolPresentation(res, tool("mcp__persona__github_read")).verb)
    }

    // Another server's tool reads "<Server> · <Tool>", never "Use" (#4339 C1, iOS `ToolNaming`).
    @Test fun claudeCodeToolsKeepTheirWordsAndUnknownOnesReadInWords() {
        assertEquals("读取", toolPresentation(res, tool("Read", "file_path" to "/w/a.kt")).verb)
        toolPresentation(res, tool("mcp__github__create_issue")).let {
            assertEquals("GitHub · Create issue", it.verb)
            assertEquals("GitHub · Create issue", it.title)
        }
        assertEquals("Share onboarding guide", toolPresentation(res, tool("ShareOnboardingGuide")).verb)
    }

    private fun json(name: String, input: String) = Item("call", kind = "tool_use", toolName = name, toolInput = Json.parseToJsonElement(input))

    // Claude Code's newer tools read as iOS words them, never "Send message" or "Cron create" (#4339 C1). Their words stay the
    // row's title (the wrench says nothing of the step) and what the call is about goes under them.
    @Test fun newerToolsKeepIOSWordsAsTheirTitleWithTheSubjectUnderIt() {
        toolPresentation(res, json("SendMessage", """{"to":"researcher","summary":"assign task 1"}""")).let {
            assertEquals("给 Agent 发消息", it.title)
            assertEquals("给 Agent 发消息", it.verb)
            assertEquals("researcher · assign task 1", it.detail)
        }
        toolPresentation(res, json("CronCreate", """{"cron":"7 * * * *","prompt":"check CI\nthen report"}""")).let {
            assertEquals("设定期任务", it.title)
            assertEquals("7 * * * * · check CI", it.detail)
        }
        toolPresentation(res, json("LSP", """{"operation":"findReferences","filePath":"/a/ToolViews.swift","line":120}""")).let {
            assertEquals("找引用", it.title)
            assertEquals("ToolViews.swift:120", it.detail)
        }
        // Several findings by their count, a wake-up without a reason by its delay (iOS `ToolPresentation.subject`).
        toolPresentation(res, json("ReportFindings", """{"findings":[{"file":"a.swift","summary":"x"},{"file":"b.swift","summary":"y"}]}""")).let {
            assertEquals("报告审查结果", it.title)
            assertEquals("2 条发现", it.detail)
        }
        toolPresentation(res, json("ScheduleWakeup", """{"delaySeconds":300}""")).let {
            assertEquals("安排下次唤醒", it.title)
            assertEquals("5 分", it.detail)
        }
        toolPresentation(res, json("ScheduleWakeup", """{"delaySeconds":300,"reason":"wait for CI"}""")).detail.let { assertEquals("wait for CI", it) }
        // What the call did, from its input.
        assertEquals("停止循环", toolPresentation(res, json("ScheduleWakeup", """{"stop":true}""")).title)
        assertEquals("移除 worktree", toolPresentation(res, json("ExitWorktree", """{"action":"remove"}""")).title)
        toolPresentation(res, json("TaskStop", """{"task_id":"b7f2"}""")).let {
            assertEquals("停止任务", it.title)
            assertEquals("b7f2", it.detail)
        }
        // A tool with nothing to name is its words alone.
        toolPresentation(res, json("ListAgents", "{}")).let {
            assertEquals("看 Agent 列表", it.title)
            assertNull(it.detail)
        }
        // The iPhone's tools name the phone.
        toolPresentation(res, json("mcp__persona__iphone_do", """{"phone":"iPhone 16","steps":[]}""")).let {
            assertEquals("操控 iPhone", it.title)
            assertEquals("iPhone 16", it.detail)
        }
    }

    // The task list and the other tools the chat's own icons name keep the subject as the title, with iOS's verb.
    @Test fun theTaskListAndShellsSayIOSWords() {
        toolPresentation(res, json("TaskCreate", """{"subject":"Write tests","description":"…"}""")).let {
            assertEquals("Write tests", it.title)
            assertEquals("加待办", it.verb)
            assertTrue(it.named)
        }
        toolPresentation(res, json("TaskUpdate", """{"taskId":"3","status":"completed"}""")).let {
            assertEquals("#3", it.title)
            assertEquals("完成待办", it.verb)
        }
        assertEquals("更新待办", toolPresentation(res, json("TaskUpdate", """{"taskId":"3","owner":"me"}""")).verb)
        toolPresentation(res, json("TaskList", "{}")).let { assertEquals("看待办列表", it.title); assertEquals("看待办列表", it.verb) }
        toolPresentation(res, json("TaskGet", """{"taskId":"3"}""")).let { assertEquals("#3", it.title); assertEquals("看待办", it.verb) }
        toolPresentation(res, json("PowerShell", """{"command":"Get-ChildItem\nSort-Object"}""")).let {
            assertEquals("Get-ChildItem", it.title)
            assertEquals("运行", it.verb)
        }
        toolPresentation(res, json("NotebookEdit", """{"notebook_path":"/n/analysis.ipynb","edit_mode":"insert"}""")).let {
            assertEquals("analysis.ipynb", it.title)
            assertEquals("加单元格", it.verb)
        }
        assertEquals("读取", toolPresentation(res, json("NotebookRead", """{"notebook_path":"/n/analysis.ipynb"}""")).verb)
        // The tools it already had keep their verbs.
        assertEquals("读取", toolPresentation(res, tool("Read", "file_path" to "/w/a.kt")).verb)
    }

    // On Glass the to-do list's tools keep iOS's words as the title with the subject under them: "#3" alone says nothing of
    // whether the to-do was started, completed or deleted, and Glass's rows draw no verb.
    @Test fun onGlassTheTaskListKeepsItsWordsAsTheTitle() {
        val glass = ToolNames(glass = true)
        toolPresentation(res, json("TaskCreate", """{"subject":"Write tests"}"""), glass).let {
            assertEquals("加待办", it.title)
            assertEquals("Write tests", it.detail)
            assertEquals("加待办 Write tests", it.running(res))
        }
        toolPresentation(res, json("TaskUpdate", """{"taskId":"3","status":"completed"}"""), glass).let {
            assertEquals("完成待办", it.title)
            assertEquals("#3", it.detail)
        }
        toolPresentation(res, json("TaskGet", """{"taskId":"3"}"""), glass).let { assertEquals("看待办", it.title); assertEquals("#3", it.detail) }
        toolPresentation(res, json("TaskList", "{}"), glass).let { assertEquals("看待办列表", it.title); assertNull(it.detail) }
        // Another server's tool of the same name is that server's.
        assertEquals("Other · Task create", toolPresentation(res, json("mcp__other__TaskCreate", "{}"), glass).title)
        // The shells and notebook reads keep the subject as the title under their own icons.
        assertEquals("Get-ChildItem", toolPresentation(res, json("PowerShell", """{"command":"Get-ChildItem"}"""), glass).title)
    }

    @Test fun theRunningLineSaysTheNewerToolsWords() {
        fun running(item: Item) = workSummary(res, ChatLine.Work("w", listOf(WorkItem.Tool(ToolCall(item.asLineItem(), null, emptyList()))),
            live = true, start = null, end = null, open = false), null)
        assertEquals("给 Agent 发消息 researcher · assign task 1", running(json("SendMessage", """{"to":"researcher","summary":"assign task 1"}""")))
        assertEquals("安排下次唤醒 5 分", running(json("ScheduleWakeup", """{"delaySeconds":300}""")))
        assertEquals("看 Agent 列表", running(json("ListAgents", "{}")))
        // The task list is bookkeeping: the running line never names it (iOS `WorkNow.quiet`), but its words are iOS's.
        assertEquals("加待办 Write tests", toolPresentation(res, json("TaskCreate", """{"subject":"Write tests"}""")).running(res))
        assertEquals("完成待办 #3", toolPresentation(res, json("TaskUpdate", """{"taskId":"3","status":"completed"}""")).running(res))
        assertEquals("正在工作 · 工具 1", running(json("TaskCreate", """{"subject":"Write tests"}""")))
    }

    // A computer tool's row names the Mac as the user knows it (iOS `ComputerSteps.machineName`, #4339 C9 and C18).
    @Test fun aComputerToolNamesTheMacAsTheUserKnowsIt() {
        val names = ToolNames(listOf(Machine("macbook", "MacBook")), "server")
        assertEquals("MacBook · 整个屏幕", toolPresentation(res, tool("mcp__persona__computer_state", "machine" to "macbook"), names).title)
        assertEquals("macbook · 整个屏幕", toolPresentation(res, tool("mcp__persona__computer_state", "machine" to "macbook")).title)
        assertEquals("MacBook", toolPresentation(res, tool("mcp__persona__computer_run", "machine" to "macbook"), names).title)
        assertEquals("MacBook", toolPresentation(res, tool("mcp__persona__browser_look", "machine" to "macbook"), names).title)
        toolPresentation(res, json("mcp__persona__computer_run", """{"machine":"macbook","steps":[{"click":{"name":"All Clear"}},{"click":{"name":"Multiply"}}]}"""), names).let {
            assertEquals("MacBook · 点 AC → ×", it.title)
            assertEquals("操控电脑", it.verb)
        }
        // An agent's own Cua Driver call is another server's tool, as on iOS: its words, and what its input says the most.
        toolPresentation(res, json("mcp__cua-driver__run_steps", """{"steps":[{"click":{"name":"All Clear"}}]}"""), names).let {
            assertEquals("Cua Driver · Run steps", it.title)
            assertNull(it.detail)
        }
        assertEquals("Cua Driver · Click", toolPresentation(res, json("mcp__cua-driver__click", """{"pid":1,"element_token":"s1:5"}"""), names).title)
    }

    // On Glass Persona's own tools that no case reads the subject of draw one under their words (iOS picks one for them), the
    // words kept as the title; Brief and Board keep the words alone.
    @Test fun personaToolsDrawASubjectUnderTheirWordsOnGlass() {
        val search = json("mcp__persona__kb_search", """{"query":"release notes"}""")
        toolPresentation(res, search).let {
            assertEquals("查知识库", it.title)
            assertNull(it.detail)
        }
        val glass = ToolNames(glass = true)
        toolPresentation(res, search, glass).let {
            assertEquals("查知识库", it.title)
            assertEquals("查知识库", it.verb)
            assertEquals("release notes", it.detail)
            assertEquals("查知识库 release notes", it.running(res))
        }
        // No field to pick: the words alone.
        toolPresentation(res, json("mcp__persona__agents_list", "{}"), glass).let { assertEquals("看 Agent 列表", it.title); assertNull(it.detail) }
        // A tool a case reads the subject of keeps it as its title, in every style.
        toolPresentation(res, tool("mcp__persona__conversation_rename", "kind" to "功能", "topic" to "登录页"), glass).let {
            assertEquals("功能 | 登录页", it.title)
            assertEquals("重命名对话", it.verb)
        }
        toolPresentation(res, tool("mcp__persona__computer_run", "machine" to "macbook"), glass).let { assertEquals("macbook", it.title) }
        // Not a Persona tool: as before.
        assertFalse(toolPresentation(res, json("mcp__github__search", """{"query":"x"}"""), glass).title == "x")
    }

    @Test fun theNewerToolsHaveTheirIcons() {
        assertEquals(PhIcons.TerminalWindow, toolCategory("PowerShell"))
        assertEquals(PhIcons.Eye, toolCategory("NotebookRead"))
        assertEquals(PhIcons.ListChecks, toolCategory("TaskGet"))
    }

    @Test fun theRunningLineSaysTheNamedToolsWords() {
        fun running(item: Item) = workSummary(res, ChatLine.Work("w", listOf(WorkItem.Tool(ToolCall(item.asLineItem(), null, emptyList()))),
            live = true, start = null, end = null, open = false), null)
        // The step it is on, as iOS's running line (#4339 C5); with no clock yet, no time.
        assertEquals("操控电脑 macbook", running(tool("mcp__persona__computer_run", "machine" to "macbook")))
        assertEquals("归档对话", running(tool("mcp__persona__conversation_archive")))
        assertEquals("正在读取 a.kt", running(tool("Read", "file_path" to "/w/a.kt")))
        assertEquals("GitHub · Create issue", running(tool("mcp__github__create_issue")))
    }

    @Test fun namedToolsHaveTheirIcons() {
        assertEquals(PhIcons.Desktop, toolCategory("mcp__persona__computer_run"))
        assertEquals(PhIcons.GlobeSimple, toolCategory("mcp__persona__browser_look"))
        assertEquals(PhIcons.Archive, toolCategory("mcp__persona__conversation_unarchive"))
        assertEquals(PhIcons.ChatCircle, toolCategory("mcp__persona__conversation_send"))
        assertEquals(PhIcons.DeviceMobile, toolCategory("mcp__persona__phone_photos_recent"))
        assertEquals(PhIcons.EnvelopeSimple, toolCategory("mcp__persona__mail_send"))
        assertEquals(PhIcons.Eye, toolCategory("Read"))
        assertEquals(PhIcons.Wrench, toolCategory("mcp__github__create_issue"))
    }

    // As iOS's `WorkNow` (#4339 C5): the newest running call and "+N" for those beside it; several reads as one; a
    // thought by its first line; bookkeeping tools never named; no step yet: "Working" and its time.
    @Test fun theRunningLineNamesItsStepAsIOS() {
        val start = java.time.Instant.parse("2026-11-12T10:00:00Z")
        fun call(id: String, name: String, vararg fields: Pair<String, String>) = WorkItem.Tool(ToolCall(
            Item(id, kind = "tool_use", toolName = name, toolInput = buildJsonObject { fields.forEach { (k, v) -> put(k, v) } }).asLineItem(), null, emptyList()))
        fun line(vararg steps: WorkItem) = workSummary(res, ChatLine.Work("w", steps.toList(), live = true, start = start, end = null, open = false),
            null, start.plusSeconds(39))
        assertEquals("正在读取 a.kt +2 · 39 秒", line(call("1", "Grep", "pattern" to "x"), call("2", "Bash", "command" to "ls"), call("3", "Read", "file_path" to "/w/a.kt")))
        assertEquals("正在读取 2 个文件 · 39 秒", line(call("1", "Read", "file_path" to "/w/a.kt"), call("2", "Read", "file_path" to "/w/b.kt")))
        assertEquals("先看看工作行 · 39 秒", line(WorkItem.Thought(Item("t", kind = "thinking", text = "\n先看看工作行\n再画").asLineItem())))
        assertEquals("正在读取 a.kt · 39 秒", line(call("1", "Read", "file_path" to "/w/a.kt"), call("2", "TodoWrite")))
        assertEquals("正在工作 39 秒", line())
    }
}
