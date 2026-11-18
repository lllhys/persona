package persona.android.ui

import persona.core.contracts.Session
import persona.core.contracts.SessionCapabilities
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** What the terminal page's Resume can bring back (#4345 / #5565). */
class TerminalResumeTest {
    @Test fun stoppedAgentAndClaudeWithoutSource() {
        val stopped = Session(
            machineId = "m",
            sessionId = "s",
            cwd = "/tmp/p",
            status = "offline",
            agent = "claude",
            capabilities = SessionCapabilities(read = true, terminal = true),
        )
        assertTrue(resumableInTerminal(stopped))
        assertTrue(resumableInTerminal(stopped.copy(agent = "codex", sourceKind = "codex_history")))
        // As iOS TerminalPanel.canResume: Claude without a source is resumable when the terminal shows NoTerminal.
        assertTrue(resumableInTerminal(Session(machineId = "m", sessionId = "s", cwd = "/tmp/p", status = "offline")))
        assertTrue(resumableInTerminal(stopped.copy(status = "idle", capabilities = null)))
        assertFalse(resumableInTerminal(stopped.copy(assistant = true)))
        assertFalse(resumableInTerminal(stopped.copy(starting = true)))
        assertFalse(resumableInTerminal(Session(machineId = "m", sessionId = "s", cwd = "", status = "offline")))
        assertFalse(resumableInTerminal(stopped.copy(sourceKind = "claude_cli", capabilities = SessionCapabilities(read = true, send = true))))
    }
}
