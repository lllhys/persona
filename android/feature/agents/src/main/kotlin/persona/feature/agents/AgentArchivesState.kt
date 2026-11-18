package persona.feature.agents

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AgentArchives

internal data class AgentArchiveSnapshot(
    val sessions: List<Session> = emptyList(),
    val loaded: Boolean = false,
    val loading: Boolean = false,
    val busy: SessionKey? = null,
    val error: Throwable? = null,
)

/** Acknowledged writes update the list before refresh; a failed refresh must not undo a successful restore. */
internal class AgentArchivesState(
    val api: AgentArchives,
    private val scope: CoroutineScope,
    private val onChanged: suspend (SessionKey, Boolean) -> Unit,
) {
    private val mutable = MutableStateFlow(AgentArchiveSnapshot())
    val state = mutable.asStateFlow()

    fun refresh() {
        if (mutable.value.loading || mutable.value.busy != null) return
        mutable.value = mutable.value.copy(loading = true, error = null)
        scope.launch {
            try { load() }
            catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { mutable.value = mutable.value.copy(error = error) }
            finally { mutable.value = mutable.value.copy(loading = false) }
        }
    }

    private suspend fun load() {
        val sessions = api.list().filter { it.archived && !it.assistant }
        mutable.value = mutable.value.copy(sessions = sessions, loaded = true)
    }

    fun archive(session: Session) = change(session, archived = true)
    fun restore(session: Session) = change(session, archived = false)

    private fun change(session: Session, archived: Boolean) {
        if (mutable.value.loading || mutable.value.busy != null || !api.supports(session.key)) return
        mutable.value = mutable.value.copy(busy = session.key, error = null)
        scope.launch {
            try {
                if (archived) api.archive(session.key) else api.restore(session.key)
                val others = mutable.value.sessions.filterNot { it.key == session.key }
                mutable.value = mutable.value.copy(sessions = if (archived) others + session.copy(archived = true) else others)
                onChanged(session.key, archived)
                load()
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { mutable.value = mutable.value.copy(error = error) }
            finally { mutable.value = mutable.value.copy(busy = null) }
        }
    }

    fun clearError() { mutable.value = mutable.value.copy(error = null) }
}

/**
 * Source control capabilities do not govern archive visibility; active work must remain visible. Any agent archives,
 * whatever runs it (a Paseo provider's too, #8143), as the manager takes them all (agent_archives.go) and the web offers
 * it; only an assistant conversation archives in its own place.
 */
internal val Session.canArchiveAgent: Boolean
    get() = !assistant && !archived && !starting &&
        status !in setOf("working", "blocked") && state !in setOf("working", "needs_you") &&
        !awaitsReply && pendingInteractions == 0 && backgroundWorking != true &&
        backgroundCount == 0 && (runningWorkflowsCount ?: 0) == 0 &&
        (runningSubagents ?: 0) == 0 && subagents.orEmpty().none { it.running }
