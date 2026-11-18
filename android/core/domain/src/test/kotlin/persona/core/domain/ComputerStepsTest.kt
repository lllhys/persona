package persona.core.domain

import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item
import persona.core.contracts.Machine
import java.time.Instant

/**
 * A tool that operates a Mac in words, and the chat's latest such call for its pill: iOS `ComputerTests`'s cases
 * (`stepsReadAsWords`, `theLatestOperationIsRunningUntilItsResult`, `aToolThatOperatesAMacShowsItsScreen`,
 * `theToolsReadInChinese`), in zh-Hans as iOS checks them, and in English and Japanese.
 */
class ComputerStepsTest {
    private fun click(name: String) = buildJsonObject {
        put("click", buildJsonObject { put("app", "Calculator"); put("role", "button"); put("name", name) })
    }

    private val mixed = buildJsonObject {
        put("steps", buildJsonArray {
            add(click("Clear"))
            add(buildJsonObject { put("type_text", buildJsonObject { put("text", "你好") }) })
            add(buildJsonObject { put("tool", "press_key"); put("args", buildJsonObject { put("key", "return") }) })
            add(buildJsonObject { put("hotkey", buildJsonObject { put("keys", buildJsonArray { add("cmd"); add("s") }) }) })
            add(buildJsonObject { put("launch_app", buildJsonObject { put("name", "Safari") }) })
            add(buildJsonObject { put("expect", buildJsonObject { }) })
            add(click("Equals"))
            add(click("Done"))
        })
    }

    private fun input(vararg pairs: Pair<String, Any>): JsonObject = buildJsonObject {
        pairs.forEach { (k, v) ->
            when (v) {
                is String -> put(k, v)
                is JsonElement -> put(k, v)
                else -> error("unsupported $v")
            }
        }
    }

    private fun describe(name: String, input: JsonObject = JsonObject(emptyMap())) = ComputerSteps.describe(name, input)

    @Test fun stepsReadAsWords() {
        assertEquals("点 C → 输入「你好」 → 按 ⏎ → 按 ⌘S → 打开 Safari → 检查 → 点 = → Done",
            describe("mcp__persona__computer_run", mixed).zh())
        assertEquals("看 Calculator", describe("mcp__persona__computer_state", input("app" to "Calculator")).zh())
        assertEquals("看整个屏幕", describe("mcp__persona__computer_state").zh())
        assertEquals("点 一处", describe("mcp__cua-driver__click", input("element_token" to "s1:5")).zh())
        assertEquals("输入「" + "长".repeat(23) + "…」", describe("mcp__cua-driver__type_text", input("text" to "长".repeat(40))).zh())
        assertTrue(ComputerSteps.isComputerTool("mcp__cua-driver__run_steps"))
        assertFalse(ComputerSteps.isComputerTool("Bash"))
    }

    @Test fun stepsReadInEnglishAndJapanese() {
        val run = describe("mcp__persona__computer_run", mixed)
        assertEquals("Click C → Type “你好” → Press ⏎ → Press ⌘S → Open Safari → Check → Click = → Done", run.english())
        assertEquals("クリック C → 「你好」を入力 → ⏎を押す → ⌘Sを押す → Safariを開く → 確認 → クリック = → Done", run.ja())
        assertEquals("Look at the whole screen", describe("computer_state").english())
        assertEquals("画面全体を見る", describe("computer_state").ja())
        val space = describe("mcp__cua-driver__hotkey", input("keys" to buildJsonArray { add("shift"); add("space") }))
        assertEquals("Press ⇧Space", space.english())
        assertEquals("⇧スペースを押す", space.ja())
    }

