package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Session
import persona.core.contracts.Subagent
import persona.core.domain.lines.SubagentStatus

/** A task card's status word and second line say what the web's do (`taskStatusWord`, `subagentMeta`), in each language. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
class SubagentCardTextTest {
    private val res get() = ApplicationProvider.getApplicationContext<Context>().resources
    private val agent = Subagent("a", toolUseId = "c", running = false, steps = 12, model = "claude-haiku-4-5-20251001", tokens = 23_400)

    @Test fun theSecondLineNamesWhatRanItItsModelTokensToolUsesAndTime() {
        assertEquals(SubagentCardText.Meta("Claude Code", "Haiku 4.5", "23.4k tokens", "12 tool uses", "1m 5s"),
            SubagentCardText.meta(res, agent, 65, assistant = false))
        assertEquals(SubagentCardText.Meta(null, "Haiku 4.5", "23.4k tokens", "1 tool use", null),
            SubagentCardText.meta(res, agent.copy(steps = 1), null, assistant = true))
        val bare = SubagentCardText.meta(res, null, 42, assistant = false)
        assertNull(bare.model); assertNull(bare.tokens); assertNull(bare.uses)
        assertEquals("42 s", bare.took)
    }

    /** #8143: what ran it is the chat's own agent, a Paseo provider by its own name; Claude Code only for Claude's. */
    @Test fun whatRanItIsTheChatsAgent() {
        val grok = Session(sessionId = "paseo-1", agent = "paseo", agentLabelHint = "My Grok", launcher = "grok")
        assertEquals("My Grok", subagentHarness(grok, res))
        assertEquals("grok", subagentHarness(grok.copy(agentLabelHint = "Paseo"), res))
        assertEquals("Codex", subagentHarness(Session(sessionId = "s", agent = "codex"), res))
        assertEquals("Claude Code", subagentHarness(Session(sessionId = "s", agent = "claude", launcher = "my-claude"), res))
        assertEquals("My Grok", SubagentCardText.meta(res, agent, 65, assistant = false, harness = subagentHarness(grok, res)).harness)
        // Nothing said (a preview, a card outside an Agent's chat): Claude Code, as before; a Persona conversation names none.
        assertEquals("Claude Code", SubagentCardText.meta(res, agent, 65, assistant = false, harness = null).harness)
        assertNull(SubagentCardText.meta(res, agent, 65, assistant = true, harness = "Grok").harness)
    }

    @Test fun theFourStatusWords() {
        assertEquals(listOf("Running", "Completed", "Failed", "Stopped"), SubagentStatus.entries.map { SubagentCardText.status(res, it) })
    }

    @Test @Config(qualifiers = "zh") fun inChinese() {
        assertEquals(listOf("运行中", "已完成", "失败", "已停止"), SubagentStatus.entries.map { SubagentCardText.status(res, it) })
        assertEquals("12 次工具调用", SubagentCardText.meta(res, agent, null, false).uses)
        assertEquals("23.4k tokens", SubagentCardText.meta(res, agent, null, false).tokens)
    }

    @Test @Config(qualifiers = "ja") fun inJapanese() {
        assertEquals(listOf("実行中", "完了", "失敗", "停止済み"), SubagentStatus.entries.map { SubagentCardText.status(res, it) })
        assertEquals("12 回のツール使用", SubagentCardText.meta(res, agent, null, false).uses)
    }
}
