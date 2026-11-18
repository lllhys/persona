package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.domain.ja
import persona.core.domain.zh
import persona.core.contracts.CoreText
import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/** One confirmation for the list's long press and swipe and the chat's menu: which agent, where, what happens. */
class AgentCloseTest {
    @Test fun itNamesTheAgentTheMachineAndTheTab() {
        val session = Session(machineId = "macbook", sessionId = "s", title = "补齐网络层的单元测试", spaceName = "weather", slotName = "tests")
        val confirmation = session.closeConfirmation("MacBook")
        assertEquals("关闭这个 Agent？", (confirmation.title).zhAny())
        assertEquals("「补齐网络层的单元测试」\nMacBook / weather › tests\n\n会退出 MacBook 上的 Claude，并关掉它在终端里的标签页「tests」" +
            "（分屏的标签页只关它这一格）。正在做的事会中断；对话记录会保留，之后可以在 Agents 的「历史」里继续。", (confirmation.message).zhAny())
    }

    @Test fun withoutATabOrAMachineNameItStillReads() {
        val codex = Session(machineId = "server", sessionId = "s", cwd = "/srv/shop", agent = "codex")
        val message = codex.closeConfirmation(null).message.zh()
        assertTrue(message.startsWith("「shop」\nserver\n\n会退出 server 上的 Codex，并关掉它所在的终端窗格。"))
    }

    // #6294: a runtime's session has no terminal tab or pane to close, though a Paseo row names its workspace in slot_name;
    // whether its conversation stays is the runtime's, so nothing is promised.
    @Test fun aRuntimesSessionOnlyEnds() {
        val paseo = Session(machineId = "macbook", sessionId = "s", title = "整理发布说明", spaceName = "weather", slotName = "发布",
            agent = "claude", sourceKind = "runtime", capabilities = SessionCapabilities(read = true, send = true, close = true))
        val confirmation = paseo.closeConfirmation("MacBook")
        assertEquals("关闭这个 Agent？", confirmation.title.zh())
        assertEquals("「整理发布说明」\nMacBook / weather › 发布\n\n会结束它在 MacBook 上的会话，正在做的事会中断。", confirmation.message.zh())
        assertEquals("「整理发布说明」\nMacBook / weather › 发布\n\nMacBookでこのセッションを終了します。作業中の処理は中断されます。", confirmation.message.ja())
        assertEquals("“%1\$s”\n%2\$s\n\nThis ends the session on %3\$s. Work in progress stops.", CoreText.Key.CloseAgentMessageRuntime.english)
        // An older node's row says no close and has no terminal: the same words, if anything opens it.
        assertEquals(confirmation, paseo.copy(capabilities = SessionCapabilities(read = true)).closeConfirmation("MacBook"))
    }
}