    @Test fun eachActionHasItsWords() {
        fun action(tool: String, vararg args: Pair<String, Any>) = describe("mcp__cua-driver__$tool", input(*args))
        assertEquals("双击 Notes", action("double_click", "label" to "Notes").zh())
        assertEquals("右键 一处", action("right_click").zh())
        assertEquals("填「me@example.com」", action("set_value", "value" to "me@example.com").zh())
        assertEquals("滚动", action("scroll").zh())
        assertEquals("拖动", action("drag").zh())
        assertEquals("打开 com.apple.Notes", action("launch_app", "bundle_id" to "com.apple.Notes").zh())
        assertEquals("切到 Notes", action("bring_to_front", "app" to "Notes").zh())
        assertEquals("切到前台", action("bring_to_front").zh())
        assertEquals("看窗口", action("get_window_state").zh())
        assertEquals("截屏", action("screenshot").zh())
        assertEquals("截屏", action("get_desktop_state").zh())
        assertEquals("看屏幕上有什么", action("list_apps").zh())
        assertEquals("放大看", action("zoom").zh())
        assertEquals("运行脚本", action("run-script").zh())
        // A Cua tool this does not know reads as its own name.
        assertEquals("move_cursor", action("move-cursor").zh())
        // Cua's run_steps: the steps, clicks after a click by their target alone.
        assertEquals("点 AC → 6 → ×", describe("mcp__cua-driver__run_steps", input("steps" to buildJsonArray {
            listOf("All Clear", "6", "Multiply").forEach { add(click(it)) }
        })).zh())
        // computer_run with neither steps nor a script: its verb.
        assertEquals("操控电脑", describe("computer_run", input("machine" to "macbook")).zh())
    }

    /** A computer_run row's subject: the machine and what it does; nothing more said, the row keeps its own. */
    @Test fun aRunsRowSaysWhatItDoes() {
        val steps = input("machine" to "MacBook", "steps" to buildJsonArray {
            listOf("All Clear", "6", "Multiply", "7", "Equals").forEach { add(click(it)) }
        })
        assertEquals("MacBook · 点 AC → 6 → × → 7 → =", ComputerSteps.runSubject("mcp__persona__computer_run", steps)?.zh())
        val script = input("machine" to "MacBook", "script" to "const app = await cua.getApp(\"Calculator\");\nreturn 1")
        assertEquals("MacBook · 运行脚本：const app = await cua.getApp(\"Calculator\");",
            ComputerSteps.runSubject("mcp__persona__computer_run", script)?.zh())
        assertNull(ComputerSteps.runSubject("mcp__persona__computer_run", input("machine" to "MacBook")))
        assertNull(ComputerSteps.runSubject("mcp__persona__computer_state", input("machine" to "MacBook")))
    }

    private val mac = Machine("macbook", "MacBook", online = true)
    private val work = Machine("macbook~g0000beef", "MacBook", online = true)

    /** The Mac a tool's `machine` names, as the user knows it (iOS `ComputerSteps.machineName`, #4339 C9 and C18). */
    @Test fun aMacIsNamedAsTheUserKnowsIt() {
        val machines = listOf(mac, work)
        assertEquals("by id", "MacBook", ComputerSteps.machineName("macbook", machines))
        assertEquals("by name, whatever the case", "MacBook", ComputerSteps.machineName("MACBOOK", machines))
        assertEquals("no machine goes by it: as the tool said", "studio", ComputerSteps.machineName("studio", machines))
        assertEquals("no machines at all", "macbook", ComputerSteps.machineName("macbook", emptyList()))
        // The chat's own group of hubs first, then any.
        val renamed = Machine("macbook~g0000beef", "Work MacBook")
        assertEquals("Work MacBook", ComputerSteps.machineName("macbook", listOf(mac, renamed), "server~g0000beef"))
        assertEquals("MacBook", ComputerSteps.machineName("macbook", listOf(mac, renamed), "server"))
        assertEquals("MacBook", ComputerSteps.machineName("macbook", listOf(mac), "server~g0000beef"))
        // A Mac the hub gave no name goes by what the tool said.
        assertEquals("macbook", ComputerSteps.machineName("macbook", listOf(Machine("macbook", ""))))
    }

    @Test fun aRunsRowNamesTheMacByItsName() {
        val steps = input("machine" to "macbook", "steps" to buildJsonArray { listOf("All Clear", "6").forEach { add(click(it)) } })
        val machines = listOf(mac)
        assertEquals("macbook · 点 AC → 6", ComputerSteps.runSubject("mcp__persona__computer_run", steps)?.zh())
        assertEquals("MacBook · 点 AC → 6", ComputerSteps.runSubject("mcp__persona__computer_run", steps) { ComputerSteps.machineName(it, machines) }?.zh())
        assertNull(ComputerSteps.runSubject("mcp__persona__computer_run", input("machine" to "macbook")) { ComputerSteps.machineName(it, machines) })
    }

