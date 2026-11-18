package persona.feature.chat

import android.content.res.Resources
import persona.core.contracts.AgentState
import persona.core.contracts.Session

/** Follow only the same computer's pane, never a new thread inferred from its folder. */
internal fun codexThreadOf(rows: List<Session>, waiting: Session): Session? {
    val pane = waiting.paneId?.takeIf(String::isNotBlank) ?: return null
    if (!waiting.awaitingFirstMessage) return null
    return rows.firstOrNull {
        it.machineId == waiting.machineId && it.paneId == pane && it.sessionId != waiting.sessionId &&
            !it.awaitingFirstMessage && it.agent == "codex" && it.agentState != AgentState.Offline
    }
}

/** One empty-state note; it gives way to the first message and to view-only/error states. */
internal fun emptyChatHint(res: Resources, session: Session?, computer: String?, readOnly: Boolean): String = when {
    readOnly -> res.getString(R.string.chat_screen_empty_read_only)
    session?.awaitingFirstMessage == true -> res.getString(R.string.chat_codex_waiting,
        computer?.takeIf(String::isNotBlank) ?: res.getString(R.string.chat_codex_computer))
    else -> res.getString(R.string.chat_screen_empty_hint)
}
