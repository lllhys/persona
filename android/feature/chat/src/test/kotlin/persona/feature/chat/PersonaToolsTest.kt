package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Item
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem

/** Persona's own tools in words (iOS PersonaToolNames, ComputerSteps), ask_user's answers, the plan pill's anchor. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "zh")
class PersonaToolsTest {
    private val res = ApplicationProvider.getApplicationContext<Context>().resources
    private fun item(name: String, input: kotlinx.serialization.json.JsonObject = buildJsonObject { }) =
        Item("u", kind = "tool_use", role = "assistant", toolName = name, toolInput = input)

    @Test fun askUserIsOnlyPersonasOwn() {
        assertTrue(PersonaTools.isAskUser("mcp__persona__ask_user"))
        assertTrue(!PersonaTools.isAskUser("mcp__github__ask_user"))
        assertEquals(persona.core.designsystem.icons.PhIcons.Question, toolCategory("mcp__persona__ask_user"))
        assertEquals(persona.core.designsystem.icons.PhIcons.BookOpen, toolCategory("mcp__persona__kb_search"))
    }

    @Test fun computerStepsReadAsOneSentence() {
        val run = item("mcp__persona__computer_run", buildJsonObject {
            put("machine", "MacBook")
            put("steps", buildJsonArray {
                listOf("All Clear", "6", "Multiply").forEach { add(buildJsonObject { put("click", buildJsonObject { put("name", it) }) }) }
                add(buildJsonObject { put("tool", "type_text"); put("args", buildJsonObject { put("text", "你好") }) })
                add(buildJsonObject { put("tool", "hotkey"); put("args", buildJsonObject { put("keys", buildJsonArray { add("cmd"); add("s") }) }) })
            })
        })
        assertEquals("操控电脑", toolPresentation(res, run).verb)
        assertEquals("MacBook · 点 AC → 6 → × → 输入「你好」 → 按 ⌘S", toolPresentation(res, run).title)
        assertEquals("运行脚本：tell app", toolPresentation(res, item("mcp__persona__computer_run", buildJsonObject { put("script", "\ntell app\nend") })).title)
        // Nothing it does in words: the machine alone, as before.
        assertEquals("MacBook", toolPresentation(res, item("mcp__persona__computer_run", buildJsonObject { put("machine", "MacBook") })).title)
    }

    @Test fun askUserTicksWhatWasPicked() {
        val questions = listOf(AskUserQuestion("哪天？", listOf("周五", "周一"), false), AskUserQuestion("通知谁？", listOf("产品", "测试"), true))
        val answers = AskUserAnswers.parse("The user answered:\n- 哪天？ → 周一\n- 通知谁？ → 产品, 测试", questions)
        assertEquals(mapOf("哪天？" to "周一", "通知谁？" to "产品, 测试"), answers)
        assertTrue(AskUserAnswers.picked("测试", answers["通知谁？"], multiSelect = true))
        assertTrue(!AskUserAnswers.picked("产品, 测试", "产品", multiSelect = true))
        assertEquals("下周再说", AskUserAnswers.typed(questions[0], "下周再说"))
        assertNull(AskUserAnswers.typed(questions[0], "周五"))
        assertEquals(emptyMap<String, String>(), AskUserAnswers.parse("No answer: the request expired", questions))
    }

    @Test fun thePillPointsAtTheNewestDrawnPlan() {
        fun todos(id: String) = WorkItem.Tool(ToolCall(Item(id, kind = "tool_use", role = "assistant", toolName = "TodoWrite",
            toolInput = buildJsonObject { put("todos", buildJsonArray {
                add(buildJsonObject { put("content", "一"); put("status", "completed") })
                add(buildJsonObject { put("content", "二"); put("status", "in_progress") })
            }) }).asLineItem(), null, emptyList()))
        val older = todos("p1")
        val newer = todos("p2")
        val plans = mapOf("w1" to latestPlan(listOf(older)), "w2" to latestPlan(listOf(newer)))
        val lines = listOf<ChatLine>(ChatLine.Step("w2", newer, true), ChatLine.Step("w1", older, true)) // newest first
        assertEquals("w2/p2", latestPlanLine(lines, plans)?.lineId)
        val live = ChatLine.Work("w3", listOf(todos("p3")), live = true, start = null, end = null, open = false)
        // Neither conversation type previews a collapsed plan; only a drawn expanded step has an anchor.
        assertEquals("w2/p2", latestPlanLine(listOf(live) + lines, plans)?.lineId)
        assertNull(latestPlanLine(listOf(live), emptyMap()))
        assertNull(latestPlanLine(emptyList(), emptyMap()))
    }
}