    @Test fun theMacACallOperates() {
        val machines = listOf(mac, work)
        val named = input("machine" to "MacBook")
        assertEquals("the assistant's: the Mac it names, by name too", mac.id,
            ComputerActivity.machine("mcp__persona__computer_run", named, "server", machines))
        assertEquals("in the chat's group of hubs", work.id,
            ComputerActivity.machine("mcp__persona__computer_state", input("machine" to "macbook"), "server~g0000beef", machines))
        assertEquals("an agent's own Cua Driver: its machine", mac.id,
            ComputerActivity.machine("mcp__cua-driver__click", null, mac.id, machines))
        assertNull(ComputerActivity.machine("Bash", named, mac.id, machines))
        assertNull(ComputerActivity.machine("mcp__persona__computer_run", JsonObject(emptyMap()), mac.id, machines))
        assertNull("a Mac no list has", ComputerActivity.machine("mcp__persona__computer_run", input("machine" to "studio"), "server", machines))
    }

    private val t0 = Instant.parse("2026-11-12T10:00:00Z")
    private fun item(id: String, kind: String, at: Instant, use: String, name: String? = null, input: JsonObject? = null) =
        Item(id, 1, ts = at.toString(), role = if (kind == "tool_use") "assistant" else "user", kind = kind, toolUseId = use,
            toolName = name, toolInput = input)

    @Test fun theLatestOperationIsRunningUntilItsResult() {
        val use = item("1", "tool_use", t0, "t1", "mcp__persona__computer_run",
            input("machine" to "MacBook", "steps" to buildJsonArray { add(click("All Clear")) }))
        val bash = item("2", "tool_use", t0, "t2", "Bash", JsonObject(emptyMap()))
        val running = requireNotNull(ComputerActivity.latest(listOf(use, bash), "server", listOf(mac)))
        assertEquals(mac.id, running.machineId)
        assertEquals("MacBook", running.machineName)
        assertEquals("点 AC", running.step.zh())
        assertTrue(running.running)
        assertTrue("running: up however long", running.shown(t0.plusSeconds(600).toEpochMilli()))
        assertEquals("正在操作 MacBook · 点 AC", running.title.zh())
        assertEquals("Controlling MacBook · Click AC", running.title.english())
        assertEquals("MacBookを操作中・クリック AC", running.title.ja())
        assertEquals("正在操作 MacBook，点 AC", running.spoken.zh())

        val done = requireNotNull(ComputerActivity.latest(listOf(use, item("3", "tool_result", t0.plusSeconds(2), "t1")), "server", listOf(mac)))
        assertFalse(done.running)
        assertEquals(t0.plusSeconds(2).toEpochMilli(), done.endedAtMs)
        assertTrue(done.shown(t0.plusSeconds(5).toEpochMilli()))
        assertFalse("then fades", done.shown(t0.plusSeconds(9).toEpochMilli()))
        assertEquals("刚操作 MacBook · 点 AC", done.title.zh())
        assertEquals("Just controlled MacBook · Click AC", done.title.english())
        assertEquals("MacBookを操作しました・クリック AC", done.title.ja())
        assertNull(ComputerActivity.latest(listOf(bash), mac.id, listOf(mac)))
    }

    @Test fun onlyTheNewestCallCountsWithinItsWindow() {
        val older = item("1", "tool_use", t0, "t1", "mcp__cua-driver__screenshot", JsonObject(emptyMap()))
        val newer = item("2", "tool_use", t0.plusSeconds(1), "t2", "mcp__persona__computer_state", input("machine" to "studio"))
        assertNull("the newest names a Mac no list has: none, not an older one", ComputerActivity.latest(listOf(older, newer), mac.id, listOf(mac)))
        val cua = requireNotNull(ComputerActivity.latest(listOf(older), mac.id, listOf(mac)))
        assertEquals("截屏", cua.step.zh())
        val filler = (3..10).map { item("$it", "text", t0, "") }
        assertNull("older than the window", ComputerActivity.latest(listOf(older) + filler, mac.id, listOf(mac), window = 8))
        // A result with no time: ended, and not shown.
        val ended = requireNotNull(ComputerActivity.latest(listOf(older, Item("r", 2, role = "user", kind = "tool_result", toolUseId = "t1")),
            mac.id, listOf(mac)))
        assertFalse(ended.running)
        assertFalse(ended.shown(t0.toEpochMilli()))
    }
}
