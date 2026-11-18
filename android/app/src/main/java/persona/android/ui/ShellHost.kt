package persona.android.ui

import androidx.compose.runtime.State
import persona.core.contracts.SessionKey
import persona.core.contracts.api.TerminalPanes
import persona.core.contracts.api.TerminalTransport
import persona.core.designsystem.AppFeedback

/** App-owned services around the shell; navigation and page state remain in PersonaShell. */
internal interface ShellHost {
    val automation: Boolean
    val posture: State<String?>
    val feedback: AppFeedback
    val terminalPanes: TerminalPanes?
    fun groupTerminal(key: SessionKey): TerminalTransport?
    fun exitDemo()
}

internal class AppShellHost(private val app: AppServices) : ShellHost {
    override val automation get() = app.automation
    override val posture get() = app.posture
    override val feedback get() = app.feedback
    override val terminalPanes get() = app.terminalPanes
    override fun groupTerminal(key: SessionKey) = app.groupTerminal(key)
    override fun exitDemo() = app.exitDemo()
}
